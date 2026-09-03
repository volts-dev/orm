package orm

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/volts-dev/utils"
)

/*
child_of / parent_of 的语义回归。

这两个操作符此前是**空实现**（函数体只剩注释掉的 Python 和 `return nil`），调用点
拿到 nil 后循环一次不走，整条叶子凭空消失——child_of 筛选等于没筛、整表返回，不报错。
行级权限规则正是靠它表达"这条记录的公司是我某个公司的祖先/后代"，一条筛不出来的
规则就是一次越权，所以这里的每一条断言都要同时钉住**筛出了什么**和**没筛出什么**。

树（company）：
                root
              /      \
            asia     europe
           /    \        \
        cn      jp        de
       /
   shenzhen
*/

type hrCompany struct {
	TModel   `table:"name('hr_company')"`
	Id       int64  `field:"pk autoincr"`
	Name     string `field:"varchar(64) recname()"`
	ParentId int64  `field:"many2one(hr_company)"`
}

// hrOrder 用来验"对端是别的模型"那条路：('company_id','child_of',…) 要在
// company 里走树，而不是拿 order 自己的字段去走。
type hrOrder struct {
	TModel    `table:"name('hr_order')"`
	Id        int64  `field:"pk autoincr"`
	Name      string `field:"varchar(64)"`
	CompanyId int64  `field:"many2one(hr_company)"`
}

type hrFixture struct {
	o  *TOrm
	id map[string]int64
}

func newHierarchyFixture(t *testing.T) *hrFixture {
	t.Helper()
	ds := &TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "hier.db")}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(hrCompany), new(hrOrder)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}

	fx := &hrFixture{o: o, id: map[string]int64{}}
	mk := func(name, parent string) {
		vals := map[string]any{"name": name}
		if parent != "" {
			vals["parent_id"] = fx.id[parent]
		}
		ids, err := o.Model("hr.company").Create(vals)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		switch v := ids[0].(type) {
		case int64:
			fx.id[name] = v
		case int:
			fx.id[name] = int64(v)
		default:
			t.Fatalf("unexpected id type %T", ids[0])
		}
	}
	mk("root", "")
	mk("asia", "root")
	mk("europe", "root")
	mk("cn", "asia")
	mk("jp", "asia")
	mk("de", "europe")
	mk("shenzhen", "cn")
	return fx
}

// names 按 domain 读 company，回排序后的名字集合。
func (fx *hrFixture) names(t *testing.T, dom string) []string {
	t.Helper()
	ds, err := fx.o.Model("hr.company").Domain(dom).Limit(-1).OrderBy("name").Read()
	if err != nil {
		t.Fatalf("domain %s: %v", dom, err)
	}
	var out []string
	if ds == nil {
		return out
	}
	ds.First()
	for !ds.Eof() {
		out = append(out, ds.Record().FieldByName("name").AsString())
		ds.Next()
	}
	return out
}

func assertNames(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("期望 %v，实得 %v", want, got)
	}
}

// ---------- ('id', child_of/parent_of, …) ----------

// child_of 取的是后代**含自身**（对齐 Odoo）。
func TestHierarchy_ChildOfById(t *testing.T) {
	fx := newHierarchyFixture(t)

	assertNames(t, fx.names(t, dm("id", "child_of", fx.id["asia"])),
		"asia", "cn", "jp", "shenzhen")

	// 整棵树
	assertNames(t, fx.names(t, dm("id", "child_of", fx.id["root"])),
		"asia", "cn", "de", "europe", "jp", "root", "shenzhen")

	// 叶子节点：只有自己
	assertNames(t, fx.names(t, dm("id", "child_of", fx.id["shenzhen"])), "shenzhen")
}

// parent_of 取的是祖先**含自身**。
func TestHierarchy_ParentOfById(t *testing.T) {
	fx := newHierarchyFixture(t)

	assertNames(t, fx.names(t, dm("id", "parent_of", fx.id["shenzhen"])),
		"asia", "cn", "root", "shenzhen")

	// 根节点：只有自己
	assertNames(t, fx.names(t, dm("id", "parent_of", fx.id["root"])), "root")
}

// 多个种子取并集，且不重复。
func TestHierarchy_MultipleSeeds(t *testing.T) {
	fx := newHierarchyFixture(t)

	dom := fmtDomain("[('id','child_of',[%d,%d])]", fx.id["cn"], fx.id["europe"])
	assertNames(t, fx.names(t, dom), "cn", "de", "europe", "shenzhen")

	// 种子互为祖先后代时不能出现重复行
	dom = fmtDomain("[('id','child_of',[%d,%d])]", fx.id["asia"], fx.id["cn"])
	assertNames(t, fx.names(t, dom), "asia", "cn", "jp", "shenzhen")
}

