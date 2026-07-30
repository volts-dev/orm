package test

import (
	"context"
	"testing"

	_ "github.com/lib/pq"
	"github.com/volts-dev/orm"
)

// 委托继承(one2one/_inherits)读取在**非默认 schema** 下必须照样 JOIN 上父表。
//
// 生产对应场景：VectorsSystem 系统租户的 T 表物化在 "system" schema，而 res.company /
// res.user 都通过 one2one 委托继承 res.partner。读它们时 SELECT 里会出现父表别名限定的
// 列(`"res_company__partner_id"."city"`)，这个别名靠 inherits_join_calc 往 query.joins
// 里登记一条 LEFT JOIN 来提供。
//
// 而 joins 的键是**裸表名**(inherits_join_calc 用 model.Table())，getSql 渲染 FROM 时
// 却是拿 self.tables 里的条目去查——非默认 schema 下那是 "system.res_company"，两者对不
// 上，JOIN 一条都不渲染，SELECT 里的别名限定列却照常输出：
//
//	pq: missing FROM-clause entry for table "res_company__partner_id" (42P01)
//
// 报错被 OnRead 吞成一行日志，请求照常 200 —— 表现是「同一条记录里 country_id 内嵌成功、
// company_id / user_id 只剩裸 id」(没有委托继承的 comodel 不受影响)。public schema 下
// 一切正常，所以只有系统租户/专属 schema 的租户会中招。
type (
	DlgPartner struct {
		orm.TModel `table:"name('dlg_partner')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Name       string `field:"varchar() required"`
		City       string `field:"varchar()"`
	}

	// 委托继承 DlgPartner —— 对标 res.company/res.user 继承 res.partner。
	DlgCompany struct {
		orm.TModel `table:"name('dlg_company')"`
		DlgPartner  `field:"relate(PartnerId)"`
		PartnerId  int64  `field:"one2one(dlg_partner)"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Name       string `field:"varchar() required"`
	}

	DlgContact struct {
		orm.TModel `table:"name('dlg_contact')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Name       string `field:"varchar() required"`
		CompanyId  int64  `field:"many2one(dlg_company)"`
	}
)

const dlgSchema = "dlg_sys"

func TestInheritsJoinInNonDefaultSchemaPG(t *testing.T) {
	ensurePG(t)

	o, err := orm.New(orm.WithDataSource(defaultPostgresSource()), orm.WithBigNumberToString(true))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}

	for _, q := range []string{
		`DROP SCHEMA IF EXISTS ` + dlgSchema + ` CASCADE`,
		`DROP TABLE IF EXISTS public.dlg_contact`,
		`DROP TABLE IF EXISTS public.dlg_company`,
		`DROP TABLE IF EXISTS public.dlg_partner`,
		`CREATE SCHEMA ` + dlgSchema,
	} {
		if _, err := o.Exec(q); err != nil {
			t.Fatalf("清场失败 %q: %v", q, err)
		}
	}

	models := []orm.IModel{new(DlgPartner), new(DlgCompany), new(DlgContact)}
	if _, err := o.SyncModel("test", models...); err != nil {
		t.Fatalf("SyncModel(public): %v", err)
	}
	ms := o.NewSession()
	ms.SetSchema(dlgSchema)
	if _, err := ms.SyncModel("test", models...); err != nil {
		t.Fatalf("SyncModel(%s): %v", dlgSchema, err)
	}
	ms.Close()
	if err := o.Freeze(context.Background()); err != nil {
		t.Fatalf("Freeze: %v", err)
	}

	companyModel, _ := o.GetModel("dlg_company")
	contactModel, _ := o.GetModel("dlg_contact")

	ss := o.NewSession()
	defer ss.Close()
	ss.SetSchema(dlgSchema)
	if err := ss.Begin(); err != nil {
		t.Fatal(err)
	}
	cids, err := companyModel.Tx(ss).Create(map[string]any{"name": "SysCo"})
	if err != nil {
		t.Fatalf("create company: %v", err)
	}
	if _, err := contactModel.Tx(ss).Create(map[string]any{"name": "Mitchell Admin", "company_id": cids[0]}); err != nil {
		t.Fatalf("create contact: %v", err)
	}
	if err := ss.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// 父记录用显式限定的裸 SQL 建，复刻生产布局：真栈 system 租户的
	// system.res_company.partner_id 指向 system.res_partner 的同 schema 行。
	//
	// ⚠️ 不走 Create 的继承字段写入路径是有意的：那条路径给委托父表发的 INSERT
	// **没有带 schema**（实测 `INSERT INTO dlg_partner …` 对 `dlg_sys.dlg_company`），
	// 父记录会落到 public。那是写入侧另一个独立的 schema 限定缺口，不在本用例范围内；
	// 本用例锁的是读取侧的 JOIN 渲染。
	if _, err := o.Exec(`INSERT INTO ` + dlgSchema + `.dlg_partner (id, name, city) VALUES (9001, 'SysCo', 'Scranton')`); err != nil {
		t.Fatalf("建父记录: %v", err)
	}
	if _, err := o.Exec(`UPDATE `+dlgSchema+`.dlg_company SET partner_id=9001 WHERE id=$1`, cids[0]); err != nil {
		t.Fatalf("回填 partner_id: %v", err)
	}

	// ── 1. 直接读带委托继承的模型：不能报 42P01 ──────────────────────────
	rs := companyModel.Records()
	rs.SetSchema(dlgSchema)
	cds, err := rs.Ids(cids[0]).Read()
	if err != nil {
		t.Fatalf("在 schema %q 下读 dlg_company 失败(继承字段的父表 JOIN 没渲染进 FROM): %v", dlgSchema, err)
	}
	if cds.Count() != 1 {
		t.Fatalf("期望 1 条 company, 实际 %d", cds.Count())
	}
	if got := cds.Record().GetByField("city"); got != "Scranton" {
		t.Errorf("继承字段 city 期望 Scranton, 实际 %#v", got)
	}

	// ── 2. classic 内嵌该模型：必须是子记录 map 而不是裸 id ──────────────
	cs := contactModel.Records()
	cs.SetSchema(dlgSchema)
	ds, err := cs.Classic().Select("id", "name", "company_id").Read()
	if err != nil {
		t.Fatalf("classic read contact: %v", err)
	}
	if ds.Count() != 1 {
		t.Fatalf("期望 1 条 contact, 实际 %d", ds.Count())
	}
	m := ds.Record().AsMap()
	sub, ok := m["company_id"].(map[string]any)
	if !ok {
		t.Fatalf("company_id 期望内嵌子记录 map, 实际 %T = %#v "+
			"(comodel 有委托继承 + 非默认 schema → 子读取 SQL 报 42P01, 被吞成日志)", m["company_id"], m["company_id"])
	}
	if sub["name"] != "SysCo" {
		t.Errorf("company_id.name = %v, want SysCo", sub["name"])
	}
}
