package orm

import (
	"context"
	"strings"
	"testing"

	_ "github.com/lib/pq"
)

// 带静态默认值的按公司区分字段（Odoo：purchase 的 reminder_date_before_receipt 默认 1）。
type cdDefProbe struct {
	TModel `table:"name('cd_def_probe')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(32)"`
	Days   int64  `field:"int(64) company_dependent default(1)"`
	Flag   bool   `field:"bool() company_dependent default(true)"`
	Kind   string `field:"varchar(16) company_dependent default('receive')"`
}

func cdDefOrm(t *testing.T, reset bool) (*TOrm, string) {
	t.Helper()
	ds := &TDataSource{DbType: "postgres", Host: "localhost", Port: "5432", UserName: "postgres", Password: "postgres", DbName: "test_orm_addfield", SSLMode: "disable"}
	if reset {
		cleaner, err := New(WithDataSource(ds))
		if err != nil {
			t.Skipf("postgres unavailable: %v", err)
		}
		_, err = cleaner.Exec(`DROP TABLE IF EXISTS public.cd_def_probe CASCADE`)
		cleaner.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	t.Cleanup(func() { o.Close() })
	names, err := o.SyncModel("test", new(cdDefProbe))
	if err != nil || len(names) != 1 {
		t.Fatalf("SyncModel: %v %v", names, err)
	}
	if err := o.Freeze(context.Background()); err != nil {
		t.Fatal(err)
	}
	return o, names[0]
}

func cdColumnDefaults(t *testing.T, o *TOrm) string {
	t.Helper()
	ds, err := o.Query(`SELECT column_name, COALESCE(column_default, '') AS d FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'cd_def_probe' AND column_name IN ('days','flag','kind','cap') ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for ds.First(); !ds.Eof(); ds.Next() {
		if d := ds.FieldByName("d").AsString(); d != "" {
			out = append(out, ds.FieldByName("column_name").AsString()+"="+d)
		}
	}
	return strings.Join(out, ",")
}

// 默认值是读的时候兜底，不是列默认值：jsonb 列上的 DEFAULT 1 连表都建不出来。
// 没设过的格子（包括建列前就有的行）读出来是默认值；显式设成 0 / false 的不回落。
func TestCompanyDependent_Postgres_StaticDefaultIsReadFallback(t *testing.T) {
	o, m := cdDefOrm(t, true)
	if d := cdColumnDefaults(t, o); d != "" {
		t.Fatalf("按公司区分的列不该有列默认值: %s", d)
	}
	in := func(cid int64) *TSession {
		return o.Model(m).WithContext(WithCompanyId(context.Background(), cid))
	}

	// 绕过 orm 直接插的行：三格全是 NULL
	if _, err := o.Exec(`INSERT INTO cd_def_probe (name) VALUES ('raw')`); err != nil {
		t.Fatal(err)
	}
	made, err := in(1).Create(map[string]any{"name": "made"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = in(1).Create(map[string]any{"name": "set", "days": 5, "flag": false, "kind": "purchase"})
	if err != nil {
		t.Fatal(err)
	}

	type row struct {
		days int64
		flag bool
		kind string
	}
	read := func(cid int64, name string) row {
		t.Helper()
		ds, err := in(cid).Domain(`[('name','=','` + name + `')]`).Read()
		if err != nil || ds.Count() != 1 {
			t.Fatalf("read %s in company %d: %v", name, cid, err)
		}
		return row{ds.FieldByName("days").AsInteger(), ds.FieldByName("flag").AsBoolean(), ds.FieldByName("kind").AsString()}
	}
	def := row{1, true, "receive"}
	for _, cid := range []int64{0, 1, 2} {
		for _, name := range []string{"raw", "made"} {
			if got := read(cid, name); got != def {
				t.Fatalf("公司 %d 的 %s 应读到默认值 %+v，实得 %+v", cid, name, def, got)
			}
		}
	}
	if got := read(1, "set"); got != (row{5, false, "purchase"}) {
		t.Fatalf("公司 1 设过的值: %+v", got)
	}
	if got := read(2, "set"); got != def {
		t.Fatalf("公司 2 没设过，应回落到默认值: %+v", got)
	}

	// 显式设成 0：是一个值，不回落
	if _, err := in(1).Ids(made...).Write(map[string]any{"days": 0}); err != nil {
		t.Fatal(err)
	}
	if got := read(1, "made").days; got != 0 {
		t.Fatalf("公司 1 显式设了 0，读回 %d", got)
	}
	if got := read(2, "made").days; got != 1 {
		t.Fatalf("公司 2 没设过，读回 %d", got)
	}

	// 条件也按兜底后的值算：days = 1 命中没设过的行
	ds, err := in(1).Domain(`[('days','=',1)]`).OrderBy("name").Read()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for ds.First(); !ds.Eof(); ds.Next() {
		names = append(names, ds.FieldByName("name").AsString())
	}
	if strings.Join(names, ",") != "raw" {
		t.Fatalf("公司 1 days = 1: %v", names)
	}
	ds, err = in(1).Domain(`[('flag','=',False)]`).Read()
	if err != nil || ds.Count() != 1 || ds.FieldByName("name").AsString() != "set" {
		t.Fatalf("公司 1 flag = False 应只命中 set: %v %d", err, ds.Count())
	}

	// 再同步一次（重启）：不去给列补默认值
	o2, _ := cdDefOrm(t, false)
	if d := cdColumnDefaults(t, o2); d != "" {
		t.Fatalf("重启同步后列上多了默认值: %s", d)
	}
}

// 运行期加一个带默认值的按公司区分字段：建列不报错，已有的行读出来就是默认值。
func TestCompanyDependent_Postgres_AddFieldWithDefault(t *testing.T) {
	o, m := cdDefOrm(t, true)
	rec, err := o.Model(m).Create(map[string]any{"name": "old"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.AddField(m, "cap", "int(64) company_dependent default(7)"); err != nil {
		t.Fatalf("AddField: %v", err)
	}
	if d := cdColumnDefaults(t, o); d != "" {
		t.Fatalf("列上不该有默认值: %s", d)
	}
	ds, err := o.Model(m).WithContext(WithCompanyId(context.Background(), 3)).Ids(rec...).Read()
	if err != nil || ds.Count() != 1 {
		t.Fatalf("read: %v", err)
	}
	if got := ds.FieldByName("cap").AsInteger(); got != 7 {
		t.Fatalf("建列前就有的行应读到默认值 7，实得 %d", got)
	}
}
