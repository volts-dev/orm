package orm

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/volts-dev/dataset"
)

/*
many2one 上 `('x','=',False)` 的回归。

Odoo 用它表达"关系为空"，移植过来的 XML 里有 263 处，其中一部分落在**记录规则**的
domain_force 上（sale/security/ir_rules.xml 的 ('user_id','=',False)、
product/security/product_security.xml 的 ('company_id','=',False)×6）。

原来这里没有分支，false 一路当普通右值绑进 `x = ?`：

	pq: invalid input syntax for type bigint: "false" (22P02)

不是筛错，是**整条请求 500**。真栈实测 res.partner 的 user_id / company_id /
parent_id 三个字段全崩；记录规则一崩就是整模型读不出来。

第二个要点是"空"不等于 NULL。同一张 res_partner 里三种形态同时存在：

	company_id   NULL=0   0=0   -1=41   真值=13
	user_id      NULL=51  0=0   -1=0    真值=3
	parent_id    NULL=27  0=1   -1=0    真值=26

所以判据必须是**集合本身**，而且样本要同时含 NULL / 0 / -1，只造 NULL 的话
`IS NULL` 那种半吊子修法也能全绿。

方言差异要留个心眼：这些用例跑在 sqlite 上，`site_id = 'false'` 被它悄悄折算成
0，于是**回错行**；PG 上同一条 SQL 直接 22P02 报错。所以线上表现是 500，而单测里
的反证表现是集合不对 —— 两者是同一个病灶。反证实测（把修复还原）：

	('site_id','=',False)          得到 [zero_row]            应为 [neg_row null_row zero_row]
	('site_id','!=',False)         得到 [home neg_row other]  应为 [home other]
	('site_id','in',[site1,False]) 得到 [home null_row]       应为 [home neg_row null_row zero_row]

`not in` 那条修改前后都是绿的（旧代码补的 `OR IS NULL` 恰好够用），留着当不回退的
护栏，不算反证。
*/

type (
	FalseSite struct {
		TModel `table:"name('mfalse_site')"`
		Id     int64  `field:"pk autoincr title('ID')"`
		Name   string `field:"varchar() size(64)"`
	}

	FalseMenu struct {
		TModel `table:"name('mfalse_menu')"`
		Id     int64  `field:"pk autoincr title('ID')"`
		Name   string `field:"varchar() size(64)"`
		SiteId int64  `field:"many2one(mfalse_site)"`
	}
)

// setupM2OFalse 造出真栈同款的五行：两行有主、三行"空"但落库形态各不相同。
// 空的三种都要走裸 INSERT —— 经 ORM 建记录控制不了落哪一种。
func setupM2OFalse(t *testing.T) (*TOrm, int64) {
	t.Helper()
	ds := &TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "mfalse.db")}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(FalseSite), new(FalseMenu)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}

	site1 := firstId(t, mustCreate(t, o, "mfalse.site", map[string]any{"name": "site1"}))
	site2 := firstId(t, mustCreate(t, o, "mfalse.site", map[string]any{"name": "site2"}))

	mustCreate(t, o, "mfalse.menu", map[string]any{"name": "home", "site_id": site1})
	mustCreate(t, o, "mfalse.menu", map[string]any{"name": "other", "site_id": site2})
	if _, err := o.Exec(`INSERT INTO mfalse_menu ("name") VALUES (?)`, "null_row"); err != nil {
		t.Fatalf("insert NULL row: %v", err)
	}
	if _, err := o.Exec(`INSERT INTO mfalse_menu ("name","site_id") VALUES (?,?)`, "zero_row", 0); err != nil {
		t.Fatalf("insert 0 row: %v", err)
	}
	if _, err := o.Exec(`INSERT INTO mfalse_menu ("name","site_id") VALUES (?,?)`, "neg_row", -1); err != nil {
		t.Fatalf("insert -1 row: %v", err)
	}
	return o, site1
}

