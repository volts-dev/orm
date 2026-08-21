package orm

import (
	"path/filepath"
	"testing"
)

/*
one2many 字段上声明的 domain 必须生效。

此前 TModel.OneToMany **完全不看 field.Domain()**（m2m 那边一直是看的），于是
`one2many(x, fk) domain([('active','=',1)])` 是纯装饰：归档的子记录照样出现在内嵌
列表里，不报错。同一张表按不同条件切成两个 o2m 字段时后果更直白——两栏内容一模一样
（日记账的「收款方式」「付款方式」两栏就是这个形状）。
*/

type (
	O2mDomBox struct {
		TModel `table:"name('o2m_dom_box')"`
		Id     int64  `field:"pk autoincr title('ID')"`
		Name   string `field:"varchar() size(64)"`
		// 同一张子表，按 kind 切成两栏。
		InIds  []any `field:"one2many(o2m_dom_item,box_id) domain([('kind','=','in')])"`
		OutIds []any `field:"one2many(o2m_dom_item,box_id) domain([('kind','=','out')])"`
		AllIds []any `field:"one2many(o2m_dom_item,box_id)"`
	}

	O2mDomItem struct {
		TModel `table:"name('o2m_dom_item')"`
		Id     int64  `field:"pk autoincr title('ID')"`
		Name   string `field:"varchar() size(64)"`
		Kind   string `field:"varchar() size(16)"`
		BoxId  int64  `field:"many2one(o2m_dom_box)"`
	}
)

func TestOneToMany_FieldDomainIsApplied(t *testing.T) {
	ds := &TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "o2mdom.db")}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(O2mDomItem), new(O2mDomBox)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}

	boxIds, err := o.Model("o2m.dom.box").Create(map[string]any{"name": "box"})
	if err != nil {
		t.Fatal(err)
	}
	boxId := firstId(t, boxIds)
	for _, it := range []struct{ name, kind string }{
		{"i1", "in"}, {"i2", "in"}, {"o1", "out"},
	} {
		if _, err := o.Model("o2m.dom.item").Create(map[string]any{
			"name": it.name, "kind": it.kind, "box_id": boxId,
		}); err != nil {
			t.Fatal(err)
		}
	}

	// o2m 的值只在 classic read 下回填（同前端 form_model/list_model 走的那条路），
	// 普通读取根本不触发关系字段的 OnRead。
	rec, err := o.NewSession(true).Model("o2m.dom.box").Ids(boxId).
		Select("id", "name", "in_ids", "out_ids", "all_ids").Limit(-1).Read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if rec == nil || rec.Count() != 1 {
		t.Fatalf("读不到 box: %v", rec.Count())
	}
	countOf := func(field string) int {
		v := rec.Record().GetByField(field)
		switch vv := v.(type) {
		case []any:
			return len(vv)
		case []map[string]any:
			return len(vv)
		case nil:
			return 0
		}
		t.Fatalf("%s 的值形态没见过: %#v", field, v)
		return -1
	}
	if got := countOf("all_ids"); got != 3 {
		t.Fatalf("不带 domain 的 all_ids 应有 3 条，实得 %d", got)
	}
	if got := countOf("in_ids"); got != 2 {
		t.Fatalf(`domain kind=in 的 in_ids 应有 2 条，实得 %d —— domain 没生效`, got)
	}
	if got := countOf("out_ids"); got != 1 {
		t.Fatalf(`domain kind=out 的 out_ids 应有 1 条，实得 %d —— domain 没生效`, got)
	}
}
