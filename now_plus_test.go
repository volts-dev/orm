package orm

import (
	"path/filepath"
	"testing"
)

// NowPlusSeconds 在三个库上都是「库的当前时刻 ± N 秒」（MySQL 兼容 P4，2026-10-25）：vectors 附件账本的墓碑
// 时刻此前写死 PG 的 now() + CAST(? AS interval)，MySQL 上 1064，秒传那一支（加引用）整个失败。
func TestDialect_NowPlusSeconds(t *testing.T) {
	o, err := New(WithDataSource(testDataSource(t, filepath.Join(t.TempDir(), "nowplus.db"))))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer o.Close()
	d := o.Dialect()
	now, a0 := d.NowPlusSeconds(0)
	later, a1 := d.NowPlusSeconds(3600)
	earlier, a2 := d.NowPlusSeconds(-3600)
	s := o.NewSession()
	defer s.Close()
	ds, err := s.Query("SELECT CASE WHEN "+later+" > "+now+" THEN 1 ELSE 0 END AS a, CASE WHEN "+earlier+" < "+now+" THEN 1 ELSE 0 END AS b",
		a1, a0, a2, a0)
	if err != nil {
		t.Fatalf("%s: %v", d.DBType(), err)
	}
	if ds.FieldByName("a").AsInteger() != 1 || ds.FieldByName("b").AsInteger() != 1 {
		t.Errorf("%s：+1h 不晚于 now 或 -1h 不早于 now", d.DBType())
	}
}
