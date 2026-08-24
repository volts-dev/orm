package orm

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/volts-dev/orm/domain"
)

/*
domain 里的 '!' 曾经会让**整条 WHERE 静默消失**。

distribute_not 把取反后的三元组用 `result.Push(left, op, right)` 推进结果里，而 Push
是变参追加、不是"用这三样造一个叶子"，于是

	['!','&',('user_id','=',4),('partner_id','in',[1,2])]
	-> ["|","user_id","!=",4,"partner_id","not in",[1,2]]     7 个平级孩子

回到 parse() 时第二个孩子 "user_id" 是 Count()==0 的值节点，撞上畸形叶子闸门，而那道
闸门当时对 cnt==0 写的是 `return nil`——连栈里没处理完的条件一起丢，返回值还是"成功"。
外部特征：一加 '!' 就整表返回，全程不报错；同一个 domain 交给 Delete() 会**删光整表**
（hasCondition() 只看 domain 结构非空就放行了 AllowUnsafe 守卫）。

另一条独立的崩溃路径：'!' 碰上没有取反映射的操作符（=like/=ilike/=?/child_of）时会被
保留到 toSql，那里写的是 `stack.Push("(NOT (%s))", stack.Pop().String())`——Push 不是
Printf，而且栈里只剩一个元素时它是 VALUE_NODE，旧版 Pop() 只认 LIST_NODE 返回 nil，
`.String()` 当场空指针崩溃。
*/

type NotDomRec struct {
	TModel `table:"name('not_dom_rec')"`
	Id     int64  `field:"pk autoincr title('ID')"`
	Name   string `field:"varchar() size(64)"`
	Kind   string `field:"varchar() size(16)"`
	Num    int    `field:"int()"`
	Flag   bool   `field:"bool"`
}

// newNotDomOrm 建三条数据：a/b/c，num 分别 1/1/2，kind 分别 x/x/y，flag 分别 真/真/假。
func newNotDomOrm(t *testing.T) *TOrm {
	t.Helper()
	ds := &TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "notdom.db")}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(NotDomRec)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	for _, r := range []struct {
		name, kind string
		num        int
		flag       bool
	}{
		{"a", "x", 1, true}, {"b", "x", 1, true}, {"c", "y", 2, false},
	} {
		if _, err := o.Model("not.dom.rec").Create(map[string]any{
			"name": r.name, "kind": r.kind, "num": r.num, "flag": r.flag,
		}); err != nil {
			t.Fatal(err)
		}
	}
	return o
}

func notDomCount(t *testing.T, o *TOrm, dom string) int {
	t.Helper()
	ds, err := o.Model("not.dom.rec").Domain(dom).Limit(-1).Read()
	if err != nil {
		t.Fatalf("domain %s: %v", dom, err)
	}
	if ds == nil {
		return 0
	}
	return ds.Count()
}

// distribute_not 必须产出**结构合法**的 domain：取反后的三元组要落成一个叶子，
// 而不是三个平级孩子。
func TestDistributeNot_KeepsLeafStructure(t *testing.T) {
	for _, c := range []struct {
		src  string
		want string
	}{
		{
			`['!', '&', ('user_id','=',4), ('partner_id','in',[1,2])]`,
			`["|",("user_id","!=",4),("partner_id","not in",[1,2])]`,
		},
		{
			`['!', ('user_id','=',4)]`,
			`[("user_id","!=",4)]`,
		},
		{
			`['!', '|', ('a','=',1), ('b','=',2)]`,
			`["&",("a","!=",1),("b","!=",2)]`,
		},
	} {
		node, err := domain.String2Domain(c.src, nil)
		if err != nil {
			t.Fatalf("%s: %v", c.src, err)
		}
		nn, err := normalize_domain(node)
		if err != nil {
			t.Fatalf("%s: %v", c.src, err)
		}
		got := domain.Domain2String(distribute_not(nn))
		if got != c.want {
			t.Fatalf("distribute_not(%s)\n 得到 %s\n 期望 %s", c.src, got, c.want)
		}
	}
}

