package orm

import (
	stdErrors "errors"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
	ormerr "github.com/volts-dev/orm/errors"
)

/*
真库上的行锁行为。sqlite 没有行锁，光靠 lock_test.go 只能证明"SQL 拼对了"，
证明不了"真的互斥"——而 ForUpdate() 从前的毛病恰恰是看着对、实际不锁。
这里在 postgres 上验三件事：

  - 锁子句真进了 SQL，且带 `OF <主表>`（否则一旦查询含 LEFT JOIN，PG 会以
    "FOR UPDATE cannot be applied to the nullable side of an outer join" 报错）；
  - 两个事务确实互斥：先拿锁的没提交，后来者 NOWAIT 必须失败而不是读到旧值；
  - NOWAIT 抢锁失败映射成 errors.ErrConflict，调用方能与死锁走同一条重试分支。

postgres 不可用时整组跳过。
*/

type lockPgRec struct {
	TModel `table:"name('lock_pg_rec')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(32)"`
	Qty    int    `field:"int"`
}

func newLockPgOrm(t *testing.T) *TOrm {
	t.Helper()
	ds := &TDataSource{DbType: "postgres", Host: "localhost", Port: "5432", UserName: "postgres", Password: "postgres", DbName: "test_orm", SSLMode: "disable"}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	if _, err := o.Exec(`DROP TABLE IF EXISTS lock_pg_rec`); err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	t.Cleanup(func() { o.Exec(`DROP TABLE IF EXISTS lock_pg_rec`) })

	if _, err := o.SyncModel("", new(lockPgRec)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	if _, err := o.Model("lock.pg.rec").Create(map[string]any{"name": "a", "qty": 1}); err != nil {
		t.Fatalf("create: %v", err)
	}
	return o
}

func TestPg_ForUpdate_EmitsLockClause(t *testing.T) {
	o := newLockPgOrm(t)

	sess := o.NewSession()
	defer sess.Close()
	if err := sess.Begin(); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer sess.Rollback(nil)

	if _, err := sess.Model("lock.pg.rec").Where("qty=?", 1).ForUpdate().Read(); err != nil {
		t.Fatalf("ForUpdate().Read(): %v", err)
	}
	sql, _ := sess.LastSQL()
	up := strings.ToUpper(sql)
	if !strings.Contains(up, "FOR UPDATE") {
		t.Fatalf("SQL 里没有锁子句：%s", sql)
	}
	// 锁必须限定在主表上，且排在 LIMIT 之后。
	if !strings.Contains(up, `FOR UPDATE OF "LOCK_PG_REC"`) {
		t.Fatalf("锁子句应限定主表 (FOR UPDATE OF ...)：%s", sql)
	}
	if i, j := strings.Index(up, "LIMIT"), strings.Index(up, "FOR UPDATE"); i >= 0 && i > j {
		t.Fatalf("FOR UPDATE 必须排在 LIMIT 之后：%s", sql)
	}
}

// 真互斥：A 事务锁住行不提交，B 事务用 NOWAIT 必须立刻失败。
// 这一条是整个修复的核心断言——从前它会稳定通过（因为根本没加锁，B 直接读到）。
func TestPg_ForUpdate_ActuallyBlocksConcurrentTx(t *testing.T) {
	o := newLockPgOrm(t)

	a := o.NewSession()
	defer a.Close()
	if err := a.Begin(); err != nil {
		t.Fatalf("A.Begin: %v", err)
	}
	defer a.Rollback(nil)

	if _, err := a.Model("lock.pg.rec").Where("qty=?", 1).ForUpdate().Read(); err != nil {
		t.Fatalf("A 取锁失败: %v", err)
	}

	b := o.NewSession()
	defer b.Close()
	if err := b.Begin(); err != nil {
		t.Fatalf("B.Begin: %v", err)
	}
	defer b.Rollback(nil)

	done := make(chan error, 1)
	go func() {
		_, err := b.Model("lock.pg.rec").Where("qty=?", 1).ForUpdate(NoWait()).Read()
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("A 已持有排他行锁，B 的 NOWAIT 读却成功了——锁没有生效")
		}
		if !stdErrors.Is(err, ormerr.ErrConflict) {
			t.Fatalf("NOWAIT 抢锁失败应映射为 ErrConflict，得 %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("NOWAIT 不该阻塞")
	}
}

// SKIP LOCKED：被别人锁住的行直接跳过，结果集因此可能少于 WHERE 实际匹配数。
// 任务队列取件靠的就是这个语义。
func TestPg_ForUpdate_SkipLocked(t *testing.T) {
	o := newLockPgOrm(t)
	if _, err := o.Model("lock.pg.rec").Create(map[string]any{"name": "b", "qty": 1}); err != nil {
		t.Fatalf("create: %v", err)
	}

	a := o.NewSession()
	defer a.Close()
	if err := a.Begin(); err != nil {
		t.Fatalf("A.Begin: %v", err)
	}
	defer a.Rollback(nil)

	// A 锁住其中一条
	if _, err := a.Model("lock.pg.rec").Where("name=?", "a").ForUpdate().Read(); err != nil {
		t.Fatalf("A 取锁失败: %v", err)
	}

	b := o.NewSession()
	defer b.Close()
	if err := b.Begin(); err != nil {
		t.Fatalf("B.Begin: %v", err)
	}
	defer b.Rollback(nil)

	ds, err := b.Model("lock.pg.rec").Where("qty=?", 1).Limit(-1).ForUpdate(SkipLocked()).Read()
	if err != nil {
		t.Fatalf("B SKIP LOCKED 读失败: %v", err)
	}
	if ds.Count() != 1 {
		t.Fatalf("两条匹配、一条被锁，SKIP LOCKED 应只回 1 条，得 %d", ds.Count())
	}
}

// 加锁读绝不能被 SQL 结果缓存挡住：命中缓存等于这条 SELECT 根本没发给数据库，
// 锁自然也没加上——那就又回到了"以为锁住了其实没有"。
func TestPg_ForUpdate_BypassesSqlCache(t *testing.T) {
	o := newLockPgOrm(t)

	// 先跑一次不加锁的读，把结果灌进 SQL 缓存。
	if _, err := o.Model("lock.pg.rec").Where("qty=?", 1).Read(); err != nil {
		t.Fatalf("warmup read: %v", err)
	}

	sess := o.NewSession()
	defer sess.Close()
	if err := sess.Begin(); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer sess.Rollback(nil)

	if _, err := sess.Model("lock.pg.rec").Where("qty=?", 1).ForUpdate().Read(); err != nil {
		t.Fatalf("ForUpdate().Read(): %v", err)
	}
	// 缓存命中时 lastSQL 不会被刷新成带锁的语句；这里断言它确实发了库。
	if sql, _ := sess.LastSQL(); !strings.Contains(strings.ToUpper(sql), "FOR UPDATE") {
		t.Fatalf("加锁读被缓存挡下了，最后执行的 SQL 是：%s", sql)
	}
}
