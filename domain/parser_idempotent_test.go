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

// ★ 【2026-09-09 修，本条已由"钉住损耗"翻成"钉住无损"】
//
// 曾经：String()→String2Domain 这条往返对三种值形态有损，损法都是"叶子不再是
// 叶子"，落到 ORM 就是**整条 domain 被丢掉、返回全表**：
//
//	("f","=",1.5)   →  ["f","="]           浮点值整个消失（解析器 switch 没有 FLOAT 这一档）
//	("n","=",-3)    →  ["n","=","-",3]     负号被词法器切成独立 token
//	("b","=",true)  →  ("b","=","true")    布尔变成字符串
//
// 价格 / 金额 / 余额 / 数量这类条件正好全中。前两种已在 parser.go 修掉
// （appendOperatorToken 里合并符号位、switch 补 FLOAT 一档，见
// parser_negative_number_test.go）；布尔那条量下来其实早就是真 bool 了，
// 上面那行是**陈的**。
//
// **那条既定规则不变**：改写过的 domain 仍然一律**递节点**给 ORM，中间不经过
// 字符串（orm/statement.go 的 Op() 认 *TDomainNode）。依据换了一个 ——
// 不再是"往返有损"，而是"往返这一趟本来就没必要，而且每多一种值形态就多一次
// 出错的机会"。product / account 里那几处 AST 守卫因此照旧保留。
func TestString_RoundTripIsLosslessForScalarValues(t *testing.T) {
	cases := []struct {
		name string
		node *TDomainNode
		want any
	}{
		{"浮点", NewDomainNode("&", New("f", "=", 1.5), New("s", "=", "x")), 1.5},
		{"负数", NewDomainNode("&", New("n", "=", -3), New("s", "=", "x")), int64(-3)},
		{"负小数", NewDomainNode("&", New("f", "=", -1.5), New("s", "=", "x")), -1.5},
		{"布尔", NewDomainNode("&", New("b", "=", true), New("s", "=", "x")), true},
		{"整数", NewDomainNode("&", New("i", "=", 7), New("s", "=", "x")), int64(7)},
		{"带逗号的串", NewDomainNode("&", New("s", "=", "a,b"), New("s", "=", "x")), "a,b"},
	}
	for _, c := range cases {
		back, err := String2Domain(c.node.String(), nil)
		if err != nil {
			t.Errorf("%s：解析报错 %v", c.name, err)
			continue
		}
		leaf := back.Item(1)
		if !leaf.IsLeafNode() {
			t.Errorf("%s：往返之后不再是叶子（%s）—— 落到 ORM 是整条 domain 被丢掉、返回全表",
				c.name, back.String())
			continue
		}
		// ★ 光看长相不够：值的**类型**也要保住，拿字符串去比数值/布尔列
		//   在 SQL 那一侧会被引号括起来。
		if got := leaf.Item(2).Value; got != c.want {
			t.Errorf("%s：值是 %#v，想要 %#v", c.name, got, c.want)
		}
	}
}
