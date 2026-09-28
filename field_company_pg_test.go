package orm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	_ "github.com/lib/pq"
	"github.com/volts-dev/utils"
)

type cdProbe struct {
	TModel   `table:"name('cd_probe')"`
	Id       int64  `field:"pk autoincr"`
	Name     string `field:"varchar(32)"`
	ParentId int64  `field:"many2one(cd.probe) company_dependent title('Parent')"`
	Note     string `field:"varchar(64) company_dependent"`
	Level    int64  `field:"int(64) company_dependent"`
}

func setupCompanyOrm(t *testing.T) (*TOrm, string) {
	t.Helper()
	ds := &TDataSource{DbType: "postgres", Host: "localhost", Port: "5432", UserName: "postgres", Password: "postgres", DbName: "test_orm_addfield", SSLMode: "disable"}
	cleaner, err := New(WithDataSource(ds))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	if _, err := cleaner.Exec(`DROP TABLE IF EXISTS public.cd_probe CASCADE`); err != nil {
		t.Fatal(err)
	}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	names, err := o.SyncModel("test", new(cdProbe))
	if err != nil || len(names) != 1 {
		t.Fatalf("SyncModel: %v %v", names, err)
	}
	if err := o.Freeze(context.Background()); err != nil {
		t.Fatal(err)
	}
	return o, names[0]
}

