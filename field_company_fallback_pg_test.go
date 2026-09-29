package orm

import (
	"context"
	"strings"
	"testing"

	_ "github.com/lib/pq"
)

// 宿主给的默认值（Odoo 18 起 company_dependent 的 ir.default）：多对一也能兜底、按公司分，
// 条件 / 排序都按兜底后的值算；字段的静态默认值排在它后面；与类型对不上的值宁可不用。
type cdFbProbe struct {
	TModel  `table:"name('cd_fb_probe')"`
	Id      int64  `field:"pk autoincr"`
	Name    string `field:"varchar(32)"`
	Carrier int64  `field:"many2one(cd.fb.probe) company_dependent"`
	Days    int64  `field:"int(64) company_dependent default(1)"`
}

func TestCompanyDependent_Postgres_HostFallback(t *testing.T) {
	ds := &TDataSource{DbType: "postgres", Host: "localhost", Port: "5432", UserName: "postgres", Password: "postgres", DbName: "test_orm_addfield", SSLMode: "disable"}
	cleaner, err := New(WithDataSource(ds))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	_, err = cleaner.Exec(`DROP TABLE IF EXISTS public.cd_fb_probe CASCADE`)
	cleaner.Close()
	if err != nil {
		t.Fatal(err)
	}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	t.Cleanup(func() { o.Close() })
	names, err := o.SyncModel("test", new(cdFbProbe))
	if err != nil || len(names) != 1 {
		t.Fatalf("SyncModel: %v %v", names, err)
	}
	if err := o.Freeze(context.Background()); err != nil {
		t.Fatal(err)
	}
	m := names[0]

	// 宿主的默认值：carrier 在公司 1 是 77、别的公司 70；days 在公司 2 是 "oops"（坏值，不用）。
	var asked []string
	fb := CompanyFallback(func(model, field string, cid int64) (string, bool) {
		asked = append(asked, model+"."+field)
		switch field {
		case "carrier":
			if cid == 1 {
				return "77", true
			}
			return "70", true
		case "days":
			if cid == 2 {
				return "oops", true
			}
			if cid == 3 {
				return "9", true
			}
		}
		return "", false
	})
	in := func(cid int64, withFb bool) *TSession {
		ctx := WithCompanyId(context.Background(), cid)
		if withFb {
			ctx = WithCompanyFallback(ctx, fb)
		}
		return o.Model(m).WithContext(ctx)
	}
	// 没设过的行：绕过 orm 插（orm 新建时会把静态默认值落进当前公司那一格）
	if _, err := o.Exec(`INSERT INTO cd_fb_probe (name) VALUES ('a'), ('b')`); err != nil {
		t.Fatal(err)
	}
	if _, err := in(1, false).Create(map[string]any{"name": "own", "carrier": 5}); err != nil {
		t.Fatal(err)
	}
	read := func(s *TSession, name string) (int64, int64) {
		t.Helper()
		ds, err := s.Domain(`[('name','=','` + name + `')]`).Read()
		if err != nil || ds.Count() != 1 {
			t.Fatalf("read %s: %v", name, err)
		}
		return ds.FieldByName("carrier").AsInteger(), ds.FieldByName("days").AsInteger()
	}

	if c, d := read(in(1, true), "a"); c != 77 || d != 1 {
		t.Fatalf("公司 1 没设过：carrier 应是宿主给的 77、days 回落静态 1，实得 %d %d", c, d)
	}
	if c, _ := read(in(2, true), "a"); c != 70 {
		t.Fatalf("公司 2：carrier 应是 70，实得 %d", c)
	}
	if c, _ := read(in(1, true), "own"); c != 5 {
		t.Fatalf("公司 1 自己设过 5，不该被默认值盖掉，实得 %d", c)
	}
	if c, _ := read(in(2, true), "own"); c != 70 {
		t.Fatalf("公司 2 没设过（5 只在公司 1 那一格），应是 70，实得 %d", c)
	}
	if _, d := read(in(2, true), "a"); d != 1 {
		t.Fatalf("公司 2 的 days 默认值是坏值，应不用它、回落静态 1，实得 %d", d)
	}
	if _, d := read(in(3, true), "a"); d != 9 {
		t.Fatalf("公司 3：宿主的默认值排在静态默认值前面，应是 9，实得 %d", d)
	}
	if c, _ := read(in(1, false), "a"); c != 0 {
		t.Fatalf("没挂宿主来源：多对一没有默认值，应是 0，实得 %d", c)
	}

	// 条件与排序也按兜底后的值
	got := func(s *TSession, domain string) string {
		t.Helper()
		ds, err := s.Domain(domain).OrderBy("name").Read()
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for ds.First(); !ds.Eof(); ds.Next() {
			out = append(out, ds.FieldByName("name").AsString())
		}
		return strings.Join(out, ",")
	}
	if g := got(in(1, true), `[('carrier','=',77)]`); g != "a,b" {
		t.Fatalf("公司 1 carrier = 77 应命中没设过的 a,b：%s", g)
	}
	if g := got(in(1, true), `[('carrier','=',False)]`); g != "" {
		t.Fatalf("有默认值时没有「空」的行：%s", g)
	}
	if g := got(in(1, false), `[('carrier','=',False)]`); g != "a,b" {
		t.Fatalf("没挂宿主来源时 a,b 是空的：%s", g)
	}
	// 写入等于兜底值：那一格不存（Odoo 19 写 company_dependent 时剔掉等于兜底值的格子）
	rawOf := func(name string) string {
		t.Helper()
		ds, err := o.Query(`SELECT COALESCE(carrier::text, '') AS c FROM cd_fb_probe WHERE name = ?`, name)
		if err != nil || ds.Count() != 1 {
			t.Fatalf("raw %s: %v", name, err)
		}
		return ds.FieldByName("c").AsString()
	}
	bIds, err := in(1, true).Domain(`[('name','=','b')]`).Read()
	if err != nil || bIds.Count() != 1 {
		t.Fatal(err)
	}
	bId := bIds.FieldByName("id").AsInteger()
	if _, err := in(1, true).Ids(bId).Write(map[string]any{"carrier": 5}); err != nil {
		t.Fatal(err)
	}
	if r := rawOf("b"); !strings.Contains(r, `"1": 5`) {
		t.Fatalf("写 5 应存进公司 1 那一格：%s", r)
	}
	if _, err := in(1, true).Ids(bId).Write(map[string]any{"carrier": 77}); err != nil {
		t.Fatal(err)
	}
	if r := rawOf("b"); strings.Contains(r, `"1"`) {
		t.Fatalf("写回等于默认值的 77：公司 1 那一格应剔掉，实得 %s", r)
	}
	if c, _ := read(in(1, true), "b"); c != 77 {
		t.Fatalf("剔掉之后读到的仍是默认值 77，实得 %d", c)
	}
	if _, err := in(2, true).Ids(bId).Write(map[string]any{"carrier": 77}); err != nil {
		t.Fatal(err)
	}
	if r := rawOf("b"); !strings.Contains(r, `"2": 77`) {
		t.Fatalf("公司 2 的默认值是 70，写 77 是一个真值，应存下：%s", r)
	}
	if _, err := in(1, true).Create(map[string]any{"name": "new", "carrier": 77}); err != nil {
		t.Fatal(err)
	}
	if r := rawOf("new"); r != "" {
		t.Fatalf("新建时给的就是默认值：这一列不该存，实得 %s", r)
	}
	if !strings.Contains(strings.Join(asked, " "), "cd.fb.probe.carrier") && !strings.Contains(strings.Join(asked, " "), "carrier") {
		t.Fatalf("宿主来源没被问到模型名.字段名：%v", asked)
	}
}
