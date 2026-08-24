package domain

import "testing"

/*
TDomainNode 这层的"判断/边界"回归。这些都不是崩溃就是恒真恒假的谓词，
上游（expr.go）拿它们分流，错一个就是一条筛选条件悄悄失效。
*/

// Clone 必须是深拷贝：曾经直接共享 children 切片，在克隆体上 Remove/Insert 会
// 就地移动元素，把原树一起改坏。
func TestClone_IsDeepCopy(t *testing.T) {
	a := NewDomainNode()
	a.Push("x", "y", "z")

	b := a.Clone()
	b.Remove(0)
	if got := Domain2String(a); got != `["x","y","z"]` {
		t.Fatalf("在克隆体上 Remove 改坏了原树: %s", got)
	}

	c := a.Clone()
	c.Insert(0, "w")
	if got := Domain2String(a); got != `["x","y","z"]` {
		t.Fatalf("在克隆体上 Insert 改坏了原树: %s", got)
	}

	// 嵌套子树同样不能共享
	root := NewDomainNode()
	root.Push(New("f", "=", 1))
	d := root.Clone()
	d.Item(0).Item(0).Value = "changed"
	if got := root.Item(0).Item(0).String(); got != "f" {
		t.Fatalf("深层节点被共享了: %s", got)
	}
}

// IsEmpty 说的是"既没有值也没有孩子"。原式 `n==0 || (n==0 && Value==nil)`
// 后半永远被短路，等价于只判孩子数，于是有值的值节点被判成空。
func TestIsEmpty_ConsidersValue(t *testing.T) {
	if NewDomainNode("hello").IsEmpty() {
		t.Fatal(`值节点 "hello" 不该判为空`)
	}
	if NewDomainNode(0).IsEmpty() {
		t.Fatal("值节点 0 不该判为空")
	}
	if !NewDomainNode().IsEmpty() {
		t.Fatal("全新空节点应当判为空")
	}
	if New("a", "=", 1).IsEmpty() {
		t.Fatal("叶子不该判为空")
	}
}

// 空集合上不能返回"真空真"——上游拿这两个谓词给右值分流。
func TestTypePredicates_EmptyIsFalse(t *testing.T) {
	e := NewDomainNode()
	if e.IsStringList() {
		t.Fatal("空节点不该判成 IsStringList")
	}
	if e.IsIntLeaf() {
		t.Fatal("空节点不该判成 IsIntLeaf")
	}

	strs := NewDomainNode()
	strs.Push("a", "b")
	if !strs.IsStringList() {
		t.Fatal(`["a","b"] 应当是 IsStringList`)
	}
	nums := NewDomainNode()
	nums.Push(1, 2)
	if !nums.IsIntLeaf() {
		t.Fatal("[1,2] 应当是 IsIntLeaf")
	}
}

// Item() 的契约是"取不到就 panic"（调用方都先校验过 Count），这里只要求**两个方向
// 的越界表现一致**：负下标原来绕过了上界检查，落到 children[-1] 抛的是
// `index out of range`，那条带 PrintDomain 上下文的显式报错反而走不到。
func TestItem_OutOfRangePanicsExplicitly(t *testing.T) {
	n := NewDomainNode()
	n.Push("a", "b")

	for _, idx := range []int{-1, 99} {
		func() {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("Item(%d) 应当 panic", idx)
				}
				msg, _ := r.(string)
				if msg == "" {
					t.Fatalf("Item(%d) 抛的不是显式报错: %v", idx, r)
				}
			}()
			n.Item(idx)
		}()
	}
}

// 越界的 Remove/Insert 一律不许 panic。
func TestNodeAccess_OutOfRangeIsSafe(t *testing.T) {
	n := NewDomainNode()
	n.Push("a", "b")

	n.Remove(99)
	n.Remove(-1)
	if n.Count() != 2 {
		t.Fatalf("越界 Remove 不该改动节点，实得 %d 个孩子", n.Count())
	}

	m := NewDomainNode()
	m.Push("a", "b")
	m.Insert(99, "c")
	if m.Count() != 3 {
		t.Fatalf("越界 Insert 后应有 3 个孩子，实得 %d", m.Count())
	}
}

// Strings() 曾经硬断言 .(string)，数字/布尔直接 panic——而调用点包括
// leaf_to_sql 的错误日志，等于"一报错就崩"。
func TestStrings_NonStringValuesAreSafe(t *testing.T) {
	leaf := New("id", "=", 1)
	got := leaf.Strings()
	if len(got) != 3 || got[0] != "id" || got[1] != "=" || got[2] != "1" {
		t.Fatalf("Strings() = %v", got)
	}

	if v := NewDomainNode(true).Strings(); len(v) != 1 || v[0] != "true" {
		t.Fatalf("布尔值节点 Strings() = %v", v)
	}
	// 越界的下标形式不许 panic
	if v := leaf.Strings(99); len(v) != 0 {
		t.Fatalf("Strings(99) 应回空，实得 %v", v)
	}
}

