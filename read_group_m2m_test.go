package orm

import (
	"fmt"
	"path/filepath"
	"testing"
)

// ReadGroup 按 many2many 分组：一条记录按它的每个对端各进一组，一个都没有的进空组
// （对齐 Odoo；account 的分录列表「按税格分组」就是这么用的）。此前直接报
// "cannot group by non-stored field"。

type rgmTag struct {
	TModel `table:"name('rgm_tag')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(32) recname()"`
}

type rgmLine struct {
	TModel `table:"name('rgm_line')"`
	Id     int64   `field:"pk autoincr"`
	Name   string  `field:"varchar(32)"`
	Amount float64 `field:"double()"`
	TagIds []any   `field:"many2many(rgm_tag,rgm_line_tag_rel,line_id,tag_id)"`
}

func TestReadGroup_ByMany2Many(t *testing.T) {
	ds := testDataSource(t, filepath.Join(t.TempDir(), "rgm.db"))
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(rgmTag), new(rgmLine)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	a, _ := o.Model("rgm.tag").Create(map[string]any{"name": "A"})
	b, _ := o.Model("rgm.tag").Create(map[string]any{"name": "B"})
	mk := func(name string, amount float64, tags ...any) {
		vals := map[string]any{"name": name, "amount": amount}
		if len(tags) > 0 {
			vals["tag_ids"] = []any{[]any{6, 0, tags}}
		}
		if _, err := o.Model("rgm.line").Create(vals); err != nil {
			t.Fatal(err)
		}
	}
	mk("both", 10, a[0], b[0])
	mk("onlyA", 5, a[0])
	mk("none", 7)

	res, err := o.Model("rgm.line").ReadGroup([]string{"tag_ids"}, []string{"amount"})
	if err != nil {
		t.Fatalf("按 m2m 分组失败: %v", err)
	}
	type g struct {
		count int64
		sum   float64
	}
	got := map[string]g{}
	res.First()
	for !res.Eof() {
		got[res.FieldByName("tag_ids").AsString()] = g{res.FieldByName(GroupCountField).AsInteger(), res.FieldByName("amount").AsFloat()}
		res.Next()
	}
	ka, kb := fmt.Sprint(a[0]), fmt.Sprint(b[0])
	if got[ka] != (g{2, 15}) {
		t.Errorf("A 组应是 2 行合计 15，实得 %+v（全部 %v）", got[ka], got)
	}
	if got[kb] != (g{1, 10}) {
		t.Errorf("B 组应是 1 行合计 10，实得 %+v", got[kb])
	}
	if got[""] != (g{1, 7}) {
		t.Errorf("没有标签的应进空组（1 行合计 7），实得 %+v", got[""])
	}

	// domain 过滤照样生效（子查询里套的是同一套 where）。
	res, err = o.Model("rgm.line").Domain([]any{[]any{"name", "!=", "both"}}).ReadGroup([]string{"tag_ids"}, []string{"amount"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Count() != 2 {
		t.Errorf("过滤掉 both 之后应剩 A 与空两组，实得 %d", res.Count())
	}
}
