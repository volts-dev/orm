package orm

import (
	"testing"

	"github.com/volts-dev/dataset"
)

// TestCreate_EmptyValues_SetsStillApplied 空载荷建记录（前端新建表单什么都没改就点保存，
// 只回传改过的字段 → Data 是 `[{}]`）时，.Set 盖的统一字段也必须落进 INSERT。
//
// 真栈（2026-09-15）：「设置 ▸ 技术 ▸ 视图」新建后直接保存，INSERT 里没有 tenant_id /
// create_id，撞 NOT NULL 报 500；随便填一格再存就好了 —— withSession 的盖戳只在载荷
// 非空时生效。
func TestCreate_EmptyValues_SetsStillApplied(t *testing.T) {
	// 根因修在 volts-dev/dataset（c812c8c）。orm 的 go.mod 钉的是 dataset 的发布版本：
	// 还没升到含修复的版本时跳过（而不是红着），升了版本自动生效。
	if !datasetKeepsSetOnEmptyRecord() {
		t.Skip("钉住的 volts-dev/dataset 还没有 c812c8c（空数据集上 Record().SetByField 读不回），升级 dataset 后本用例生效")
	}
	o := setupIntegrationOrm(t)

	ids, err := o.Model("bench.model").Set("age", 777).Create(map[string]any{})
	if err != nil {
		t.Fatalf("empty-values create with Set: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("expected 1 id, got %d", len(ids))
	}
	ds, err := o.Model("bench.model").Limit(-1).Read()
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if ds.Count() != 1 {
		t.Fatalf("expected 1 row, got %d", ds.Count())
	}
	if age := ds.FieldByName("age").AsInteger(); age != 777 {
		t.Fatalf("age=%d, want 777: the Set field must land even when the submitted values are empty", age)
	}
}

// datasetKeepsSetOnEmptyRecord 当前链接的 dataset 是否已修好「空数据集上现造的记录写了读不回」。
func datasetKeepsSetOnEmptyRecord() bool {
	ds := dataset.NewDataSet()
	ds.Record().SetByField("probe", 1)
	return ds.Record().GetByField("probe") == 1
}
