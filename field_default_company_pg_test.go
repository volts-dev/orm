package orm

import (
	"context"
	"testing"

	_ "github.com/lib/pq"
)

type dcProbe struct {
	TModel    `table:"name('dc_probe')"`
	Id        int64  `field:"pk autoincr"`
	Name      string `field:"varchar(32)"`
	CompanyId int64  `field:"int(64) default_company title('Company')"`
	// 对照：没有 tag 的共享字段，新建时谁也不替它填。
	OwnerCompanyId int64 `field:"int(64) title('Owner Company')"`
}

func setupDefaultCompanyOrm(t *testing.T) (*TOrm, string) {
	t.Helper()
	ds := &TDataSource{DbType: "postgres", Host: "localhost", Port: "5432", UserName: "postgres", Password: "postgres", DbName: "test_orm_addfield", SSLMode: "disable"}
	cleaner, err := New(WithDataSource(ds))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	_, err = cleaner.Exec(`DROP TABLE IF EXISTS public.dc_probe CASCADE`)
	cleaner.Close()
	if err != nil {
		t.Fatal(err)
	}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	// 本包测试各自开连接池、大多不关；真栈跑着时再多几个就撞上 max_connections。
	t.Cleanup(func() { o.Close() })
	names, err := o.SyncModel("test", new(dcProbe))
	if err != nil || len(names) != 1 {
		t.Fatalf("SyncModel: %v %v", names, err)
	}
	if err := o.Freeze(context.Background()); err != nil {
		t.Fatal(err)
	}
	return o, names[0]
}

// 没给 → 当前公司；显式给空 → 保留（共享记录）；显式给值 → 照给的；没有当前公司 → 不填。
func TestDefaultCompany_Postgres_Create(t *testing.T) {
	o, m := setupDefaultCompanyOrm(t)
	create := func(ctx context.Context, vals map[string]any) (company, owner int64) {
		t.Helper()
		ids, err := o.Model(m).WithContext(ctx).Create(vals)
		if err != nil {
			t.Fatalf("create %v: %v", vals, err)
		}
		ds, err := o.Query(`SELECT COALESCE(company_id, 0) AS company_id, COALESCE(owner_company_id, 0) AS owner_company_id FROM dc_probe WHERE id = ?`, ids[0])
		if err != nil {
			t.Fatal(err)
		}
		return ds.FieldByName("company_id").AsInteger(), ds.FieldByName("owner_company_id").AsInteger()
	}
	in5 := WithCompanyId(context.Background(), 5)

	if c, owner := create(in5, map[string]any{"name": "absent"}); c != 5 || owner != 0 {
		t.Fatalf("没给公司：company_id 应为当前公司 5、没 tag 的字段不填，实得 %d / %d", c, owner)
	}
	if c, _ := create(in5, map[string]any{"name": "nil", "company_id": nil}); c != 5 {
		t.Fatalf("给 nil 等于没给，应为 5，实得 %d", c)
	}
	if c, _ := create(in5, map[string]any{"name": "shared", "company_id": false}); c > 0 {
		t.Fatalf("显式给 false 是要共享记录，不该被填成 %d", c)
	}
	if c, _ := create(in5, map[string]any{"name": "zero", "company_id": 0}); c > 0 {
		t.Fatalf("显式给 0 是要共享记录，不该被填成 %d", c)
	}
	if c, _ := create(in5, map[string]any{"name": "given", "company_id": 7}); c != 7 {
		t.Fatalf("显式给了 7，应为 7，实得 %d", c)
	}
	if c, _ := create(context.Background(), map[string]any{"name": "nobody"}); c > 0 {
		t.Fatalf("没有当前公司时不该填，实得 %d", c)
	}
	lookup := WithCompanyLookup(context.Background(), func() int64 { return 9 })
	if c, _ := create(lookup, map[string]any{"name": "lookup"}); c != 9 {
		t.Fatalf("没有显式公司时应取惰性来源给的 9，实得 %d", c)
	}
}

// 打开新记录表单（DefaultGet）时同样带出当前公司——取的是这个模型实例的 context。
func TestDefaultCompany_Postgres_DefaultGet(t *testing.T) {
	o, m := setupDefaultCompanyOrm(t)
	model, err := o.GetModel(m)
	if err != nil {
		t.Fatal(err)
	}
	model.Ctx(WithCompanyId(context.Background(), 5))
	vals, err := model.DefaultGet("company_id", "owner_company_id")
	if err != nil {
		t.Fatal(err)
	}
	if got := toInt64(vals["company_id"]); got != 5 {
		t.Fatalf("DefaultGet company_id 应为当前公司 5，实得 %v", vals["company_id"])
	}
	if got := toInt64(vals["owner_company_id"]); got > 0 {
		t.Fatalf("没 tag 的字段不该有默认值，实得 %v", vals["owner_company_id"])
	}
	if !IsDefaultCompany(model.GetFieldByName("company_id")) || IsDefaultCompany(model.GetFieldByName("owner_company_id")) {
		t.Fatal("IsDefaultCompany 认错了字段")
	}
}

// 只允许多对一或整数字段：写在文本字段上是声明错误，不许静默当成没写。
func TestDefaultCompany_RejectsNonRelationalField(t *testing.T) {
	type bad struct {
		TModel `table:"name('dc_bad')"`
		Id     int64  `field:"pk autoincr"`
		Name   string `field:"varchar(32) default_company"`
	}
	ds := &TDataSource{DbType: "postgres", Host: "localhost", Port: "5432", UserName: "postgres", Password: "postgres", DbName: "test_orm_addfield", SSLMode: "disable"}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	defer o.Close()
	if _, err := o.SyncModel("test", new(bad)); err == nil {
		t.Fatal("default_company 写在 varchar 上应报错")
	}
}

// 默认值函数不是列默认值：建表不许带 DEFAULT，重启再同步一次也不许报错。
// 曾经：IsDefaultEmpty 把默认值函数也算作「有默认值」，建表落成 `company_id bigint DEFAULT 0`；
// 下一次启动同步时静态默认值（nil）与库里的 "0" 对不上，生成 `SET DEFAULT `（空值）→ 语法错，
// 这个模型的同步整个失败。
func TestDefaultCompany_Postgres_NoColumnDefaultAndResyncs(t *testing.T) {
	o, _ := setupDefaultCompanyOrm(t)
	ds, err := o.Query(`SELECT COALESCE(column_default, '') AS d FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'dc_probe' AND column_name = 'company_id'`)
	if err != nil || ds.Count() != 1 {
		t.Fatalf("query column default: %v", err)
	}
	if d := ds.FieldByName("d").AsString(); d != "" {
		t.Fatalf("company_id 不该有列默认值（默认值函数在应用层求值），实得 %q", d)
	}

	src := &TDataSource{DbType: "postgres", Host: "localhost", Port: "5432", UserName: "postgres", Password: "postgres", DbName: "test_orm_addfield", SSLMode: "disable"}
	again, err := New(WithDataSource(src))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	defer again.Close()
	if _, err := again.SyncModel("test", new(dcProbe)); err != nil {
		t.Fatalf("表已存在时再同步一次（即重启）报错: %v", err)
	}
}
