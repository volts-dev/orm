package domain

import "testing"

// String() 必须**幂等**：同一棵树渲染多少次都得到同一份文本，且树本身不被改动。
//
// 修之前不是这样。IsLeafNode() 认出叶子后会把 nodeType 就地改写成 LEAF_NODE 作
// 记忆化，而 parseDomain 按 IsListNode() 分派——第一次渲染顺手把子节点改成了
// LEAF_NODE，第二次这些子节点就不再是 LIST_NODE，整条叶子掉进"当成值"那一支被
// Quote() 加上引号：
//
//	1st = ["&",("a","=",7),("b","=","x")]
//	2nd = ["&","(\"a\",\"=\",7)","(\"b\",\"=\",\"x\")"]
//
// ★ 这一步不报错，所以只断言 err==nil 的测试全绿。第二份文本再解析回来时那些
// 叶子已经是普通字符串，ORM 认不出叶子就把**整条 domain 丢掉、返回全表**。
// 外部特征：筛的条件越多，返回的记录越多。
//
// 为什么"调两次"很常见：打一行日志用一次、交给 ORM 再用一次就够了；
// 同一个 domain 节点在重试/多次读里复用也够了。
func TestString_IsIdempotent(t *testing.T) {
	n := NewDomainNode("&",
		New("a", "=", 7),
		New("b", "=", "x"),
	)

	first := n.String()
	for i := 2; i <= 4; i++ {
		if got := n.String(); got != first {
			t.Fatalf("第 %d 次 String() 与第一次不同：\n first = %s\n got   = %s", i, first, got)
		}
	}
}

// 多条件树 String()→解析→String() 必须无损，且叶子还是叶子。
//
// 这是外部真正踩到的形态：改写过的 domain 交回 ORM。
func TestString_MultiConditionRoundTripKeepsLeaves(t *testing.T) {
	n := NewDomainNode("&",
		New("state", "=", "posted"),
		New("journal_id", "not in", 203, 0),
	)

	s1 := n.String()
	back, err := String2Domain(s1, nil)
	if err != nil {
		t.Fatalf("解析不回来：%v（%s）", err, s1)
	}
	if back.Count() != 3 {
		t.Fatalf("往返后条目数变了：%d（%s）", back.Count(), back.String())
	}
	for i := 1; i <= 2; i++ {
		if !back.Item(i).IsLeafNode() {
			t.Fatalf("往返后第 %d 项不是叶子——ORM 会把整条 domain 丢掉、返回全表：%s",
				i, back.String())
		}
	}
	if s2 := back.String(); s2 != s1 {
		t.Fatalf("两趟文本不一致：\n s1 = %s\n s2 = %s", s1, s2)
	}
}

// 嵌套一层也得成立（| 里再套 &）。
func TestString_NestedIsIdempotent(t *testing.T) {
	n := NewDomainNode("|",
		NewDomainNode("&", New("a", "=", 1), New("b", "=", 2)),
		New("c", "=", 3),
	)
	first := n.String()
	if second := n.String(); second != first {
		t.Fatalf("嵌套树两次渲染不同：\n first = %s\n second= %s", first, second)
	}
	back, err := String2Domain(first, nil)
	if err != nil {
		t.Fatalf("嵌套树解析不回来：%v（%s）", err, first)
	}
	if got := back.String(); got != first {
		t.Fatalf("嵌套树往返不一致：\n want = %s\n got  = %s", first, got)
	}
}

// ★ 已知限制（**未修**，此测试钉住现状）：String()→String2Domain 这条往返
// 对三种值形态是有损的，而且损法都是"叶子不再是叶子"，落到 ORM 就是**整条
// domain 被丢掉、返回全表**：
//
//	("f","=",1.5)   →  ["f","="]           浮点值整个消失
//	("n","=",-3)    →  ["n","=","-",3]     负号被词法器切成独立 token
//	("b","=",true)  →  ("b","=","true")    布尔变成字符串（拿字符串比布尔列）
//
// 价格 / 金额 / 余额 / 数量这类条件正好全中。修它要动词法器（负号、小数点），
// 是热路径上的大改，本次不做。
//
// **结论是那条既定规则的真正依据**：改写过的 domain 一律**递节点**给 ORM，
// 中间不经过字符串（orm/statement.go 的 Op() 认 *TDomainNode）。只要不走这趟
// 字符串，上面三种损耗一个都碰不到。
//
// 哪天词法器修好了，这条会变红——那时才该回头放宽调用侧的规矩。
func TestString_RoundTripStillLosesFloatsAndNegatives(t *testing.T) {
	cases := []struct {
		name string
		node *TDomainNode
	}{
		{"浮点", NewDomainNode("&", New("f", "=", 1.5), New("s", "=", "x"))},
		{"负数", NewDomainNode("&", New("n", "=", -3), New("s", "=", "x"))},
	}
	for _, c := range cases {
		back, err := String2Domain(c.node.String(), nil)
		if err != nil {
			t.Fatalf("%s：解析报错了（那是另一个故事）：%v", c.name, err)
		}
		if back.Item(1).IsLeafNode() {
			t.Fatalf("%s：往返已经无损了（%s）——词法器修好了，"+
				"请复查 product 那条 AST 守卫与各处「递节点」注释是否还需要保留",
				c.name, back.String())
		}
	}

	// 反过来：整数和字符串是无损的，所以上面的失败一定是值形态引起的，
	// 不是"往返全都坏"。
	ok := NewDomainNode("&", New("i", "=", 7), New("s", "=", "a,b"))
	back, err := String2Domain(ok.String(), nil)
	if err != nil {
		t.Fatalf("整数/字符串这条也解析不回来：%v", err)
	}
	for i := 1; i <= 2; i++ {
		if !back.Item(i).IsLeafNode() {
			t.Fatalf("整数/字符串本该无损，第 %d 项却不是叶子：%s", i, back.String())
		}
	}
}
