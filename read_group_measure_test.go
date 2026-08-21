package orm

import (
	"path/filepath"
	"testing"
)

/*
ReadGroup 的 measure 容错。

kanban 按设计把**整张卡片的字段列表**原样当 measures 传过来，而 arch 多半是照 Odoo
抄的——里面必然有本仓没实现的字段。以前那种情况直接报错，整页分组只剩一行

	ReadGroup: measure field "activity_state" not found on model pro.tmpl

而那行字看不出跟哪个字段、哪张 arch 有关。现在跳过并打一条点名的 Warn。
*/

type (
	RgBox struct {
		TModel `table:"name('rg_box')"`
		Id     int64   `field:"pk autoincr title('ID')"`
		Name   string  `field:"varchar() size(64)"`
		Kind   string  `field:"varchar() size(16)"`
		Qty    int64   `field:"int()"`
		Price  float64 `field:"double()"`
	}
)

func setupReadGroup(t *testing.T) *TOrm {
	t.Helper()
	ds := &TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "rg.db")}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(RgBox)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	for _, r := range []struct {
		name, kind string
		qty        int64
		price      float64
	}{
		{"a", "x", 1, 10}, {"b", "x", 2, 20}, {"c", "y", 4, 40},
	} {
		if _, err := o.Model("rg.box").Create(map[string]any{
			"name": r.name, "kind": r.kind, "qty": r.qty, "price": r.price,
		}); err != nil {
			t.Fatal(err)
		}
	}
	return o
}

// TestReadGroup_UnknownMeasureIsSkipped 是本次修复的主判据：
// measures 里混进模型上不存在的字段时，**整次分组仍要成功**，已知的 measure 照常合计。
func TestReadGroup_UnknownMeasureIsSkipped(t *testing.T) {
	o := setupReadGroup(t)

	ds, err := o.Model("rg.box").ReadGroup([]string{"kind"},
		[]string{"qty", "activity_state", "price", "message_needaction"})
	if err != nil {
		t.Fatalf("measures 里混进未知字段就整次失败了（kanban 会因此整页打不开）: %v", err)
	}
	if ds == nil || ds.Count() != 2 {
		t.Fatalf("按 kind 应分出 2 组，实得 %v", ds.Count())
	}

	sums := map[string]float64{}
	counts := map[string]int64{}
	ds.First()
	for !ds.Eof() {
		k := ds.FieldByName("kind").AsString()
		sums[k] = ds.FieldByName("qty").AsFloat()
		counts[k] = ds.FieldByName(GroupCountField).AsInteger()
		ds.Next()
	}
	if sums["x"] != 3 || sums["y"] != 4 {
		t.Fatalf("已知 measure 的合计不对: %v", sums)
	}
	if counts["x"] != 2 || counts["y"] != 1 {
		t.Fatalf("分组计数不对: %v", counts)
	}
}

// TestReadGroup_UnknownGroupByStillErrors 钉住**没有一起放宽**的那一半：
// 分组字段本身不存在仍然报错。measures 是 kanban 塞进来的一大把，分组字段是用户
// 明确点的一个——后者写错时静默换一种分法，比报错难查得多。
func TestReadGroup_UnknownGroupByStillErrors(t *testing.T) {
	o := setupReadGroup(t)
	if _, err := o.Model("rg.box").ReadGroup([]string{"nope_field"}, []string{"qty"}); err == nil {
		t.Fatal("分组字段不存在时应当报错")
	}
}

// TestReadGroup_NonAggregatableMeasureStillSkipped 确认既有行为没被动：
// 字段存在但聚合不了（字符串）时照旧跳过，不报错。
func TestReadGroup_NonAggregatableMeasureStillSkipped(t *testing.T) {
	o := setupReadGroup(t)
	ds, err := o.Model("rg.box").ReadGroup([]string{"kind"}, []string{"name", "qty"})
	if err != nil {
		t.Fatalf("字符串 measure 应当被跳过而不是报错: %v", err)
	}
	if ds == nil || ds.Count() != 2 {
		t.Fatalf("应分出 2 组，实得 %v", ds.Count())
	}
}