// Push/Pop 必须对称：栈里只剩一个元素时它是 VALUE_NODE，Pop 也得能取出来
// （原来只认 LIST_NODE，返回 nil 让调用方空指针崩溃）。
func TestPop_WorksOnSingleValueStack(t *testing.T) {
	stack := NewDomainNode()
	stack.Push("only")

	one := stack.Pop()
	if one == nil {
		t.Fatal("单元素栈 Pop() 回了 nil")
	}
	if one.String() != "only" {
		t.Fatalf("Pop() = %q", one.String())
	}
	if stack.Pop() != nil {
		t.Fatal("空栈 Pop() 应回 nil")
	}

	// 多元素仍是后进先出
	stack = NewDomainNode()
	stack.Push("a", "b", "c")
	for _, want := range []string{"c", "b", "a"} {
		got := stack.Pop()
		if got == nil || got.String() != want {
			t.Fatalf("Pop() 期望 %q，实得 %v", want, got)
		}
	}
	if stack.Pop() != nil {
		t.Fatal("取空之后应回 nil")
	}
}

// 恒真/恒假常量必须按结构判，不能拿渲染出来的字符串去比常量
// （常量带空格与单引号，渲染结果不带，两者永远不相等）。
func TestConstLeaf_MatchesStructurally(t *testing.T) {
	tn, err := String2Domain(TRUE_DOMAIN, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !tn.IsTrueLeaf() {
		t.Fatalf("TRUE_DOMAIN 解析出来的 %s 没被认成恒真叶子", Domain2String(tn))
	}
	if tn.IsFalseLeaf() {
		t.Fatal("恒真叶子被认成了恒假")
	}

	fn, err := String2Domain(FALSE_DOMAIN, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !fn.IsFalseLeaf() {
		t.Fatalf("FALSE_DOMAIN 解析出来的 %s 没被认成恒假叶子", Domain2String(fn))
	}

	if New("name", "=", "x").IsTrueLeaf() {
		t.Fatal("普通叶子被认成了恒真")
	}
}

// 不带引号的 True/False 是布尔字面量；带引号的仍是字符串。
func TestParse_BoolLiterals(t *testing.T) {
	for _, c := range []struct {
		src  string
		want any
	}{
		{`[('flag','=',True)]`, true},
		{`[('flag','=',False)]`, false},
		{`[('flag','=',true)]`, true},
		{`[('flag','=',false)]`, false},
		{`[('flag','=','True')]`, "True"},
		{`[('flag','=',"False")]`, "False"},
	} {
		n, err := String2Domain(c.src, nil)
		if err != nil {
			t.Fatalf("%s: %v", c.src, err)
		}
		got := n.Item(2).Value
		if got != c.want {
			t.Fatalf("%s 的右值 = %#v，期望 %#v", c.src, got, c.want)
		}
	}
}

// 往一条已有的 domain 上再叠加一个条件（where_calc 的 active_test 就是这么干的）：
// 无论原 domain 是**裸叶子**还是列表，合并结果都必须是合法的前缀式 domain。
//
// 曾经用的是 node.Insert(0, extra)：String2Domain 对单条件返回的是裸叶子本身，
// Insert 会把新条件插进那个叶子内部，变成 4 个孩子的畸形节点。
func TestMergeExtraCondition_KeepsWellFormedShape(t *testing.T) {
	extra := func() *TDomainNode {
		n, err := String2Domain(`[('active','=',1)]`, nil)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}

	// 原 domain 是裸叶子（单条件）
	single, _ := String2Domain(`[('name','=','x')]`, nil)
	merged := single.Clone()
	merged.AND(extra())
	if got := Domain2String(merged); got != `["&",("name","=","x"),("active","=",1)]` {
		t.Fatalf("单条件合并结果: %s", got)
	}
	if got := Domain2String(single); got != `("name","=","x")` {
		t.Fatalf("原 domain 被就地改动了: %s", got)
	}

	// 原 domain 已经是带操作符的列表
	multi, _ := String2Domain(`['|', ('a','=',1), ('b','=',2)]`, nil)
	merged = multi.Clone()
	merged.AND(extra())
	if got := Domain2String(merged); got != `["&","|",("a","=",1),("b","=",2),("active","=",1)]` {
		t.Fatalf("多条件合并结果: %s", got)
	}
}
