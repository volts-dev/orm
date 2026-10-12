package orm

import (
	"path/filepath"
	"testing"

	"github.com/volts-dev/orm/domain"
)

/*
`auto_join` 标签（Odoo 的 `auto_join=True`）的回归。

product 一族照 Odoo 在 product_template_id 上写着 `auto_join`，ORM 不认，每次注册模型都打
一条 `Couldn't handle unknown tag < auto_join > on pro.variant@product_template_id`
（真栈 2026-10-11：三个模型各 551 次，日志中心里常年挂着）。

auto_join 在 Odoo 里只是**性能提示**（`a_id.name` 这种穿过 m2o 的 domain 用 JOIN 而不是子
查询），结果集不变。本仓穿过 m2o 的路径走子查询、已有回归测试兜着；expr.go 里那条
IsAutoJoin 的 JOIN 分支从未被启用、也没有测试（它拿的 comodel 是字段**所属**模型，见那里
m2o 子查询分支的注释）。所以标签**认下但不启用**：不再报未知标签，行为一字不变。
*/

type (
	AjParent struct {
		TModel `table:"name('aj_parent')"`
		Id     int64  `field:"pk autoincr title('ID')"`
		Name   string `field:"varchar() size(64)"`
	}
	AjChild struct {
		TModel   `table:"name('aj_child')"`
		Id       int64  `field:"pk autoincr title('ID')"`
		Name     string `field:"varchar() size(64)"`
		ParentId int64  `field:"many2one(aj_parent) required ondelete('cascade') auto_join"`
	}
)

func TestTagAutoJoin_KnownAndInert(t *testing.T) {
	if GetTagControllerByName("auto_join") == nil {
		t.Fatal("auto_join 没有标签处理器：每个写了它的字段注册时都会报 unknown tag")
	}

	ds := &TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "aj.db")}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.SyncModel("", new(AjParent), new(AjChild)); err != nil {
		t.Fatal(err)
	}
	model, err := o.GetModel("aj.child")
	if err != nil {
		t.Fatal(err)
	}
	f := model.GetFieldByName("parent_id")
	if f == nil {
		t.Fatal("parent_id 字段没建出来")
	}
	if f.IsAutoJoin() {
		t.Fatal("auto_join 不该启用 JOIN 分支（未经验证），只应被静默接受")
	}

	p1 := firstId(t, mustCreate(t, o, "aj.parent", map[string]any{"name": "alpha"}))
	p2 := firstId(t, mustCreate(t, o, "aj.parent", map[string]any{"name": "beta"}))
	mustCreate(t, o, "aj.child", map[string]any{"name": "c1", "parent_id": p1})
	mustCreate(t, o, "aj.child", map[string]any{"name": "c2", "parent_id": p2})

	got, err := o.Model("aj.child").Domain(domain.New("parent_id.name", "=", "beta")).Limit(-1).Read()
	if err != nil {
		t.Fatal(err)
	}
	if got.Count() != 1 || got.Record().FieldByName("name").AsString() != "c2" {
		t.Fatalf("parent_id.name = beta 应只回 c2，实得 %d 条", got.Count())
	}
}
