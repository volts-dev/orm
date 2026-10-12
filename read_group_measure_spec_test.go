package orm

import (
	"testing"
)

/*
ReadGroup 的 measures 里带冒号的两种写法（Fields 那条老路；Odoo 19 的 Aggregates 另有一套）。

  - `字段:算子`：Odoo ≤16 read_group 的 fields 参数写法（`sale_price:sum`），spreadsheet 的
    透视表照这么发。结果列名是**字段名**（与 Odoo 一致），算子由调用方指定。
  - `字段:粒度`：分组写法（`day:month`、`occurred:day`）跟着一起混进了 fields——它不是度量。

此前两种都被整串当字段名去查，查不到就打
`ReadGroup: measure field "sale_price:sum" not found on model event.sale.report, ignored`，
前者的合计列于是凭空消失，后者白白在日志中心里挂一条问题（2026-10-09 ~ 10-12 真栈：
admin.log 的 occurred:day、social.platform.usage 的 day:month、event.sale.report 的
sale_price:sum）。
*/

func TestSplitMeasure(t *testing.T) {
	cases := []struct{ spec, name, op string }{
		{"qty", "qty", ""},
		{"sale_price:sum", "sale_price", "sum"},
		{"day:month", "day", "month"},
		{"amount:SUM", "amount", "sum"},
		{" qty ", "qty", ""},
	}
	for _, c := range cases {
		if n, op := SplitMeasure(c.spec); n != c.name || op != c.op {
			t.Errorf("SplitMeasure(%q) = (%q, %q)，期望 (%q, %q)", c.spec, n, op, c.name, c.op)
		}
	}
}

func TestReadGroup_MeasureWithOperator(t *testing.T) {
	o := setupReadGroup(t) // kind x: qty 1,2 price 10,20；kind y: qty 4 price 40

	ds, err := o.Model("rg.box").ReadGroup([]string{"kind"}, []string{"qty:max", "price:sum", "kind:count"})
	if err != nil {
		t.Fatal(err)
	}
	maxQty := map[string]float64{}
	sumPrice := map[string]float64{}
	ds.First()
	for !ds.Eof() {
		k := ds.FieldByName("kind").AsString()
		if fs := ds.FieldByName("qty"); fs.IsValid {
			maxQty[k] = fs.AsFloat()
		}
		if fs := ds.FieldByName("price"); fs.IsValid {
			sumPrice[k] = fs.AsFloat()
		}
		ds.Next()
	}
	if maxQty["x"] != 2 || maxQty["y"] != 4 {
		t.Fatalf("qty:max 应按字段名 qty 回每组最大值 x=2 y=4，实得 %v（空 = 整串当字段名被跳过了）", maxQty)
	}
	if sumPrice["x"] != 30 || sumPrice["y"] != 40 {
		t.Fatalf("price:sum 应回 x=30 y=40，实得 %v", sumPrice)
	}
}

// `字段:粒度` 是分组写法，不是度量：跳过、不产出列，也不让整次分组失败。
func TestReadGroup_GranularitySpecInMeasuresIsNotAMeasure(t *testing.T) {
	o := setupReadGroup(t)
	ds, err := o.Model("rg.box").ReadGroup([]string{"kind"}, []string{"qty", "name:month", "price:bogus"})
	if err != nil {
		t.Fatal(err)
	}
	if ds.Count() != 2 {
		t.Fatalf("应分出 2 组，实得 %d", ds.Count())
	}
	if !ds.FieldByName("qty").IsValid {
		t.Fatal("正常的度量 qty 丢了")
	}
}
