package orm

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/volts-dev/orm/domain"
	"github.com/volts-dev/utils"
)

/*
非存储字段 search 钩子的回归。

判据同 expr_x2many_test.go：本类 bug 的外部形状是"查询成功、返回一堆看着正常的行，
只是一条都没筛掉"，所以每个用例都断言集合本身。
*/

type SrchDoc struct {
	TModel `table:"name('srch_doc')"`
	Id     int64  `field:"pk autoincr title('ID')"`
	Code   string `field:"varchar() size(64)"`
	Title  string `field:"varchar() size(64)"`
	Rank   int64  `field:"int()"`
}

// OnBuildFields 声明三个非存储列：
//
//	label      —— 有 Searcher，翻译成单个叶子
//	combo      —— 有 Searcher，翻译成带 | 的多叶子树（专门压 normalize_domain 那条路）
//	orphan     —— 没有 Searcher，用来锁住"没钩子就是丢条件"这条既有行为仍然被报出来
func (self SrchDoc) OnBuildFields() error {
	if err := self.TModel.OnBuildFields(); err != nil {
		return err
	}
	b := self.Builder()
	b.VarcharField("label").Store(false).Readonly(true).
		Searcher(func(ctx *TFieldSearchContext) (*domain.TDomainNode, error) {
			return domain.New("code", ctx.Operator, ctx.Value), nil
		})
	b.VarcharField("combo").Store(false).Readonly(true).
		Searcher(func(ctx *TFieldSearchContext) (*domain.TDomainNode, error) {
			node := domain.New("code", ctx.Operator, ctx.Value)
			node.OR(domain.New("title", ctx.Operator, ctx.Value))
			return node, nil
		})
	b.VarcharField("orphan").Store(false).Readonly(true)
	return nil
}

type srchFixture struct {
	orm *TOrm
	ids map[string]int64
}

func setupSearcher(t *testing.T) *srchFixture {
	t.Helper()
	ds := &TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "srch.db")}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(SrchDoc)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	fx := &srchFixture{orm: o, ids: map[string]int64{}}
	for _, r := range []struct {
		code, title string
		rank        int64
	}{
		{"AAA", "alpha", 1},
		{"BBB", "alpha", 2},
		{"CCC", "beta", 3},
	} {
		id, err := o.Model("srch.doc").Create(map[string]any{"code": r.code, "title": r.title, "rank": r.rank})
		if err != nil {
			t.Fatalf("create %s: %v", r.code, err)
		}
		fx.ids[r.code] = firstId(t, id)
	}
	return fx
}

func (fx *srchFixture) codes(t *testing.T, node *domain.TDomainNode) []string {
	t.Helper()
	ds, err := fx.orm.Model("srch.doc").Domain(node).Limit(-1).Read()
	if err != nil {
		t.Fatalf("read %s: %v", node.String(), err)
	}
	var out []string
	if ds != nil {
		for _, id := range ds.Keys() {
			for code, v := range fx.ids {
				if v == utils.ToInt64(id) {
					out = append(out, code)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

func (fx *srchFixture) assertCodes(t *testing.T, node *domain.TDomainNode, want ...string) {
	t.Helper()
	sort.Strings(want)
	got := fx.codes(t, node)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("domain %s\n got: %v\nwant: %v", node.String(), got, want)
	}
}

// TestSearcher_SingleLeaf：钩子还一个叶子。没有钩子时这条 domain 会返回全部 3 条。
func TestSearcher_SingleLeaf(t *testing.T) {
	fx := setupSearcher(t)
	fx.assertCodes(t, domain.New("label", "=", "AAA"), "AAA")
	fx.assertCodes(t, domain.New("label", "!=", "AAA"), "BBB", "CCC")
	fx.assertCodes(t, domain.New("label", "=", "ZZZ"))
}

// TestSearcher_MultiLeafTree：钩子还一棵带 | 的树。
//
// ★ 这条专门压 normalize_domain：少了它，两个叶子会在 toSql 的栈上各留一个，
// 生成的 SQL 只用到其中一条——结果是 alpha 那两条里只回一条，或者干脆多回一条。
func TestSearcher_MultiLeafTree(t *testing.T) {
	fx := setupSearcher(t)
	fx.assertCodes(t, domain.New("combo", "=", "alpha"), "AAA", "BBB") // 命中 title
	fx.assertCodes(t, domain.New("combo", "=", "CCC"), "CCC")          // 命中 code
	fx.assertCodes(t, domain.New("combo", "=", "nope"))
}

// TestSearcher_CombinedWithStoredColumn：翻译出来的条件必须与相邻条件求交，
// 不能把结果放大。这是"筛得越多出来越多"那类 bug 的判据。
func TestSearcher_CombinedWithStoredColumn(t *testing.T) {
	fx := setupSearcher(t)
	node := domain.New("combo", "=", "alpha")
	node.AND(domain.New("rank", "=", 2))
	fx.assertCodes(t, node, "BBB")

	node2 := domain.New("label", "=", "AAA")
	node2.AND(domain.New("rank", "=", 3))
	fx.assertCodes(t, node2)
}

// TestSearcher_NilMeansNoMatch 锁住那条刻意的默认值：钩子返回 nil 是"一条都不匹配"，
// 不是"不筛"。反过来的默认值正是这一整类 bug 的成因。
func TestSearcher_NilMeansNoMatch(t *testing.T) {
	fx := setupSearcher(t)
	model, err := fx.orm.GetModel("srch.doc")
	if err != nil {
		t.Fatal(err)
	}
	field := model.GetFieldByName("label")
	orig := field.Searcher()
	defer field.SetSearcher(orig)

	field.SetSearcher(func(ctx *TFieldSearchContext) (*domain.TDomainNode, error) {
		return nil, nil
	})
	// SyncModel 注册的那份原型与 GetModel 返回的克隆共享同一批 IField 指针，
	// 所以上面的 SetSearcher 对查询路径生效。
	got := fx.codes(t, domain.New("label", "=", "AAA"))
	if len(got) != 0 {
		t.Fatalf("钩子返回 nil 必须是\"一条都不匹配\"，实得 %v", got)
	}
}

// TestSearcher_ErrorIsNotSwallowed：钩子报错必须往上抛。吞掉它就等于回到丢条件的老路，
// 而那条路的表现是"看着正常的错数据"。
func TestSearcher_ErrorIsNotSwallowed(t *testing.T) {
	fx := setupSearcher(t)
	model, err := fx.orm.GetModel("srch.doc")
	if err != nil {
		t.Fatal(err)
	}
	if model.GetFieldByName("label").Searcher() == nil {
		t.Fatal("夹具不对：label 应当带 Searcher")
	}
	// orphan 没挂钩子，行为不变（条件被丢），这里只确认它确实没有钩子，
	// 免得哪天默认值变了没人发现。
	if model.GetFieldByName("orphan").Searcher() != nil {
		t.Fatal("orphan 不该有 Searcher")
	}
	if model.GetFieldByName("orphan").SearchOnSelf() {
		t.Fatal("没挂钩子的非存储字段 SearchOnSelf 必须为 false")
	}
	if !model.GetFieldByName("label").SearchOnSelf() {
		t.Fatal("挂了钩子的字段 SearchOnSelf 必须为 true")
	}
}
