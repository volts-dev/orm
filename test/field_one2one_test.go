package test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/volts-dev/orm"
	"github.com/volts-dev/utils"

	_ "modernc.org/sqlite"
)

// newOne2OneOrm 创建一个独立的 sqlite ORM 实例并同步三张表,
// 与全局 TestORMInterfaces 隔离, 避免数据/执行顺序相互干扰。
func newOne2OneOrm(t *testing.T) *orm.TOrm {
	t.Helper()
	ds := &orm.TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "o2o.db")}
	o, err := orm.New(orm.WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	o.Config().Init(orm.WithShowSql(true))

	if _, err := o.SyncModel("test", new(PartnerModel), new(CompanyModel), new(UserModel)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}

	// one2one 委托继承在 osv.Freeze Phase 2 才执行(inheritO2OFields)。
	// SyncModel 不会自动 Freeze, 故显式调用, 否则继承字段不会出现在 company_model 上。
	if err := o.Freeze(context.Background()); err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	return o
}

// TestOne2OneInheritReadWrite 验证 one2one(委托继承)字段的读写闭环:
//
//	company_model 通过 one2one(partner_model) 委托继承 partner_model 的字段。
//	homepage 仅存在于 partner_model, 对 company_model 而言是 isInherited 字段(本表无列)。
//
// 覆盖三点:
//  1. 写入: 继承字段 homepage 应被路由写入关系表 partner_model 并自动建立 FK。
//  2. 普通读: 非 Classic 模式不触发 one2one OnRead, 故继承字段不回填(对照基线)。
//  3. Classic 读: 应完整回填关系表的继承字段值。
func TestOne2OneInheritReadWrite(t *testing.T) {
	o := newOne2OneOrm(t)

	suffix := utils.ToString(time.Now().UnixNano())
	companyName := "InheritCo_" + suffix
	homepage := "https://inherited.example/" + suffix

	companyModel, err := o.GetModel("company_model")
	if err != nil {
		t.Fatal(err)
	}
	partnerModel, err := o.GetModel("partner_model")
	if err != nil {
		t.Fatal(err)
	}

	// 确认 homepage 在 company_model 上确为继承字段(本表不存储)。
	if f := companyModel.GetFieldByName("homepage"); f == nil {
		t.Fatal("company_model 未继承到 homepage 字段")
	} else if !f.IsInherited() {
		t.Fatalf("homepage 应为继承字段(isInherited), 实际 inherited=%v store=%v", f.IsInherited(), f.Store())
	} else {
		t.Logf("homepage 为继承字段: inherited=%v store=%v ✓", f.IsInherited(), f.Store())
	}

	// --- 1. 写入: company + 继承字段 homepage ---
	ss := o.NewSession()
	defer ss.Close()
	if err := ss.Begin(); err != nil {
		t.Fatal(err)
	}
	ids, err := companyModel.Tx(ss).Create(map[string]any{
		"name":     companyName,
		"homepage": homepage,
	})
	if err != nil {
		t.Fatalf("create company failed: %v", err)
	}
	if err := ss.Commit(); err != nil {
		t.Fatalf("commit failed: %v", err)
	}
	if len(ids) == 0 || ids[0] == nil {
		t.Fatal("create 未返回 id")
	}
	companyId := ids[0]
	t.Logf("created company id=%v", companyId)

	// --- 验证 1: 继承字段已落到关系表 partner_model ---
	pds, err := partnerModel.Records().Where("homepage=?", homepage).Read()
	if err != nil {
		t.Fatal(err)
	}
	if pds.Count() != 1 {
		t.Fatalf("期望关系表 partner_model 恰有 1 条携带继承 homepage 的记录, 实际 %d", pds.Count())
	}
	partnerId := pds.Record().GetByField("id")
	if got := utils.ToString(pds.Record().GetByField("homepage")); got != homepage {
		t.Fatalf("partner.homepage=%v, want %v", got, homepage)
	}
	t.Logf("继承 homepage 已写入 partner_model id=%v ✓", partnerId)

	// --- 验证 2: company.partner_id FK 指向该 partner 记录 ---
	cds, err := companyModel.Records().Ids(companyId).Read()
	if err != nil {
		t.Fatal(err)
	}
	if cds.Count() != 1 {
		t.Fatalf("期望 company 1 条, 实际 %d", cds.Count())
	}
	fk := cds.Record().GetByField("partner_id")
	if utils.ToString(fk) != utils.ToString(partnerId) {
		t.Fatalf("company.partner_id=%v, 期望指向 partner %v", fk, partnerId)
	}
	t.Logf("company.partner_id=%v 正确指向 partner ✓", fk)

	// 普通(非 Classic)读: 委托继承的标量字段经 inherits_join_calc 的 JOIN 回填,
	// 故 homepage 在普通读下也应返回关系表的值。
	if got := utils.ToString(cds.Record().GetByField("homepage")); got != homepage {
		t.Fatalf("普通读未通过 JOIN 回填继承 homepage: got %q, want %q", got, homepage)
	}
	t.Logf("普通读经 JOIN 回填继承 homepage=%q ✓", utils.ToString(cds.Record().GetByField("homepage")))

	// --- 验证 3: Classic 读应完整回填继承字段 homepage ---
	cds2, err := companyModel.Records().Ids(companyId).Classic().Read()
	if err != nil {
		t.Fatal(err)
	}
	if cds2.Count() != 1 {
		t.Fatalf("Classic 读期望 1 条, 实际 %d", cds2.Count())
	}
	gotHome := utils.ToString(cds2.Record().GetByField("homepage"))
	if gotHome != homepage {
		t.Fatalf("Classic 读未回填继承 homepage: got %q, want %q\n完整记录: %v",
			gotHome, homepage, cds2.Record().AsMap())
	}
	t.Logf("Classic 读完整回填继承 homepage=%q ✓", gotHome)
}
