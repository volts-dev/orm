package orm

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// RwWord 的列名全是 MySQL 8 的保留字（INFORMATION_SCHEMA.KEYWORDS RESERVED=1），而 vectors 各模块的模型里真有这些列：
// sys_view.key、sys_default.condition、stock_location.usage、res_partner.function、loyalty_program.trigger、
// sys_tenant_archive.rows……PG 上都不是保留字，裸写没事；MySQL 上不加引号整条 1064。
type RwWord struct {
	TModel     `table:"name('rw_word')"`
	Id         int64  `field:"pk autoincr title('ID')"`
	Key        string `field:"varchar() index"`
	Rows       int64  `field:"int()"`
	Condition  string `field:"varchar()"`
	Usage      string `field:"varchar()"`
	Function   string `field:"varchar()"`
	Trigger    string `field:"varchar()"`
	Release    string `field:"varchar()"`
	Groups     string `field:"varchar()"`
	Grouping   string `field:"varchar()"`
	Lines      string `field:"varchar()"`
	Keys       string `field:"varchar()"`
	References string `field:"varchar()"`
}

// ORM 自己拼的每一条 SQL（建表 / 建索引、增删改、domain、排序、选列、分组、求和）在列名是保留字时三个方言都能跑
// （MySQL 兼容，2026-10-26）。手写 SQL 那一侧由 vectors 的 TestRawSQL_RunsOnMySQL 静态守。
func TestReservedColumnNames_OnEveryDialect(t *testing.T) {
	o, err := New(WithDataSource(testDataSource(t, filepath.Join(t.TempDir(), "rw.db"))))
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	if _, err := o.SyncModel("", new(RwWord)); err != nil {
		t.Fatalf("建表: %v", err)
	}
	// 再同步一次：列与索引的反查 / 比对也要认得这些名字（不该再 ALTER）。
	if _, err := o.SyncModel("", new(RwWord)); err != nil {
		t.Fatalf("再同步: %v", err)
	}
	row := func(key string, rows int64, usage string) map[string]any {
		return map[string]any{"key": key, "rows": rows, "condition": "c" + key, "usage": usage, "function": "f",
			"trigger": "t", "release": "r", "groups": "g", "grouping": "gp", "lines": "l", "keys": "k", "references": "ref"}
	}
	var ids []any
	for _, r := range []map[string]any{row("a", 1, "internal"), row("b", 2, "internal"), row("c", 4, "view")} {
		id, err := o.Model("rw.word").Create(r)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		ids = append(ids, id)
	}
	if _, err := o.Model("rw.word").Ids(ids[0]).Write(map[string]any{"key": "a2", "rows": 8, "trigger": "t2"}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	keys := func(ds *TDataset) string {
		var out []string
		for ds.First(); !ds.Eof(); ds.Next() {
			out = append(out, ds.FieldByName("key").AsString())
		}
		return strings.Join(out, " ")
	}
	ds, err := o.Model("rw.word").Domain(`[("usage","=","internal"),("rows",">",1)]`).OrderBy("key desc").Limit(-1).Read()
	if err != nil {
		t.Fatalf("Domain + OrderBy: %v", err)
	}
	if got := keys(ds); got != "b a2" {
		t.Errorf("Domain usage=internal, rows>1 按 key 倒序：得 %q，want \"b a2\"", got)
	}
	ds, err = o.Model("rw.word").Select("key", "rows", "condition", "trigger").Domain(`[("key","in",["a2","c"])]`).OrderBy("rows").Limit(-1).Read()
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if got := keys(ds); got != "c a2" {
		t.Errorf("Select + key in：得 %q，want \"c a2\"", got)
	}
	if ds.First(); ds.FieldByName("condition").AsString() != "cc" {
		t.Errorf("condition 读回 %q", ds.FieldByName("condition").AsString())
	}
	if n, err := o.Model("rw.word").Domain(`[("trigger","=","t2")]`).Count(); err != nil || n != 1 {
		t.Errorf("Count trigger=t2：%d, %v", n, err)
	}
	if s, err := o.Model("rw.word").Domain(`[("usage","=","internal")]`).Sum("rows"); err != nil || s != 10 {
		t.Errorf("Sum rows：%v, %v", s, err)
	}
	g, err := o.Model("rw.word").ReadGroup([]string{"usage"}, []string{"rows"})
	if err != nil {
		t.Fatalf("ReadGroup: %v", err)
	}
	var groups []string
	for g.First(); !g.Eof(); g.Next() {
		groups = append(groups, g.FieldByName("usage").AsString()+"="+g.FieldByName("rows").AsString())
	}
	sort.Strings(groups)
	if got := strings.Join(groups, " "); got != "internal=10 view=4" {
		t.Errorf("ReadGroup usage / sum(rows)：得 %q", got)
	}
	if _, err := o.Model("rw.word").Domain(`[("key","=","b")]`).Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if n, err := o.Model("rw.word").Count(); err != nil || n != 2 {
		t.Errorf("删后 Count：%d, %v", n, err)
	}
}