// 空种子恒假，绝不能变成恒真（那正是修复前"筛了等于没筛"的形状）。
func TestHierarchy_EmptySeedIsFalse(t *testing.T) {
	fx := newHierarchyFixture(t)

	if got := fx.names(t, `[('id','child_of',False)]`); len(got) != 0 {
		t.Fatalf("child_of False 应当一条都不匹配，实得 %v", got)
	}
	if got := fx.names(t, `[('id','parent_of',False)]`); len(got) != 0 {
		t.Fatalf("parent_of False 应当一条都不匹配，实得 %v", got)
	}
}

// 层级条件必须能和别的条件正常 AND，不能把兄弟条件挤掉。
func TestHierarchy_CombinesWithOtherConditions(t *testing.T) {
	fx := newHierarchyFixture(t)

	dom := fmtDomain("['&',('id','child_of',%d),('name','!=','cn')]", fx.id["asia"])
	assertNames(t, fx.names(t, dom), "asia", "jp", "shenzhen")
}

// ---------- 自引用 m2o 上的 child_of ----------

// ('parent_id','child_of',x)：字段本身就是父链接，应当在**本模型**里沿它走树，
// 落成 (id in [...])，而不是把 parent_id 当普通外键去匹配。
func TestHierarchy_OnSelfReferencingM2O(t *testing.T) {
	fx := newHierarchyFixture(t)

	assertNames(t, fx.names(t, dm("parent_id", "child_of", fx.id["asia"])),
		"asia", "cn", "jp", "shenzhen")
}

// ---------- 对端是别的模型 ----------

// ('company_id','child_of',asia)：在 company 里展开 asia 的后代，
// 再落成 (company_id in [后代...])。走错模型会得到一棵错的树。
func TestHierarchy_OnForeignM2O(t *testing.T) {
	fx := newHierarchyFixture(t)

	for _, c := range []struct{ name, company string }{
		{"O-root", "root"}, {"O-asia", "asia"}, {"O-cn", "cn"},
		{"O-shenzhen", "shenzhen"}, {"O-de", "de"},
	} {
		if _, err := fx.o.Model("hr.order").Create(map[string]any{
			"name": c.name, "company_id": fx.id[c.company]}); err != nil {
			t.Fatal(err)
		}
	}

	ds, err := fx.o.Model("hr.order").
		Domain(dm("company_id", "child_of", fx.id["asia"])).
		Limit(-1).OrderBy("name").Read()
	if err != nil {
		t.Fatalf("child_of on foreign m2o: %v", err)
	}
	var got []string
	ds.First()
	for !ds.Eof() {
		got = append(got, ds.Record().FieldByName("name").AsString())
		ds.Next()
	}
	assertNames(t, got, "O-asia", "O-cn", "O-shenzhen")
}

// ('company_id','parent_of',shenzhen)：正是 vectors 那 27 条存量记录规则的形态
// ——"这条记录的公司是我某个公司的祖先（含自身）"。规则筛不出来就是一次越权，
// 所以既要断言筛出了什么，也要断言**没**筛出什么。
func TestHierarchy_ParentOfOnForeignM2O(t *testing.T) {
	fx := newHierarchyFixture(t)

	for _, c := range []struct{ name, company string }{
		{"P-root", "root"}, {"P-asia", "asia"}, {"P-cn", "cn"},
		{"P-shenzhen", "shenzhen"}, {"P-jp", "jp"}, {"P-de", "de"},
	} {
		if _, err := fx.o.Model("hr.order").Create(map[string]any{
			"name": c.name, "company_id": fx.id[c.company]}); err != nil {
			t.Fatal(err)
		}
	}

	ds, err := fx.o.Model("hr.order").
		Domain(dm("company_id", "parent_of", fx.id["shenzhen"])).
		Limit(-1).OrderBy("name").Read()
	if err != nil {
		t.Fatalf("parent_of on foreign m2o: %v", err)
	}
	var got []string
	ds.First()
	for !ds.Eof() {
		got = append(got, ds.Record().FieldByName("name").AsString())
		ds.Next()
	}
	// shenzhen 的祖先链是 root→asia→cn→shenzhen；jp / de 不在链上，必须被排除。
	assertNames(t, got, "P-asia", "P-cn", "P-root", "P-shenzhen")
}

// ---------- 坏数据 ----------

// 环形父子关系不能把展开卡死：已访问集合保证每个 id 只入队一次。
func TestHierarchy_CycleTerminates(t *testing.T) {
	fx := newHierarchyFixture(t)

	// 把 root 的父指向它自己的孙子，造出 root→asia→cn→root 的环
	if _, err := fx.o.Model("hr.company").Ids(fx.id["root"]).
		Write(map[string]any{"parent_id": fx.id["cn"]}); err != nil {
		t.Fatal(err)
	}

	done := make(chan []string, 1)
	go func() { done <- fx.names(t, dm("id", "child_of", fx.id["root"])) }()
	select {
	case got := <-done:
		// 环上的每个节点都是彼此的后代，整棵树都在结果里；关键是**它返回了**。
		if len(got) == 0 {
			t.Fatal("环形数据下应当仍能返回结果")
		}
	case <-timeoutAfter():
		t.Fatal("环形父子关系把层级展开卡死了")
	}
}

