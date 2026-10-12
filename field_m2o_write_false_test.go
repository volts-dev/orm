package orm

import (
	"testing"
)

/*
many2one **写入** false / 空串的回归（读侧的 `('x','=',False)` 见 expr_m2o_false_test.go）。

false 是 Odoo 表达「这个多对一没值」的写法，移植过来的前端与模块代码原样这么发：
x2many 里新建一条「段落」行（`display_type: 'line_section'`）时 product_id /
product_uom_id 都是 false；设置页把「默认报价模板」清空时写的是
`sale_order_template_id: false`。

TMany2OneField.OnWrite 见空值先套默认值，没有默认值时 false 原样落进 `default:` 分支
——绑进 bigint 列：

	INSERT INTO sale_order_template_line (... "product_id" ...) VALUES (... false ...)
	pq: invalid input syntax for type bigint: "false" (22P02)
	UPDATE res_company SET "sale_order_template_id"=$3 ... [... false ...]
	pq: invalid input syntax for type bigint: "false" (22P02)

（2026-10-10 真栈，报价模板加段落行 / 设置页清空默认模板，各自整笔 500。）

判据是**库里那一格是不是 SQL NULL**：sqlite 不做类型检查，旧代码在这里不报错，而是
把 false 折成 0 存下去——与 PG 上的 500 同一个病灶，只是换了个不报错的样子。
*/

func m2oNullCount(t *testing.T, o *TOrm, name string) int64 {
	t.Helper()
	ds, err := o.Query(`SELECT count(*) AS n FROM isnull_menu WHERE site_id IS NULL AND name = ?`, name)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	return ds.Record().FieldByName("n").AsInteger()
}

func TestM2OWrite_FalseOnCreateStoresNull(t *testing.T) {
	o, _ := setupIsNull(t)

	mustCreate(t, o, "isnull.menu", map[string]any{"name": "section", "site_id": false})
	if n := m2oNullCount(t, o, "section"); n != 1 {
		t.Fatalf("新建时 site_id=false 应存 NULL，实际 IS NULL 的行数 %d（PG 上这条是 22P02）", n)
	}
}

func TestM2OWrite_FalseOnUpdateStoresNull(t *testing.T) {
	o, site1 := setupIsNull(t)

	id := firstId(t, mustCreate(t, o, "isnull.menu", map[string]any{"name": "cleared", "site_id": site1}))
	if _, err := o.Model("isnull.menu").Ids(id).Write(map[string]any{"site_id": false}); err != nil {
		t.Fatalf("write site_id=false: %v", err)
	}
	if n := m2oNullCount(t, o, "cleared"); n != 1 {
		t.Fatalf("更新时 site_id=false 应清成 NULL，实际 IS NULL 的行数 %d", n)
	}
}

// [id, 名称] 与 [] 也是 Odoo 的多对一写法（前者是 name_get 对，后者是空对）。
func TestM2OWrite_PairAndEmptyPair(t *testing.T) {
	o, site1 := setupIsNull(t)

	id := firstId(t, mustCreate(t, o, "isnull.menu", map[string]any{"name": "pair", "site_id": []any{site1, "site1"}}))
	ds, err := o.Model("isnull.menu").Ids(id).Read()
	if err != nil {
		t.Fatal(err)
	}
	if got := ds.Record().FieldByName("site_id").AsInteger(); got != site1 {
		t.Fatalf("[id, 名称] 应取 id=%d，实得 %d", site1, got)
	}

	if _, err := o.Model("isnull.menu").Ids(id).Write(map[string]any{"site_id": []any{}}); err != nil {
		t.Fatalf("write site_id=[]: %v", err)
	}
	if n := m2oNullCount(t, o, "pair"); n != 1 {
		t.Fatalf("site_id=[] 应清成 NULL，实际 IS NULL 的行数 %d", n)
	}
}
