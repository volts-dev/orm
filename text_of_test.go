package orm

import (
	"path/filepath"
	"testing"
)

// TextOf（MySQL 兼容 P3，2026-10-25）：上层按「列值的文本」比较时（vectors 扩展字段的重算规则：键是
// 'true' / 'false' / 选项值 / id），三个库给出的文本必须一样。此前 vectors 直接写 `col::text`——MySQL 上是
// 语法错；换成 CAST 也不够：MySQL / SQLite 的布尔列是整数，CAST 出来是 '1' / '0'。

type TextOfRec struct {
	TModel `table:"name('text_of_rec')"`
	Id     int64  `field:"pk autoincr"`
	Flag   bool   `field:"boolean()"`
	Qty    int64  `field:"bigint()"`
	State  string `field:"varchar() size(16)"`
	Note   string `field:"varchar()"`
}

func TestDialect_TextOf(t *testing.T) {
	o, err := New(WithDataSource(testDataSource(t, filepath.Join(t.TempDir(), "textof.db"))))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer o.Close()
	if _, err := o.SyncModel("", new(TextOfRec)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	type row struct {
		vals                    map[string]any
		flag, qty, state, note string
	}
	rows := []row{
		{map[string]any{"flag": true, "qty": 42, "state": "done", "note": "it's"}, "true", "42", "done", "it's"},
		{map[string]any{"flag": false, "qty": -7, "state": "draft", "note": "x"}, "false", "-7", "draft", "x"},
	}
	m, err := o.GetModel("text.of.rec")
	if err != nil {
		t.Fatal(err)
	}
	d := o.Dialect()
	q := d.Quoter()
	expr := func(col string) string {
		return "COALESCE(" + d.TextOf(q.Quote(col), m.GetFieldByName(col)) + ", '')"
	}
	sql := "SELECT " + expr("flag") + " AS f, " + expr("qty") + " AS q, " + expr("state") + " AS s, " + expr("note") +
		" AS n FROM " + q.Quote("text_of_rec") + " WHERE " + q.Quote("id") + " = ?"
	for _, r := range rows {
		ids, err := o.Model("text.of.rec").Create(r.vals)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		s := o.NewSession()
		ds, err := s.Query(sql, ids[0])
		s.Close()
		if err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		got := [4]string{ds.FieldByName("f").AsString(), ds.FieldByName("q").AsString(), ds.FieldByName("s").AsString(), ds.FieldByName("n").AsString()}
		want := [4]string{r.flag, r.qty, r.state, r.note}
		if got != want {
			t.Errorf("%s 上的文本 %q，想要 %q", d.DBType(), got, want)
		}
	}

	// 空值：布尔与整数都是 NULL（外面的 COALESCE 给 ''），不是 'false' / '0'。
	if _, err := o.Exec("INSERT INTO " + q.Quote("text_of_rec") + " (" + q.Quote("note") + ") VALUES ('null row')"); err != nil {
		t.Fatal(err)
	}
	s := o.NewSession()
	defer s.Close()
	ds, err := s.Query("SELECT "+expr("flag")+" AS f, "+expr("qty")+" AS q FROM "+q.Quote("text_of_rec")+" WHERE "+q.Quote("note")+" = ?", "null row")
	if err != nil {
		t.Fatal(err)
	}
	if f, n := ds.FieldByName("f").AsString(), ds.FieldByName("q").AsString(); f != "" || n != "" {
		t.Errorf("空值的文本 %q / %q，想要两个空串", f, n)
	}
}