// 父链接推断不出来时必须报错并说清原因，绝不静默返回全表。
func TestHierarchy_AmbiguousParentIsRejected(t *testing.T) {
	ds := &TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "amb.db")}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.SyncModel("", new(hrTwoParents)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	if _, err := o.Model("hr.two.parents").Create(map[string]any{"name": "x"}); err != nil {
		t.Fatal(err)
	}

	_, err = o.Model("hr.two.parents").Domain(`[('id','child_of',1)]`).Limit(-1).Read()
	if err == nil {
		t.Fatal("有两个自引用 m2o 时应当报歧义，而不是随便挑一个走出一棵错的树")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("错误信息应点明是歧义: %v", err)
	}
}

// ---------- 元数据丢失的父链接（微服务拓扑） ----------

// 一个模型的属主在别的进程、而各进程共用同一个库时，本进程从**表结构**反射出一份
// 同名模型：列都在、值读得对，但 m2o 这件事没了 —— `parent_id` 只是一根整型列，
// relation 是空串。前三级推断全部落空，child_of 于是报"这个模型没有父链接"，
// 而字段明明白白写在属主的源码里。
//
// 2026-09-03 真栈：salesvc（sale 独立进程，res.partner 属主是 kylin）上
// `/my/orders` 与 `/my/quotes` **恒 500** —— 客户门户里"我的订单""我的报价"两页
// 整整打不开；同一条 `partner_id child_of` 规则在 kylin 的 /my/invoices 一切正常。
// "别的页面好好的"正是这类 bug 的指纹。
func TestHierarchy_ReflectedParentColumnWithoutRelationMeta(t *testing.T) {
	ds := &TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "reflected.db")}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.SyncModel("", new(hrReflected)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	ids := map[string]int64{}
	for _, c := range []struct{ name, parent string }{
		{"root", ""}, {"asia", "root"}, {"cn", "asia"}, {"europe", "root"},
	} {
		vals := map[string]any{"name": c.name}
		if c.parent != "" {
			vals["parent_id"] = ids[c.parent]
		}
		created, err := o.Model("hr.reflected").Create(vals)
		if err != nil {
			t.Fatal(err)
		}
		ids[c.name] = utils.ToInt64(created[0])
	}

	dsRes, err := o.Model("hr.reflected").
		Domain(dm("id", "child_of", ids["asia"])).Limit(-1).OrderBy("name").Read()
	if err != nil {
		t.Fatalf("元数据缺席时 child_of 应当按列名 parent_id 走树，而不是报错: %v", err)
	}
	var got []string
	dsRes.First()
	for !dsRes.Eof() {
		got = append(got, dsRes.Record().FieldByName("name").AsString())
		dsRes.Next()
	}
	assertNames(t, got, "asia", "cn")
}

// hrReflected 模拟反射出来的模型：parent_id 是一根裸整型列，没有任何关系元数据。
type hrReflected struct {
	TModel   `table:"name('hr_reflected')"`
	Id       int64  `field:"pk autoincr"`
	Name     string `field:"varchar(64) recname()"`
	ParentId int64  `field:"bigint"`
}

// 放宽只针对"没有关系元数据可参考"。relation 有值却指向**别的**模型时，
// parent_id 就真的不是自引用 —— 那才是会产出错树的情形，必须照旧报错。
func TestHierarchy_ParentIdPointingElsewhereIsStillRejected(t *testing.T) {
	ds := &TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "foreignparent.db")}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.SyncModel("", new(hrCompany), new(hrForeignParent)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	if _, err := o.Model("hr.foreign.parent").Create(map[string]any{"name": "x"}); err != nil {
		t.Fatal(err)
	}

	_, err = o.Model("hr.foreign.parent").Domain(`[('id','child_of',1)]`).Limit(-1).Read()
	if err == nil {
		t.Fatal("parent_id 指向别的模型时不该被当成自引用父链接 —— 那会走出一棵错的树")
	}
}

type hrForeignParent struct {
	TModel   `table:"name('hr_foreign_parent')"`
	Id       int64  `field:"pk autoincr"`
	Name     string `field:"varchar(64)"`
	ParentId int64  `field:"many2one(hr_company)"`
}

type hrTwoParents struct {
	TModel   `table:"name('hr_two_parents')"`
	Id       int64  `field:"pk autoincr"`
	Name     string `field:"varchar(64)"`
	OwnerId  int64  `field:"many2one(hr_two_parents)"`
	MasterId int64  `field:"many2one(hr_two_parents)"`
}

// ---------- 小工具 ----------

func dm(field, op string, id int64) string {
	return fmt.Sprintf("[('%s','%s',%d)]", field, op, id)
}

func fmtDomain(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}

func timeoutAfter() <-chan time.Time {
	return time.After(10 * time.Second)
}