// 同一条记录在不同公司里各有各的值；写一个公司不碰别的公司；没有值时回落到 "0" 那一格。
func TestCompanyDependent_Postgres_PerCompanyReadWrite(t *testing.T) {
	o, m := setupCompanyOrm(t)
	in := func(cid int64) *TSession {
		return o.Model(m).WithContext(WithCompanyId(context.Background(), cid))
	}

	// 列是 jsonb
	ds, err := o.Query(`SELECT column_name, data_type FROM information_schema.columns WHERE table_name='cd_probe' AND column_name IN ('parent_id','note','level') ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	for ds.First(); !ds.Eof(); ds.Next() {
		if dt := ds.FieldByName("data_type").AsString(); dt != "jsonb" {
			t.Fatalf("列 %s 的类型是 %s，应为 jsonb", ds.FieldByName("column_name").AsString(), dt)
		}
	}

	anchor, err := in(1).Create(map[string]any{"name": "anchor"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := in(1).Create(map[string]any{"name": "other"})
	if err != nil {
		t.Fatal(err)
	}
	// 公司 1 新建时就带值
	rec, err := in(1).Create(map[string]any{"name": "rec", "parent_id": anchor[0], "note": "c1", "level": 10})
	if err != nil {
		t.Fatalf("create in company 1: %v", err)
	}

	read := func(cid int64) (parent int64, note string, level int64) {
		t.Helper()
		ds, err := in(cid).Ids(rec...).Read()
		if err != nil || ds.Count() != 1 {
			t.Fatalf("read in company %d: %v (%d rows)", cid, err, ds.Count())
		}
		return ds.FieldByName("parent_id").AsInteger(), ds.FieldByName("note").AsString(), ds.FieldByName("level").AsInteger()
	}

	if p, n, l := read(1); p != toInt64(anchor[0]) || n != "c1" || l != 10 {
		t.Fatalf("公司 1 读回 parent=%d note=%q level=%d", p, n, l)
	}
	if p, n, l := read(2); p != 0 || n != "" || l != 0 {
		t.Fatalf("公司 2 还没设过，应为空，实得 parent=%d note=%q level=%d", p, n, l)
	}

	// 公司 2 写自己的值，公司 1 不受影响
	if _, err := in(2).Ids(rec...).Write(map[string]any{"parent_id": other[0], "note": "c2"}); err != nil {
		t.Fatalf("write in company 2: %v", err)
	}
	if p, n, _ := read(1); p != toInt64(anchor[0]) || n != "c1" {
		t.Fatalf("写公司 2 把公司 1 的值改了: parent=%d note=%q", p, n)
	}
	if p, n, _ := read(2); p != toInt64(other[0]) || n != "c2" {
		t.Fatalf("公司 2 读回 parent=%d note=%q", p, n)
	}

	// 不分公司的默认值："0" 那一格；没设过的公司回落到它，设过的不受影响
	if _, err := in(0).Ids(rec...).Write(map[string]any{"note": "default"}); err != nil {
		t.Fatalf("write default slot: %v", err)
	}
	if _, n, _ := read(3); n != "default" {
		t.Fatalf("公司 3 没设过，应回落到默认值，实得 %q", n)
	}
	if _, n, _ := read(1); n != "c1" {
		t.Fatalf("公司 1 设过，不该回落，实得 %q", n)
	}

	// 写空 = 删掉这一格 → 回落
	if _, err := in(1).Ids(rec...).Write(map[string]any{"note": nil, "parent_id": nil}); err != nil {
		t.Fatalf("clear in company 1: %v", err)
	}
	if p, n, _ := read(1); p != 0 || n != "default" {
		t.Fatalf("公司 1 清空后应回落：parent=%d note=%q", p, n)
	}

	// 库里的原始值：各公司的格子都在
	raw, err := o.Query(`SELECT note::text AS note FROM cd_probe WHERE id = ?`, rec[0])
	if err != nil {
		t.Fatal(err)
	}
	var cells map[string]any
	if err := json.Unmarshal([]byte(raw.FieldByName("note").AsString()), &cells); err != nil {
		t.Fatalf("note 列不是 JSON 对象: %v (%s)", err, raw.FieldByName("note").AsString())
	}
	if cells["2"] != "c2" || cells["0"] != "default" || cells["1"] != nil {
		t.Fatalf("note 列的原始值不对: %v", cells)
	}
}

// 条件、排序、分组都按当前公司那一格算。
func TestCompanyDependent_Postgres_DomainOrderGroup(t *testing.T) {
	o, m := setupCompanyOrm(t)
	in := func(cid int64) *TSession {
		return o.Model(m).WithContext(WithCompanyId(context.Background(), cid))
	}
	ids := map[string]string{}
	for _, row := range []struct {
		name         string
		note1, note2 string
		level1       int64
	}{
		{"a", "banana", "zeta", 3},
		{"b", "apple", "alpha", 1},
		{"c", "", "", 0},
	} {
		vals := map[string]any{"name": row.name}
		if row.note1 != "" {
			vals["note"] = row.note1
			vals["level"] = row.level1
		}
		r, err := in(1).Create(vals)
		if err != nil {
			t.Fatal(err)
		}
		ids[row.name] = toString(r[0])
		if row.note2 != "" {
			if _, err := in(2).Ids(r...).Write(map[string]any{"note": row.note2}); err != nil {
				t.Fatal(err)
			}
		}
	}
	names := func(s *TSession) string {
		t.Helper()
		ds, err := s.Read()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var out []string
		for ds.First(); !ds.Eof(); ds.Next() {
			out = append(out, ds.FieldByName("name").AsString())
		}
		return strings.Join(out, ",")
	}

	cases := []struct {
		cid  int64
		dom  string
		want string
	}{
		{1, `[('note','=','apple')]`, "b"},
		{2, `[('note','=','apple')]`, ""},
		{2, `[('note','=','alpha')]`, "b"},
		{1, `[('note','=',False)]`, "c"},
		{1, `[('note','!=',False)]`, "a,b"},
		{1, `[('note','in',['apple','banana'])]`, "a,b"},
		{1, `[('note','not in',['apple'])]`, "a,c"},
		{1, `[('note','ilike','NAN')]`, "a"},
		{1, `[('level','>',1)]`, "a"},
	}
	for _, c := range cases {
		if got := names(in(c.cid).Domain(c.dom).OrderBy("name")); got != c.want {
			t.Fatalf("公司 %d %s: 得 %q，应为 %q", c.cid, c.dom, got, c.want)
		}
	}

	// 排序：公司 1 按 note 是 apple(b) < banana(a)；公司 2 是 alpha(b) < zeta(a)
	if got := names(in(1).Domain(`[('note','!=',False)]`).OrderBy("note")); got != "b,a" {
		t.Fatalf("公司 1 按 note 排序: %q", got)
	}
	if got := names(in(2).Domain(`[('note','!=',False)]`).OrderBy("note desc")); got != "a,b" {
		t.Fatalf("公司 2 按 note 倒序: %q", got)
	}

	// 分组：按当前公司那一格分
	grp, err := in(2).ReadGroup([]string{"note"}, nil)
	if err != nil {
		t.Fatalf("ReadGroup: %v", err)
	}
	var keys []string
	for grp.First(); !grp.Eof(); grp.Next() {
		keys = append(keys, grp.FieldByName("note").AsString())
	}
	if strings.Join(keys, ",") != ",alpha,zeta" && strings.Join(keys, ",") != "alpha,zeta," {
		t.Fatalf("公司 2 按 note 分组的键: %v", keys)
	}
}

func toInt64(v any) int64 { return utils.ToInt64(v) }

func toString(v any) string { return utils.ToString(v) }
