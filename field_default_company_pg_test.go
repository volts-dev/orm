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
	// 真实形态：many2one。关系字段在 orm 里走另一条写路径（_todoCompute / OnWrite）。
	RefCompanyId int64 `field:"many2one(dc.probe) default_company title('Ref Company')"`
	// 必填的多对一：显式给空也补默认值（空值本来就存不进去）。
	ReqCompanyId int64 `field:"many2one(dc.probe) default_company title('Required Company')"`
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
	ref := func(ctx context.Context, vals map[string]any) int64 {
		t.Helper()
		ids, err := o.Model(m).WithContext(ctx).Create(vals)
		if err != nil {
			t.Fatalf("create %v: %v", vals, err)
		}
		ds, err := o.Query(`SELECT COALESCE(ref_company_id, 0) AS v FROM dc_probe WHERE id = ?`, ids[0])
		if err != nil {
			t.Fatal(err)
		}
		return ds.FieldByName("v").AsInteger()
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
	// many2one 同样的四条
	if v := ref(in5, map[string]any{"name": "m2o-absent"}); v != 5 {
		t.Fatalf("many2one 没给：应为当前公司 5，实得 %d", v)
	}
	if v := ref(in5, map[string]any{"name": "m2o-false", "ref_company_id": false}); v > 0 {
		t.Fatalf("many2one 显式给 false 是要共享记录，不该被填成 %d", v)
	}
	if v := ref(in5, map[string]any{"name": "m2o-zero", "ref_company_id": int64(0)}); v > 0 {
		t.Fatalf("many2one 显式给 0 是要共享记录，不该被填成 %d", v)
	}
	if v := ref(in5, map[string]any{"name": "m2o-given", "ref_company_id": int64(7)}); v != 7 {
		t.Fatalf("many2one 显式给 7，应为 7，实得 %d", v)
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

// 更新时清空一个带默认值函数的多对一：照存空值，不许被默认值填回去（Odoo 的 write 从不套默认值）。
// 真实场景：把价格表的公司清空、改成所有公司共用，保存后又变回当前公司。
func TestDefaultCompany_Postgres_ClearOnUpdateStaysEmpty(t *testing.T) {
	o, m := setupDefaultCompanyOrm(t)
	in5 := WithCompanyId(context.Background(), 5)
	ids, err := o.Model(m).WithContext(in5).Create(map[string]any{"name": "u", "ref_company_id": int64(7)})
	if err != nil {
		t.Fatal(err)
	}
	read := func() int64 {
		ds, err := o.Query(`SELECT COALESCE(ref_company_id, 0) AS v FROM dc_probe WHERE id = ?`, ids[0])
		if err != nil {
			t.Fatal(err)
		}
		return ds.FieldByName("v").AsInteger()
	}
	for _, blank := range []any{int64(0), false} {
		if _, err := o.Model(m).WithContext(in5).Ids(ids...).Write(map[string]any{"ref_company_id": int64(7)}); err != nil {
			t.Fatal(err)
		}
		if _, err := o.Model(m).WithContext(in5).Ids(ids...).Write(map[string]any{"ref_company_id": blank}); err != nil {
			t.Fatalf("clear with %v: %v", blank, err)
		}
		if v := read(); v > 0 {
			t.Fatalf("更新时用 %v 清空，却被默认值填成了 %d", blank, v)
		}
	}
}

// 必填字段显式给空：补默认值。
func TestDefaultCompany_Postgres_RequiredBlankGetsDefault(t *testing.T) {
	type dcReq struct {
		TModel       `table:"name('dc_req')"`
		Id           int64  `field:"pk autoincr"`
		Name         string `field:"varchar(32)"`
		ReqCompanyId int64  `field:"many2one(dc.req) default_company required title('Company')"`
	}
	cleaner, err := New(WithDataSource(bfSource()))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	cleaner.Exec(`DROP TABLE IF EXISTS public.dc_req CASCADE`)
	cleaner.Close()
	o, err := New(WithDataSource(bfSource()))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	defer o.Close()
	names, err := o.SyncModel("test", new(dcReq))
	if err != nil || len(names) != 1 {
		t.Fatalf("SyncModel: %v %v", names, err)
	}
	if err := o.Freeze(context.Background()); err != nil {
		t.Fatal(err)
	}
	ids, err := o.Model(names[0]).WithContext(WithCompanyId(context.Background(), 5)).Create(map[string]any{"name": "r", "req_company_id": int64(0)})
	if err != nil {
		t.Fatalf("必填字段显式给空应补默认值而不是报错: %v", err)
	}
	ds, _ := o.Query(`SELECT COALESCE(req_company_id, 0) AS v FROM dc_req WHERE id = ?`, ids[0])
	if v := ds.FieldByName("v").AsInteger(); v != 5 {
		t.Fatalf("必填字段显式给空应补当前公司 5，实得 %d", v)
	}
}

// default_company(required)：Odoo 上必填、本仓存量有空值加不了 NOT NULL 的 company_id。
// 新建时显式给空也补当前公司；库上不加约束（存量的空值不受影响）。
func TestDefaultCompany_Postgres_RequiredParam(t *testing.T) {
	type dcSoft struct {
		TModel    `table:"name('dc_soft')"`
		Id        int64  `field:"pk autoincr"`
		Name      string `field:"varchar(32)"`
		CompanyId int64  `field:"many2one(dc.soft) default_company(required) title('Company')"`
		Level     int64  `field:"int(64) default_company(required) title('Level')"`
	}
	cleaner, err := New(WithDataSource(bfSource()))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	cleaner.Exec(`DROP TABLE IF EXISTS public.dc_soft CASCADE`)
	cleaner.Close()
	o, err := New(WithDataSource(bfSource()))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	defer o.Close()
	names, err := o.SyncModel("test", new(dcSoft))
	if err != nil || len(names) != 1 {
		t.Fatalf("SyncModel: %v %v", names, err)
	}
	if err := o.Freeze(context.Background()); err != nil {
		t.Fatal(err)
	}
	m, _ := o.GetModel(names[0])
	f := m.GetFieldByName("company_id")
	if !IsCompanyRequired(f) || f.Required() {
		t.Fatal("default_company(required) 应只标记公司必填语义，不让字段本身 required（那会 SET NOT NULL）")
	}
	ids, err := o.Model(names[0]).WithContext(WithCompanyId(context.Background(), 5)).Create(map[string]any{"name": "s", "company_id": false, "level": int64(0)})
	if err != nil {
		t.Fatal(err)
	}
	ds, _ := o.Query(`SELECT COALESCE(company_id, 0) AS c, COALESCE(level, 0) AS l FROM dc_soft WHERE id = ?`, ids[0])
	if c, l := ds.FieldByName("c").AsInteger(), ds.FieldByName("l").AsInteger(); c != 5 || l != 5 {
		t.Fatalf("显式给空也应补当前公司 5，实得 many2one=%d int=%d", c, l)
	}
	nn, _ := o.Query(`SELECT is_nullable AS n FROM information_schema.columns WHERE table_name = 'dc_soft' AND column_name = 'company_id'`)
	if n := nn.FieldByName("n").AsString(); n != "YES" {
		t.Fatalf("不该加 NOT NULL（存量有空值），is_nullable=%s", n)
	}
}
