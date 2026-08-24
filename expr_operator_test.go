package orm

import (
	"strings"
	"testing"

	"github.com/volts-dev/orm/domain"
)

/*
term 操作符落 SQL 时的语义回归。曾经的错法都是"看着正常的错数据"：

  - `not in` 只给一个值时被硬写成 `=` —— 解析器会把单元素列表拆包成标量
    （parser.go 的 `if list.Count()==1 { return list.Item(0) }`），于是
    `.NotIn("name","a")` 生成 `WHERE name = 'a'`，**恰好只回被排除的那一条**。
    两个及以上值走列表分支才是对的，所以单参数用法一直没被测出来。
  - `not in []` 落成 `= NULL` 回 0 条（应回全部）。
  - `('x','in',[1,2,False])` 里的 false 没被剔除：SliceDelete 是**按值**删除，
    传进去的却是下标。
  - `child_of` 直接空指针崩溃（NameSearch 的 error 被丢，返回 nil 后取 .Data）。
  - like/ilike 硬拼 postgres 的 `::text`，sqlite/mysql 上直接语法报错。
*/

func TestDomain_NotIn_SingleValueIsNotEquality(t *testing.T) {
	o := newNotDomOrm(t) // a/b/c

	if got := notDomCount(t, o, `[('name','not in',['a'])]`); got != 2 {
		t.Fatalf(`(name not in ['a']) 应有 2 条，实得 %d —— not in 被写成了 =`, got)
	}
	if got := notDomCount(t, o, `[('name','in',['a'])]`); got != 1 {
		t.Fatalf(`(name in ['a']) 应有 1 条，实得 %d`, got)
	}
	// 多值对照组（历史上唯一被测到的形态）
	if got := notDomCount(t, o, `[('name','not in',['a','b'])]`); got != 1 {
		t.Fatalf(`(name not in ['a','b']) 应有 1 条，实得 %d`, got)
	}
}

// 语句层的 In()/NotIn() 走 domain.IN/NotIn，同样会被单值拆包坑到。
func TestStatement_NotIn_SingleArg(t *testing.T) {
	o := newNotDomOrm(t)

	ds, err := o.Model("not.dom.rec").NotIn("name", "a").Limit(-1).Read()
	if err != nil {
		t.Fatal(err)
	}
	if ds.Count() != 2 {
		t.Fatalf(`.NotIn("name","a") 应有 2 条，实得 %d —— 反而只回了被排除的那条`, ds.Count())
	}

	ds, err = o.Model("not.dom.rec").In("name", "a").Limit(-1).Read()
	if err != nil {
		t.Fatal(err)
	}
	if ds.Count() != 1 {
		t.Fatalf(`.In("name","a") 应有 1 条，实得 %d`, ds.Count())
	}
}

// 空集合：in [] 恒假，not in [] 恒真。
func TestDomain_EmptyInList(t *testing.T) {
	o := newNotDomOrm(t)

	if got := notDomCount(t, o, `[('name','in',[])]`); got != 0 {
		t.Fatalf("(name in []) 应有 0 条，实得 %d", got)
	}
	if got := notDomCount(t, o, `[('name','not in',[])]`); got != 3 {
		t.Fatalf("(name not in []) 应有 3 条（全部），实得 %d", got)
	}
}

// child_of / parent_of 目前没实现：必须明确报错，不能崩溃、也不能把条件静默丢掉
// （丢掉的后果是整表返回）。
// 层级操作符用在**没有父链接**的模型上必须响亮报错。
//
// not.dom.rec 上没有任何自引用 many2one，走不了树。历史上这里是空实现——条件被
// 静默丢掉、整表返回；实现之后错法换了一种（推断不出父字段），但"绝不静默"这条
// 不变：child_of 筛出全表是一次越权，比查不出来贵得多。
// 正常层级查询的行为见 expr_hierarchy_test.go。
func TestDomain_HierarchyOperatorFailsLoudly(t *testing.T) {
	o := newNotDomOrm(t)

	for _, dom := range []string{
		`[('id','child_of',1)]`,
		`[('id','parent_of',1)]`,
	} {
		_, err := o.Model("not.dom.rec").Domain(dom).Limit(-1).Read()
		if err == nil {
			t.Fatalf("%s 应当报错（模型上没有父链接），实际静默通过 —— 条件被丢掉等于整表返回", dom)
		}
		if !strings.Contains(err.Error(), "self-referencing many2one") {
			t.Fatalf("%s 的错误信息没说清缺的是父链接: %v", dom, err)
		}
	}
}

// like/ilike 必须在任何方言上都能跑（`::text` 是 postgres 专有语法）。
func TestDomain_LikeAcrossDialects(t *testing.T) {
	o := newNotDomOrm(t)

	if got := notDomCount(t, o, `[('name','like','a')]`); got != 1 {
		t.Fatalf("(name like 'a') 应有 1 条，实得 %d", got)
	}
	if got := notDomCount(t, o, `[('name','ilike','A')]`); got != 1 {
		t.Fatalf("(name ilike 'A') 应有 1 条，实得 %d", got)
	}
	// =like 是"原样"变体，通配符由调用方自带
	if got := notDomCount(t, o, `[('name','=like','a')]`); got != 1 {
		t.Fatalf("(name =like 'a') 应有 1 条，实得 %d", got)
	}
	if got := notDomCount(t, o, `[('name','not like','a')]`); got != 2 {
		t.Fatalf("(name not like 'a') 应有 2 条，实得 %d", got)
	}
}

