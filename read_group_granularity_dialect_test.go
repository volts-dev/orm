package orm

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/volts-dev/dataset"
)

type RgStamp struct {
	TModel `table:"name('rg_stamp')"`
	Id     int64     `field:"pk autoincr title('ID')"`
	Ts     time.Time `field:"datetime()"`
	Qty    int64     `field:"int()"`
}

// ReadGroup 按 `字段:month` / `字段:week` 加时区分组在三个方言上分出同样的组（MySQL 2026-10-26 起有日期粒度；
// 此前 MySQL 上直接报「needs Features.DateTrunc」，透视表 / 图表按日期分组整个打不开）。SQLite 仍报错。
func TestReadGroup_DateGranularity_OnEveryDialect(t *testing.T) {
	o, err := New(WithDataSource(testDataSource(t, filepath.Join(t.TempDir(), "rgts.db"))))
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	if _, err := o.SyncModel("", new(RgStamp)); err != nil {
		t.Fatal(err)
	}
	for _, r := range []struct {
		ts  time.Time
		qty int64
	}{
		{time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC), 1}, // 东八区 9-30 23:00
		{time.Date(2026, 9, 30, 17, 0, 0, 0, time.UTC), 2}, // 东八区 10-01 01:00
		{time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC), 4}, // 10 月
		{time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC), 8},  // 11 月
	} {
		if _, err := o.Model("rg.stamp").Create(map[string]any{"ts": r.ts, "qty": r.qty}); err != nil {
			t.Fatal(err)
		}
	}
	m, err := o.GetModel("rg.stamp")
	if err != nil {
		t.Fatal(err)
	}
	rg, ok := m.(interface {
		ReadGroup(*ReadGroupRequest) (*dataset.TDataSet, error)
	})
	if !ok {
		t.Fatalf("%T 没有 ReadGroup(*ReadGroupRequest)", m)
	}
	run := func(groupBy, tz string) (string, error) {
		ds, err := rg.ReadGroup(&ReadGroupRequest{GroupBy: []string{groupBy}, Fields: []string{"qty"}, Tz: tz})
		if err != nil {
			return "", err
		}
		var out []string
		for ds.First(); !ds.Eof(); ds.Next() {
			v := ds.FieldByName("ts").AsInterface()
			if tm, ok := v.(time.Time); ok {
				v = tm.UTC().Format("2006-01-02 15:04")
			}
			out = append(out, strings.TrimSpace(strings.Join([]string{rgStampText(v), ds.FieldByName("qty").AsString()}, "=")))
		}
		sort.Strings(out)
		return strings.Join(out, " "), nil
	}
	if testDBKind() == "sqlite" {
		if _, err := run("ts:month", ""); err == nil || !strings.Contains(err.Error(), "DateTrunc") {
			t.Errorf("SQLite 上按月分组应报 DateTrunc，得 %v", err)
		}
		return
	}
	for _, c := range []struct{ groupBy, tz, want string }{
		{"ts:month", "", "2026-09-01 00:00=3 2026-10-01 00:00=4 2026-11-01 00:00=8"},
		// 东八区：9-30 17:00Z 已是 10 月；组的起点是本地 10-01 零点 = 9-30 16:00Z。
		{"ts:month", "Asia/Shanghai", "2026-08-31 16:00=1 2026-09-30 16:00=6 2026-10-31 16:00=8"},
		{"ts:week", "", "2026-09-28 00:00=3 2026-10-12 00:00=4 2026-10-26 00:00=8"},
	} {
		got, err := run(c.groupBy, c.tz)
		if err != nil {
			t.Fatalf("%s %s: %v", c.groupBy, c.tz, err)
		}
		if got != c.want {
			t.Errorf("%s %s：得 %s，want %s", c.groupBy, c.tz, got, c.want)
		}
	}
}

func rgStampText(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case []byte:
		return string(x)
	case string:
		return x
	}
	return fmt.Sprint(v)
}
