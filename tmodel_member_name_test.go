package orm

import (
	"path/filepath"
	"testing"
)

/*
成员名里碰巧含 "tmodel" 的普通字段不是表声明。

_mapping 原先用 `strings.Contains(lower(成员名), "tmodel")` 认内嵌的 TModel，于是
AliasParentModel（alias-paren-t-model）、ObjectModel（objec-t-model）这类 Odoo 原名
字段被当成了表级 tag 的载体：字段不建、列不建，`varchar()` / `title(...)` 被当成
`table_varchar` / `table_title` 报 unknown tag（真栈：mail.alias.alias_parent_model、
fetchmail.server.object_model，各 555 次）。表单上那一格保存后永远是空的，零报错。
*/

type TmnHost struct {
	TModel           `table:"name('tmn_host')"`
	Id               int64  `field:"pk autoincr"`
	AliasParentModel string `field:"varchar() title('Parent Model')"`
	ObjectModel      string `field:"varchar() title('Create a New Record')"`
	DefaultModel     string `field:"varchar(32)"`
}

func TestMapping_MemberNameContainingTModelIsAField(t *testing.T) {
	ds := testDataSource(t, filepath.Join(t.TempDir(), "tmn.db"))
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.SyncModel("", new(TmnHost)); err != nil {
		t.Fatal(err)
	}
	m, err := o.GetModel("tmn.host")
	if err != nil {
		t.Fatal(err)
	}
	// 表名仍来自内嵌 TModel 的 table:"name(...)"——修法不能把真正的表声明也认丢了。
	if m.Table() != "tmn_host" {
		t.Fatalf("表名 = %q，内嵌 TModel 的 table 标签没生效", m.Table())
	}
	for _, name := range []string{"alias_parent_model", "object_model", "default_model"} {
		f := m.GetFieldByName(name)
		if f == nil {
			t.Fatalf("%s 没建出来：成员名含 tmodel 的普通字段被当成了表声明", name)
		}
		if !f.Store() {
			t.Fatalf("%s 应是存储列", name)
		}
	}
	if got := m.GetFieldByName("alias_parent_model").Label(); got != "Parent Model" {
		t.Fatalf("alias_parent_model 的 title = %q，字段 tag 没按字段处理", got)
	}

	// 写进去读得回来——这正是表单上那两格此前做不到的事。
	id := firstId(t, mustCreate(t, o, "tmn.host", map[string]any{
		"alias_parent_model": "project.project",
		"object_model":       "crm.lead",
	}))
	got, err := o.Model("tmn.host").Ids(id).Read()
	if err != nil {
		t.Fatal(err)
	}
	rec := got.Record()
	if rec.FieldByName("alias_parent_model").AsString() != "project.project" ||
		rec.FieldByName("object_model").AsString() != "crm.lead" {
		t.Fatalf("读回 alias_parent_model=%q object_model=%q",
			rec.FieldByName("alias_parent_model").AsString(), rec.FieldByName("object_model").AsString())
	}
}