// 每个平级孩子都必须是叶子或操作符，绝不能出现裸的值节点——那正是让整条 WHERE
// 蒸发的形状。
func TestDistributeNot_NoBareValueNodes(t *testing.T) {
	node, _ := domain.String2Domain(`['!', '&', ('a','=',1), ('b','in',[1,2])]`, nil)
	nn, _ := normalize_domain(node)
	for i, n := range distribute_not(nn).Nodes() {
		if n.IsValueNode() && !n.IsDomainOperator() {
			t.Fatalf("第 %d 项是裸值节点 %q —— 结构已经坏了", i, n.String())
		}
	}
}

func TestDomain_NotOperator_DoesNotWidenQuery(t *testing.T) {
	o := newNotDomOrm(t)

	if got := notDomCount(t, o, `[('name','!=','a')]`); got != 2 {
		t.Fatalf("对照组 name != a 应有 2 条，实得 %d", got)
	}
	if got := notDomCount(t, o, `['!', ('name','=','a')]`); got != 2 {
		t.Fatalf(`['!',(name='a')] 应有 2 条，实得 %d —— '!' 把条件丢了，整表返回`, got)
	}
	// !(name=a AND num=1) == name!=a OR num!=1 -> b 被 num!=1 排除? 不，b 的 num=1 且 name=b，
	// 满足 name!=a，所以 b、c 都在内，a 不在。
	if got := notDomCount(t, o, `['!', '&', ('name','=','a'), ('num','=',1)]`); got != 2 {
		t.Fatalf(`['!','&',...] 应有 2 条，实得 %d`, got)
	}
	// !(num=1) 只剩 c
	if got := notDomCount(t, o, `['!', ('num','=',1)]`); got != 1 {
		t.Fatalf(`['!',(num=1)] 应有 1 条，实得 %d`, got)
	}
	// 正常条件与 '!' 条件混合：kind=x 的两条里排掉 num=1 -> 0 条
	if got := notDomCount(t, o, `['&', ('kind','=','x'), '!', ('num','=',1)]`); got != 0 {
		t.Fatalf(`['&',(kind=x),'!',(num=1)] 应有 0 条，实得 %d —— '!' 那半边被丢了`, got)
	}
}

// '!' 配没有取反映射的操作符（=like 一族）必须生成 NOT(...)，而不是崩溃、
// 也不是把字面量 "(NOT (%s))" 拼进 SQL。
func TestDomain_NotOperator_WithoutNegationMapping(t *testing.T) {
	o := newNotDomOrm(t)
	ds, err := o.Model("not.dom.rec").Domain(`['!', ('name','=like','a')]`).Limit(-1).Read()
	if err != nil {
		t.Fatalf("['!',(name =like a)] 报错: %v", err)
	}
	if ds == nil || ds.Count() != 2 {
		t.Fatalf(`['!',(name =like 'a')] 应有 2 条，实得 %v`, ds.Count())
	}
}

// 最坏的下游：条件被丢掉后 Delete 会删光整表。
func TestDomain_NotOperator_DeleteStaysScoped(t *testing.T) {
	o := newNotDomOrm(t)

	n, err := o.Model("not.dom.rec").Domain(`['!', ('name','=','a')]`).Delete()
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if n != 2 {
		t.Fatalf(`Domain(['!',(name='a')]).Delete() 应删 2 条，实删 %d`, n)
	}
	if got := notDomCount(t, o, `[('name','=','a')]`); got != 1 {
		t.Fatalf("name='a' 那条被误删了，剩 %d", got)
	}
}

// 畸形叶子必须报错，不能"静默跳过"——静默跳过等于把条件从 AND 里摘掉。
func TestParse_MalformedLeafIsAnError(t *testing.T) {
	o := newNotDomOrm(t)
	bad := domain.NewDomainNode()
	bad.Push(domain.New("name", "=", "a"))
	bad.Push(domain.NewDomainNode()) // 一个空项

	_, err := o.Model("not.dom.rec").Domain(bad).Limit(-1).Read()
	if err == nil {
		t.Fatal("畸形 domain 应当报错，实际静默通过了")
	}
	if !strings.Contains(err.Error(), "invalid domain leaf") {
		t.Fatalf("错误信息没点名畸形叶子: %v", err)
	}
}
