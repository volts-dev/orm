package orm

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/volts-dev/dataset"
)

/*
`('m2o字段.x2many字段', '=', False)` 的回归 —— 员工银行账号那道闸的形状。

# 为什么单拎出来

vs/modules/hr 的 hr_security.xml 里有这么一条记录规则，它是工资卡号不外泄的最后
一道闸（字段级 ACL 挡的是 hr.employee 上的 bank_account_ids，而账号本体在
res.partner.bank，绕过员工表直接查那张表照样能看到）：

	<record id="ir_rule_res_partner_bank_internal_users" model="sys.rule">
	    <field name="model_id" ref="registry.model_res_partner_bank"/>
	    <field name="domain_force">[('partner_id.employee_ids', '=', False)]</field>
	    <field name="group_ids" eval="[(4, ref('registry.group_user'))]"/>
	</record>

这条 leaf 的第一段 `partner_id` 是 **many2one 不是 x2many**，所以它不走
resolveX2manyLeaf 的点号分支（那条分支处理的是 `('tag_ids.name','=',False)`，
且明确不做 False 判断）；它先被展开成对端子查询，**内层**才是 x2many + False。
两段各自都有测试，这个组合没有。

# 为什么以前验不到

记录规则对超管不生效（recordrule.go 的 IsSuper || IsPlatformAdmin 直接跳过），
而本仓所有真栈验证都是拿 admin 做的 —— 这条闸门从建起来就没有任何东西执行过它。
在 orm 这一层测就绕开了"要有一个非超管账号"这个前提。

# 判据是集合本身

规则一旦失效不是报错，是**多回行**：普通用户看到员工的银行账号。所以断言必须是
集合，不能是 err == nil。

反证（2026-08-24 实测，把 expr_x2many.go 的 isDomainFalse 改成恒 false）：

	('partner_id.emp_ids','=',False)  得到 []                                应为 [bank_of_plain]
	('partner_id.emp_ids','!=',False) 得到 [bank_of_employee bank_of_plain]   应为 [bank_of_employee]

`!=` 那条是真正要盯的形状：它不报错、也不回空，而是**多回一张** —— 正好是
"普通用户看到了员工的银行账号"。

# 孤儿账号被排除是对的

bank_orphan（partner_id 为空）两边都不在结果里。Odoo 生成的是
`partner_id IN (SELECT ...)`，NULL 不 IN 任何集合，所以同样排除 —— 一比一。
*/

type (
	// 对应 res.partner
	DxOwner struct {
		TModel `table:"name('dx_owner')"`
		Id     int64  `field:"pk autoincr title('ID')"`
		Name   string `field:"varchar() size(64)"`
		EmpIds []any  `field:"one2many(dx_emp,owner_id)"`
	}
	// 对应 hr.employee
	DxEmp struct {
		TModel  `table:"name('dx_emp')"`
		Id      int64  `field:"pk autoincr title('ID')"`
		Name    string `field:"varchar() size(64)"`
		OwnerId int64  `field:"many2one(dx_owner)"`
	}
	// 对应 res.partner.bank
	DxBank struct {
		TModel    `table:"name('dx_bank')"`
		Id        int64  `field:"pk autoincr title('ID')"`
		Name      string `field:"varchar() size(64)"`
		PartnerId int64  `field:"many2one(dx_owner)"`
	}
)

// setupDottedX2m 造三张账号，正好分辨"闸门生效 / 失效 / 反了"：
//
//	bank_of_employee → owner_with_emp（名下有员工）—— 普通用户**不该**看到
//	bank_of_plain    → owner_no_emp  （名下无员工）—— 普通用户该看到
//	bank_orphan      → 没有 partner              —— 见文件头
//
// 闸门被丢掉的话三条全回，与正确答案（1 条）不同。
func setupDottedX2m(t *testing.T) *TOrm {
	t.Helper()
	// 不能用 :memory:，理由同 expr_x2many_test.go 的 setupX2m。
	ds := &TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "dx.db")}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(DxOwner), new(DxEmp), new(DxBank)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	mk := func(model string, vals map[string]any) int64 {
		id, err := o.Model(model).Create(vals)
		if err != nil {
			t.Fatalf("create %s %v: %v", model, vals, err)
		}
		return firstId(t, id)
	}
	withEmp := mk("dx.owner", map[string]any{"name": "owner_with_emp"})
	noEmp := mk("dx.owner", map[string]any{"name": "owner_no_emp"})
	mk("dx.emp", map[string]any{"name": "emp1", "owner_id": withEmp})
	mk("dx.bank", map[string]any{"name": "bank_of_employee", "partner_id": withEmp})
	mk("dx.bank", map[string]any{"name": "bank_of_plain", "partner_id": noEmp})
	mk("dx.bank", map[string]any{"name": "bank_orphan"})
	return o
}

func dxBankNames(t *testing.T, o *TOrm, dom any) []string {
	t.Helper()
	rs, err := o.Model("dx.bank").Domain(dom).Limit(-1).Read()
	if err != nil {
		t.Fatalf("read %v: %v", dom, err)
	}
	var out []string
	if rs != nil {
		rs.Range(func(_ int, r *dataset.TRecordSet) error {
			out = append(out, r.FieldByName("name").AsString())
			return nil
		})
	}
	sort.Strings(out)
	return out
}

// 闸门放行的只能是"名下无员工"的那一张。多回一张就是工资卡号外泄。
func TestDottedX2manyFalse_GatesEmployeeBankAccounts(t *testing.T) {
	o := setupDottedX2m(t)
	got := dxBankNames(t, o, `[('partner_id.emp_ids', '=', False)]`)
	if !sameStrings(got, []string{"bank_of_plain"}) {
		t.Errorf("得到 %v，应为 [bank_of_plain]\n"+
			"（含 bank_of_employee = 闸门失效，普通用户看得见员工银行账号；"+
			"回空 = 内层 x2many 的 False 判断没生效，整条规则把人全挡在外面）", got)
	}
}

// 取反必须正好互补（bank_orphan 两边都不在，见文件头）。
func TestDottedX2manyFalse_NotEqualsIsComplement(t *testing.T) {
	o := setupDottedX2m(t)
	got := dxBankNames(t, o, `[('partner_id.emp_ids', '!=', False)]`)
	if !sameStrings(got, []string{"bank_of_employee"}) {
		t.Errorf("得到 %v，应为 [bank_of_employee]", got)
	}
}

// 护栏：不加 domain 时三张都在。上面两条如果因为夹具坏了而回空，这条会先红。
func TestDottedX2manyFalse_FixtureSanity(t *testing.T) {
	o := setupDottedX2m(t)
	got := dxBankNames(t, o, `[]`)
	if !sameStrings(got, []string{"bank_of_employee", "bank_of_plain", "bank_orphan"}) {
		t.Errorf("夹具坏了：得到 %v", got)
	}
}
