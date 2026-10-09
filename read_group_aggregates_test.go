package orm

import "testing"

/*
ReadGroup 的 Aggregates（Odoo 19 formatted_read_group 的度量写法 `字段:算子`）。

透视表里算子是**用户选的**（同一个字段可以既求和又求平均、对 many2one 去重计数），不能再取字段
的 group_operator。不给 Aggregates 时行为与以前逐字节相同（上面那组用例照旧绿）。
*/

func TestReadGroup_AggregatesUseCallerOperators(t *testing.T) {
	o := setupReadGroup(t)
	ds, err := o.Model("rg.box").readGroup([]string{"kind"}, nil, &readGroupOptions{aggregates: []string{
		"qty:sum", "price:avg", "price:max", "name:count_distinct", "name:count", "__count",
		"no_such_field:sum", // 不存在：跳过，不让整张透视表打不开
		"name:sum",          // 非数值不求和：跳过
	}})
	if err != nil {
		t.Fatalf("ReadGroup with aggregates: %v", err)
	}
	if ds == nil || ds.Count() != 2 {
		t.Fatalf("按 kind 应分出 2 组，实得 %v", ds.Count())
	}
	got := map[string]map[string]float64{}
	ds.First()
	for !ds.Eof() {
		k := ds.FieldByName("kind").AsString()
		got[k] = map[string]float64{
			"qty:sum":             ds.FieldByName(AggregateAlias("qty:sum")).AsFloat(),
			"price:avg":           ds.FieldByName(AggregateAlias("price:avg")).AsFloat(),
			"price:max":           ds.FieldByName(AggregateAlias("price:max")).AsFloat(),
			"name:count_distinct": ds.FieldByName(AggregateAlias("name:count_distinct")).AsFloat(),
			"name:count":          ds.FieldByName(AggregateAlias("name:count")).AsFloat(),
			"__count":             float64(ds.FieldByName(GroupCountField).AsInteger()),
		}
		if fs := ds.Record().FieldByName(AggregateAlias("name:sum")); fs != nil && fs.IsValid {
			t.Fatalf("非数值列不该求和，却出了 name:sum 一列")
		}
		ds.Next()
	}
	want := map[string]map[string]float64{
		"x": {"qty:sum": 3, "price:avg": 15, "price:max": 20, "name:count_distinct": 2, "name:count": 2, "__count": 2},
		"y": {"qty:sum": 4, "price:avg": 40, "price:max": 40, "name:count_distinct": 1, "name:count": 1, "__count": 1},
	}
	for k, w := range want {
		for m, v := range w {
			if got[k][m] != v {
				t.Errorf("%s %s = %v, want %v", k, m, got[k][m], v)
			}
		}
	}
}

// 算子会作为字面量进 SQL：白名单以外的一律报错，不拼。
func TestReadGroup_AggregatesRejectUnknownOperator(t *testing.T) {
	o := setupReadGroup(t)
	for _, bad := range []string{"qty:drop table", "qty", "qty:SUM"} {
		if _, err := o.Model("rg.box").readGroup([]string{"kind"}, nil, &readGroupOptions{aggregates: []string{bad}}); err == nil {
			t.Errorf("%q 应被拒绝", bad)
		}
	}
}

func TestValidTz(t *testing.T) {
	for _, ok := range []string{"UTC", "Asia/Shanghai", "America/Argentina/Buenos_Aires", "Etc/GMT+8"} {
		if !validTz(ok) {
			t.Errorf("%q 应是合法时区", ok)
		}
	}
	for _, bad := range []string{"", "Asia/Shanghai'; drop table x; --", "Nowhere/Land", "../etc"} {
		if validTz(bad) {
			t.Errorf("%q 不该被当成时区", bad)
		}
	}
}

func TestSplitGroupBy_NumberGranularities(t *testing.T) {
	f, g := SplitGroupBy("date_order:month_number")
	if f != "date_order" || g != "month_number" || !IsNumberGranularity(g) {
		t.Fatalf("got %q %q", f, g)
	}
	if f, g := SplitGroupBy("date_order:bogus"); f != "date_order:bogus" || g != "" {
		t.Fatalf("非法粒度应原样当字段名返回，got %q %q", f, g)
	}
}
