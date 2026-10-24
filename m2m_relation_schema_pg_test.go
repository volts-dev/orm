package orm

import (
	"testing"

	_ "github.com/lib/pq"
)

type relSchemaUser struct {
	TModel    `table:"name('relschema_user')"`
	Id        int64   `field:"pk autoincr"`
	Name      string  `field:"varchar(32)"`
	TenantIds []int64 `field:"many2many(relschema.tenant,relschema_user_id,relschema_tenant_id)"`
}

type relSchemaTenant struct {
	TModel `table:"name('relschema_tenant')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(32)"`
}

// 关联表只建在某个非默认 schema 里时，关系模型上也要有那两个外键字段。
//
// 关系模型的字段原来全靠**反查**，而反查只看默认 schema：vectors 的 sys_user_sys_tenant_rel
// 只在 system 里，public 里没有这张表，于是 sys.user.sys.tenant.rel 上一个字段都没有。
// 按 sys_user_id 过滤报 Invalid field（平台用户列表的「租户数」恒为 0）；按 tenant_id 过滤
// 碰巧能跑，只因为每个模型都有 tenant_id —— 看起来一半好一半坏。
func TestM2MRelationModel_KeyFieldsWhenTableOnlyInOtherSchema(t *testing.T) {
	bootstrap := pgDataSource(t, "test_orm")
	boot, err := New(WithDataSource(bootstrap))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	// 独占一个库（同 field_runtime_pg_test.go）：建模型会反查整库，与别的用例共用 test_orm 会互相撞。
	boot.Exec(`CREATE DATABASE test_orm_relschema`)
	ds := pgDataSource(t, "test_orm_relschema")

	cleaner, err := New(WithDataSource(ds))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	for _, q := range []string{
		`DROP SCHEMA IF EXISTS relschema_s CASCADE`,
		`DROP TABLE IF EXISTS public.relschema_user_relschema_tenant_rel`,
		`DROP TABLE IF EXISTS public.relschema_user`,
		`DROP TABLE IF EXISTS public.relschema_tenant`,
		`CREATE SCHEMA relschema_s`,
	} {
		if _, err := cleaner.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	defer cleaner.Exec(`DROP SCHEMA IF EXISTS relschema_s CASCADE`)

	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	ss := o.NewSession()
	ss.SetSchema("relschema_s")
	if _, err := ss.SyncModel("test", new(relSchemaTenant), new(relSchemaUser)); err != nil {
		t.Fatalf("SyncModel in relschema_s: %v", err)
	}
	ss.Close()

	chk, _ := o.Query(`SELECT table_schema FROM information_schema.tables WHERE table_name = 'relschema_user_relschema_tenant_rel'`)
	if chk == nil || chk.Count() != 1 || chk.FieldByName("table_schema").AsString() != "relschema_s" {
		t.Fatalf("前提不成立：关联表应只建在 relschema_s 里")
	}

	user, err := o.GetModel("relschema.user")
	if err != nil {
		t.Fatal(err)
	}
	f := user.GetFieldByName("tenant_ids")
	rel, err := o.GetModel(f.JoinModelName())
	if err != nil || rel == nil {
		t.Fatalf("关系模型 %s 没注册: %v", f.JoinModelName(), err)
	}
	for _, key := range []string{f.JoinSourceKey(), f.RelatedKeyName()} {
		if rel.GetFieldByName(key) == nil {
			t.Errorf("关系模型 %s 上没有外键字段 %s（关联表只在 relschema_s，反查拿不到）", rel.String(), key)
		}
	}

	if _, err := o.Exec(`INSERT INTO relschema_s.relschema_user_relschema_tenant_rel (relschema_user_id, relschema_tenant_id) VALUES (11, 21), (11, 22), (12, 21)`); err != nil {
		t.Fatal(err)
	}
	rs := o.NewSession()
	defer rs.Close()
	rs.SetSchema("relschema_s")
	got, err := rs.Model(rel.String()).In(f.JoinSourceKey(), int64(11)).Select(f.JoinSourceKey()).Limit(-1).Read()
	if err != nil {
		t.Fatalf("按 %s 过滤关系模型: %v", f.JoinSourceKey(), err)
	}
	if got.Count() != 2 {
		t.Fatalf("用户 11 应绑 2 个租户，读出 %d 行", got.Count())
	}
}