// True/False 字面量必须解析成布尔，而不是字符串 "True"。
// 项目自己的 o2m 字段声明就写着 domain([('active','=',True)])。
func TestDomain_PythonBoolLiterals(t *testing.T) {
	o := newNotDomOrm(t)

	if got := notDomCount(t, o, `[('num','=',1)]`); got != 2 {
		t.Fatalf("对照组 (num=1) 应有 2 条，实得 %d", got)
	}
	if got := notDomCount(t, o, `[('flag','=',True)]`); got != 2 {
		t.Fatalf("(flag=True) 应有 2 条，实得 %d —— True 被当成字符串了", got)
	}
	if got := notDomCount(t, o, `[('flag','=',False)]`); got != 1 {
		t.Fatalf("(flag=False) 应有 1 条，实得 %d", got)
	}
	if got := notDomCount(t, o, `[('flag','=',true)]`); got != 2 {
		t.Fatalf("(flag=true 小写) 应有 2 条，实得 %d", got)
	}
	// 加了引号就仍然是字符串，不能被当成布尔
	if got := notDomCount(t, o, `[('name','=','True')]`); got != 0 {
		t.Fatalf(`(name='True') 应有 0 条（字符串比较），实得 %d`, got)
	}
}

// `in` 列表里的布尔 false 必须被**剔除**并转成 IS NULL 语义，占位符个数要跟着走。
//
// 原来用的是 `SliceDelete(res_params, any(idx))`——SliceDelete 是**按值**删除，
// 传进去的却是下标（而且 idx 是 int、参数多是 int64，类型都对不上）。于是 false
// 原样留在绑定参数里：postgres 上整型列直接
// `operator does not exist: bigint = boolean`；万一按值命中，又会删掉一个真 id
// 并让占位符个数与参数个数对不上。
func TestLeafToSql_InListDropsFalseAndKeepsHolderCount(t *testing.T) {
	o := newNotDomOrm(t)
	model, err := o.GetModel("not.dom.rec")
	if err != nil {
		t.Fatal(err)
	}

	exp, err := NewExpression(o, model.GetBase(), domain.New("num", "in", 1, 2, false), nil)
	if err != nil {
		t.Fatal(err)
	}
	clause, params := exp.ToSql()
	sql := strings.Join(clause, " ")

	if len(params) != 2 {
		t.Fatalf("false 应当被剔除，只剩 2 个参数，实得 %d 个: %v", len(params), params)
	}
	for _, p := range params {
		if _, isBool := p.(bool); isBool {
			t.Fatalf("布尔值漏进了绑定参数: %v", params)
		}
	}
	if n := strings.Count(sql, "?"); n != len(params) {
		t.Fatalf("占位符 %d 个但参数 %d 个，对不上: %s / %v", n, len(params), sql, params)
	}
	if !strings.Contains(sql, "IS NULL") {
		t.Fatalf("列表里有 false 时应当补上 IS NULL: %s", sql)
	}
}

// 算不出 WHERE 的域不能骗过危险操作守卫。
func TestUnsafeGuard_DomainWithoutWhereIsNotACondition(t *testing.T) {
	o := newNotDomOrm(t)

	for _, dom := range []string{``, `[]`} {
		n, err := o.Model("not.dom.rec").Domain(dom).Delete()
		if err == nil {
			t.Fatalf("Domain(%q).Delete() 应被守卫拦下，实际删了 %d 条", dom, n)
		}
	}
	if got := notDomCount(t, o, `[('num','>',0)]`); got != 3 {
		t.Fatalf("数据被误删了，只剩 %d 条", got)
	}
}

// 三元值列表不能被 IsLeafNode() 的记忆化改写成"叶子"后当成标量。
//
// IsLeafNode() 认出三元 LIST_NODE 是叶子后会就地把 nodeType 改成 LEAF_NODE。于是
// `('kind','in',['x','=','y'])` 这种**中间那个值恰好是 term 操作符**的值列表，
// 只要这棵树被渲染过一次（打一遍日志就够），IsListNode() 就变成 false，整组值被
// 当成标量、Value 又是 nil —— 条件恒不匹配。判据改用 Count()。
func TestDomain_ThreeValueListIsNotMistakenForALeaf(t *testing.T) {
	o := newNotDomOrm(t) // kind: x/x/y

	node := domain.New("kind", "in", "x", "=", "zzz")
	_ = domain.Domain2String(node) // 先渲染一次，触发记忆化

	ds, err := o.Model("not.dom.rec").Domain(node).Limit(-1).Read()
	if err != nil {
		t.Fatal(err)
	}
	if ds.Count() != 2 {
		t.Fatalf(`(kind in ['x','=','zzz']) 应有 2 条，实得 %d —— 值列表被当成标量了`, ds.Count())
	}
}
