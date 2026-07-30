package test

import (
	"testing"
	"time"

	"github.com/volts-dev/utils"

	_ "modernc.org/sqlite"
)

// TestOne2OneInheritNullFKStillReadable 覆盖「子记录没有 _inherits 父记录」这一情形。
//
// company_model 通过 one2one(partner_model) 委托继承 partner 的字段，读取侧要为继承
// 字段(homepage)补上父表连接。若该连接是 INNER JOIN，FK 为 NULL 的行会被连接直接
// 过滤掉：记录在表里、列表里却查不出来，且没有任何报错。
//
// FK 为 NULL 是能真实出现的：父记录被删、由外部导入的历史数据、以及写入侧只在继承
// 字段非空时才自动建父记录(session_crwd.go 的 rel_vals 分支)。本用例用一条 UPDATE
// 直接把 FK 置空来构造该状态——company_model 继承来的 create_time/write_time 带
// created/updated 标签会被自动填值，正常 Create 反而造不出空 FK。
//
// 本用例锁死「读得回来」；继承字段读成空值是预期的(本来就没有父记录)。
func TestOne2OneInheritNullFKStillReadable(t *testing.T) {
	o := newOne2OneOrm(t)

	companyModel, err := o.GetModel("company_model")
	if err != nil {
		t.Fatal(err)
	}

	suffix := utils.ToString(time.Now().UnixNano())
	bareName := "BareCo_" + suffix   // 不带继承字段 → partner_id 为 NULL
	fullName := "FullCo_" + suffix   // 带继承字段 → 自动建 partner 并回填 FK
	homepage := "https://full.example/" + suffix

	bareIds, err := companyModel.Records().Create(map[string]any{
		"name": bareName,
	})
	if err != nil {
		t.Fatalf("create bare company failed: %v", err)
	}
	if len(bareIds) == 0 || bareIds[0] == nil {
		t.Fatal("create bare company 未返回 id")
	}

	if _, err := companyModel.Records().Create(map[string]any{
		"name":     fullName,
		"homepage": homepage,
	}); err != nil {
		t.Fatalf("create full company failed: %v", err)
	}

	// 把 bare 那条的 FK 置空，构造「没有父记录的子记录」。
	if _, err := o.NewSession().Exec(`UPDATE company_model SET partner_id=NULL WHERE name=?`, bareName); err != nil {
		t.Fatalf("置空 partner_id 失败: %v", err)
	}

	// 先确认前提成立：这条记录的 partner_id 确实是空的，否则本用例什么也没验证。
	// 必须连 name 一起选：dataset.AppendRecord 会静默丢弃「所有字段都为 nil」的记录，
	// 只选 partner_id 这一个 NULL 列的话结果集会是空的，探针本身就失真了。
	raw, err := o.NewSession().Query(`SELECT name, partner_id FROM company_model WHERE name=?`, bareName)
	if err != nil {
		t.Fatal(err)
	}
	if raw.Count() != 1 {
		t.Fatalf("company_model 里应有 1 条 %s, 实际 %d", bareName, raw.Count())
	}
	if fk := raw.Record().GetByField("partner_id"); !utils.IsBlank(fk) {
		t.Fatalf("前提不成立: 期望 partner_id 为空, 实际 %v", fk)
	}

	// --- 按 id 读：不能丢 ---
	ds, err := companyModel.Records().Ids(bareIds[0]).Read()
	if err != nil {
		t.Fatalf("read bare company failed: %v", err)
	}
	if ds.Count() != 1 {
		t.Fatalf("按 id 读无父记录的 company 丢失: 期望 1 条, 实际 %d "+
			"(继承字段的父表连接把 FK 为 NULL 的行过滤掉了)", ds.Count())
	}
	if got := utils.ToString(ds.Record().GetByField("name")); got != bareName {
		t.Fatalf("name=%q, want %q", got, bareName)
	}

	// --- 按条件读：同样不能丢，且有父记录的那条要正常回填继承字段 ---
	all, err := companyModel.Records().In("name", bareName, fullName).Read()
	if err != nil {
		t.Fatalf("read both companies failed: %v", err)
	}
	if all.Count() != 2 {
		t.Fatalf("条件读期望 2 条(有/无父记录各一), 实际 %d", all.Count())
	}

	seen := map[string]string{}
	all.First()
	for !all.Eof() {
		rec := all.Record()
		seen[utils.ToString(rec.GetByField("name"))] = utils.ToString(rec.GetByField("homepage"))
		all.Next()
	}
	if _, ok := seen[bareName]; !ok {
		t.Fatalf("条件读结果里缺少无父记录的 %s: %v", bareName, seen)
	}
	if seen[bareName] != "" {
		t.Errorf("无父记录时继承字段应为空, 实际 %q", seen[bareName])
	}
	if seen[fullName] != homepage {
		t.Errorf("有父记录时继承字段应回填: got %q, want %q", seen[fullName], homepage)
	}
}