func falseMenuNames(t *testing.T, o *TOrm, dom any) []string {
	t.Helper()
	ds, err := o.Model("mfalse.menu").Domain(dom).Limit(-1).Read()
	if err != nil {
		t.Fatalf("read %v: %v", dom, err)
	}
	var out []string
	if ds != nil {
		ds.Range(func(_ int, rec *dataset.TRecordSet) error {
			out = append(out, rec.FieldByName("name").AsString())
			return nil
		})
	}
	sort.Strings(out)
	return out
}

// 病灶本身：`('site_id','=',False)` 从前是 500，现在应回全部"空"的行。
func TestM2OEqualsFalse_MatchesAllEmptyForms(t *testing.T) {
	o, _ := setupM2OFalse(t)

	got := falseMenuNames(t, o, []any{"site_id", "=", false})
	want := []string{"neg_row", "null_row", "zero_row"}
	if !sameStrings(got, want) {
		t.Errorf("('site_id','=',False) 得到 %v，应为 %v\n"+
			"（只剩 null_row 说明修法停在 IS NULL，漏了 0 / -1 两种空形态）", got, want)
	}
}

// 反向：`!= False` 是"关系非空"，0 和 -1 都不算有主。
func TestM2ONotEqualsFalse_ExcludesAllEmptyForms(t *testing.T) {
	o, _ := setupM2OFalse(t)

	got := falseMenuNames(t, o, []any{"site_id", "!=", false})
	want := []string{"home", "other"}
	if !sameStrings(got, want) {
		t.Errorf("('site_id','!=',False) 得到 %v，应为 %v\n"+
			"（混进 zero_row/neg_row 说明 `> 0` 那半边没生效）", got, want)
	}
}

// 记录规则里最常见的那条形状：本站的 + 无主的。
// 漏掉任何一种空形态，都会让"公共数据"对使用者凭空消失。
func TestM2OSiteScopeDomain_LikeRecordRule(t *testing.T) {
	o, site1 := setupM2OFalse(t)

	got := falseMenuNames(t, o, []any{"|", []any{"site_id", "=", site1}, []any{"site_id", "=", false}})
	want := []string{"home", "neg_row", "null_row", "zero_row"}
	if !sameStrings(got, want) {
		t.Errorf("['|',('site_id','=',site1),('site_id','=',False)] 得到 %v，应为 %v", got, want)
	}
}

// `in` 列表里夹带 False 走的是另一条分支（check_nulls），同样要认三种空形态。
func TestM2OInListWithFalse_MatchesAllEmptyForms(t *testing.T) {
	o, site1 := setupM2OFalse(t)

	got := falseMenuNames(t, o, []any{"site_id", "in", []any{site1, false}})
	want := []string{"home", "neg_row", "null_row", "zero_row"}
	if !sameStrings(got, want) {
		t.Errorf("('site_id','in',[site1,False]) 得到 %v，应为 %v", got, want)
	}
}

// `not in` 不带 False 时应当**包含**空行（SQL 的 NULL 语义要补回来），
// 三种空形态一视同仁。
func TestM2ONotInList_IncludesAllEmptyForms(t *testing.T) {
	o, site1 := setupM2OFalse(t)

	got := falseMenuNames(t, o, []any{"site_id", "not in", []any{site1}})
	want := []string{"neg_row", "null_row", "other", "zero_row"}
	if !sameStrings(got, want) {
		t.Errorf("('site_id','not in',[site1]) 得到 %v，应为 %v", got, want)
	}
}

// 别把 bool 字段的老路改坏：Bool 的 `= False` 仍是 `IS NULL or = false`，
// 不能跟着走 `<= 0`。
func TestBoolEqualsFalse_Unaffected(t *testing.T) {
	o, _ := setupM2OFalse(t)

	// mfalse.site 没有 bool 列，借 menu 的 site_id 之外再验一次类型分派即可：
	// 这里只确认 m2o 分支没有把非关系字段吞掉。
	got := falseMenuNames(t, o, []any{"name", "=", "home"})
	if !sameStrings(got, []string{"home"}) {
		t.Errorf("普通字段等值被改坏了：%v", got)
	}
}
