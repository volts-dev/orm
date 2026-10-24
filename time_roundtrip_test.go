package orm

import (
	"path/filepath"
	"testing"
	"time"
)

// 日期时间的口径（MySQL 兼容 P2，2026-10-24）：三个方言都按 PG 的行为——**存的是传进来的 time.Time
// 的墙上字段，不看时区标签；读回来是贴 UTC 标签（偏移 0）的墙上时间**。全栈建立在这上面：写入用
// time.Now()（本地标签），与读回值比较用 vectors core/wall 的 wall.Now()（墙上时间贴 UTC 标签），
// 读出来再写回去的值也不能变。MySQL 探针当时差 8 小时，是 go-sql-driver 先按 loc 换算了时刻。

type TimeRec struct {
	TModel `table:"name('time_rec')"`
	Id     int64     `field:"pk autoincr"`
	Name   string    `field:"varchar()"`
	Seen   time.Time `field:"datetime()"`
	Born   time.Time `field:"date()"`
}

const wallLayout = "2006-01-02 15:04:05"

func newTimeOrm(t *testing.T) *TOrm {
	t.Helper()
	o, err := New(WithDataSource(testDataSource(t, filepath.Join(t.TempDir(), "time.db"))))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { o.Close() })
	if _, err := o.SyncModel("", new(TimeRec)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	return o
}

func TestDateTime_RoundTripKeepsWallClock(t *testing.T) {
	o := newTimeOrm(t)

	// 同一组墙上字段，贴三种标签：本地（time.Now() 的形状）、UTC（读回值 / wall.Now() 的形状）、
	// 一个别的偏移。三者落库都必须是 09:30:15。
	wallAt := func(loc *time.Location) time.Time { return time.Date(2026, 10, 24, 9, 30, 15, 0, loc) }
	const want = "2026-10-24 09:30:15"
	for _, in := range []time.Time{wallAt(time.Local), wallAt(time.UTC), wallAt(time.FixedZone("x", 5*3600))} {
		ids, err := o.Model("time.rec").Create(map[string]any{"name": in.Location().String(), "seen": in, "born": in})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		id := ids[0]
		ds, err := o.Model("time.rec").Ids(id).Read()
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		got := ds.FieldByName("seen").AsDateTime()
		if got.Format(wallLayout) != want {
			t.Errorf("写入 %s 读回墙上时间 %s，想要 %s", in, got.Format(wallLayout), want)
		}
		if _, off := got.Zone(); off != 0 {
			t.Errorf("读回值的偏移是 %ds，想要 0（与 lib/pq / wall.Now() 同形）：%s", off, got)
		}
		if b := ds.FieldByName("born").AsDateTime().Format("2006-01-02"); b != "2026-10-24" {
			t.Errorf("date 读回 %s", b)
		}

		// 按同一个值查得到（参数也只看墙上字段）；读回来的值原样写回，墙上时间不变。
		for _, q := range []time.Time{in, got} {
			n, err := o.Model("time.rec").Where("id=? AND seen=?", id, q).Count()
			if err != nil {
				t.Fatalf("Count: %v", err)
			}
			if n != 1 {
				t.Errorf("Where(seen=%s) 查到 %d 行，想要 1", q, n)
			}
		}
		if _, err := o.Model("time.rec").Ids(id).Write(map[string]any{"seen": got}); err != nil {
			t.Fatalf("Write: %v", err)
		}
		ds, _ = o.Model("time.rec").Ids(id).Read()
		if s := ds.FieldByName("seen").AsDateTime().Format(wallLayout); s != want {
			t.Errorf("读回值原样写回后变成 %s", s)
		}
	}
}

// 不给值时 ORM 自己填的「现在」（create_time / write_time 走这条）与写入的墙上时间同一套语义。
func TestDateTime_NowIsWallClockInConfiguredZone(t *testing.T) {
	o := newTimeOrm(t)
	v, _ := o._nowTime(DateTime)
	s, ok := v.(string)
	if !ok {
		t.Fatalf("_nowTime(DateTime) = %T", v)
	}
	got, err := time.ParseInLocation(wallLayout, s, o.Config().TimeZone)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	if d := time.Since(got); d < -time.Minute || d > time.Minute {
		t.Errorf("_nowTime 给出 %s，与现在差 %s——不是 %s 的墙上时间", s, d, o.Config().TimeZone)
	}
}
