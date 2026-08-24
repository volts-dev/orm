package orm

import (
	"testing"
)

// TestCreate_MultiRow_SetsAppliedToEveryRow 复现并守卫一个真栈撞出的缺陷：
// 一次 Create 写多行、同时用 .Set(...) 盖一个「统一字段」（vectors 里就是
// withSession 对 tenant_id/create_id/write_id 的盖戳）时，**只有第 1 行**被盖上，
// 第 2 行起该字段是零值。
//
// 症状（2026-07-24 kylin 真栈）：批量建的记录按 id 读得到、按条件读不到——因为
// 后续行 tenant_id=0 被 withSession 的 `WHERE tenant_id=?` 全部过滤掉。
func TestCreate_MultiRow_SetsAppliedToEveryRow(t *testing.T) {
	o := setupIntegrationOrm(t)

	// age 扮演 tenant_id：每行都不显式提供，全靠 .Set 盖上。
	ids, err := o.Model("bench.model").Set("age", 777).Create(
		map[string]any{"name": "row-a"},
		map[string]any{"name": "row-b"},
		map[string]any{"name": "row-c"},
	)
	if err != nil {
		t.Fatalf("multi-row create with Set: %v", err)
	}
	if len(ids) != 3 {
		t.Fatalf("expected 3 ids, got %d", len(ids))
	}

	ds, err := o.Model("bench.model").Limit(-1).Read()
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if ds.Count() != 3 {
		t.Fatalf("expected 3 rows, got %d", ds.Count())
	}

	ds.First()
	for !ds.Eof() {
		name := ds.FieldByName("name").AsString()
		age := ds.FieldByName("age").AsInteger()
		if age != 777 {
			t.Errorf("row %q: age=%d, want 777 (the Set field must land on EVERY row, not just the first)", name, age)
		}
		ds.Next()
	}
}
