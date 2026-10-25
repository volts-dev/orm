package orm

import (
	"fmt"
	"path/filepath"
	"testing"
)

// ReadGroup 按 many2one 分组：未设置的关系（NULL / 0 / -1 三种落库形态，见 isEmptyRelation）
// 必须进**同一个**空组。
//
// 此前按原值分组，一个"没有工作区"变成了两三组，而每组的 __domain 都是 `('x','=',False)`
// （isEmptyRelation 认全部三种）、前端按同一个空键记格子 —— 2026-10-26 文档「存储」透视表
// 真栈上撞到：两行空标签，第二行的总计是 0、各格却重复画着第一行的数。

type rgoParent struct {
	TModel `table:"name('rgo_parent')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(32) recname()"`
}

type rgoLine struct {
	TModel   `table:"name('rgo_line')"`
	Id       int64   `field:"pk autoincr"`
	Name     string  `field:"varchar(32)"`
	Amount   float64 `field:"double()"`
	ParentId int64   `field:"many2one(rgo_parent)"`
}

func TestReadGroup_Many2OneEmptyFormsShareOneGroup(t *testing.T) {
	ds := testDataSource(t, filepath.Join(t.TempDir(), "rgo.db"))
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(rgoParent), new(rgoLine)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	p, err := o.Model("rgo.parent").Create(map[string]any{"name": "P"})
	if err != nil {
		t.Fatal(err)
	}
	mk := func(name string, amount float64) {
		if _, err := o.Model("rgo.line").Create(map[string]any{"name": name, "amount": amount}); err != nil {
			t.Fatal(err)
		}
	}
	mk("null", 1)
	mk("zero", 2)
	mk("minus", 4)
	mk("real", 8)
	// 三种空形态直接落库：写路径怎么归一不是这条用例关心的，库里**已经有**这三种。
	if _, err := o.Exec("UPDATE rgo_line SET parent_id = NULL WHERE name = 'null'"); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Exec("UPDATE rgo_line SET parent_id = 0 WHERE name = 'zero'"); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Exec("UPDATE rgo_line SET parent_id = -1 WHERE name = 'minus'"); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Exec(fmt.Sprintf("UPDATE rgo_line SET parent_id = %v WHERE name = 'real'", p[0])); err != nil {
		t.Fatal(err)
	}

	res, err := o.Model("rgo.line").ReadGroup([]string{"parent_id"}, []string{"amount"})
	if err != nil {
		t.Fatalf("按 m2o 分组失败: %v", err)
	}
	type g struct {
		count int64
		sum   float64
	}
	var groups []string
	got := map[string]g{}
	res.First()
	for !res.Eof() {
		k := res.FieldByName("parent_id").AsString()
		groups = append(groups, k)
		got[k] = g{res.FieldByName(GroupCountField).AsInteger(), res.FieldByName("amount").AsFloat()}
		res.Next()
	}
	if len(groups) != 2 {
		t.Fatalf("应当正好两组（P + 一个空组），实得 %d 组：%q（%v）", len(groups), groups, got)
	}
	if got[fmt.Sprint(p[0])] != (g{1, 8}) {
		t.Errorf("P 组应是 1 行合计 8，实得 %+v（全部 %v）", got[fmt.Sprint(p[0])], got)
	}
	empty := g{}
	for _, k := range groups {
		if k != fmt.Sprint(p[0]) {
			empty = got[k]
		}
	}
	if empty != (g{3, 7}) {
		t.Errorf("NULL / 0 / -1 应并成一个空组（3 行合计 7），实得 %+v（全部 %v）", empty, got)
	}
}
