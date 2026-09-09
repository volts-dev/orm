package domain

import "testing"

// 字符串 domain 里的**负数与小数字面量**必须各自仍是一个值。
//
// 坏掉时的形态是最难查的那一种：`String2Domain` **不报错**，安静地产出一个
// 四元素节点 `["id","=","-",5]`；要到 expr.go 取数时才报
// `invalid domain leaf: expected 3 elements, got 0: id` —— 而那句话报的是
// **字段名**，一个字都没提负号。调用方那侧通常只剩一句被脱敏的 WARN。
//
// 2026-09-09 真栈量到：收银台拿本地券的负数 id 去查库，每次结账在属主进程
// 留一条 500。判据必须是**解析出来的形状**，不能只看"有没有报错"。
func TestString2Domain_NegativeNumberStaysOneValue(t *testing.T) {
	cases := []struct {
		domain string
		want   string
	}{
		{"[('id','=',-5)]", `("id","=",-5)`},
		{"[('id','=',5)]", `("id","=",5)`},
		// `>=` 先被拼回一个算子，剩下的 `-` 才轮到这里合进数字。
		{"[('points','>=',-1)]", `("points",">=",-1)`},
		{"[('points','!=',-1)]", `("points","!=",-1)`},
		// 列表里每一个都要各自合并。
		{"[('id','in',[-1,-2])]", `("id","in",[-1,-2])`},
		// 前缀 OR 加负数：礼品卡那条 domain 的真实形状。
		{"['|',('code','=','abc'),('id','=',-5)]",
			`["|",("code","=","abc"),("id","=",-5)]`},
		// ★ 隔着空格的不动：那不是一个负数字面量。
		{"[('id','=', - 5)]", `["id","=","-",5]`},
		// ★ 引号里的减号是字符串的一部分，与本条无关。
		{"[('name','=','a-b')]", `("name","=","a-b")`},

		// —— 小数是另一种坏法：词法器出的是一个完整的 FLOAT 词元，而解析器
		//    的 switch 此前**没有这一档**，于是值被静默丢掉，解析结果**少**
		//    一个元素（`["f","="]`）。负数是多一个、小数是少一个，两种都不报错。
		{"[('f','=',1.5)]", `("f","=",1.5)`},
		{"[('f','>',0.5)]", `("f",">",0.5)`},
		// 负号 + 小数：两处修复必须都在，缺一个这条就坏。
		{"[('f','=',-1.5)]", `("f","=",-1.5)`},
		{"[('f','in',[1.5,-2.5])]", `("f","in",[1.5,-2.5])`},
	}
	for _, c := range cases {
		node, err := String2Domain(c.domain, nil)
		if err != nil {
			t.Errorf("%s -> 解析报错 %v", c.domain, err)
			continue
		}
		if got := node.String(); got != c.want {
			t.Errorf("%s\n  得到 %s\n  想要 %s", c.domain, got, c.want)
		}
	}
}

// 叶子必须**真的是叶子** —— 上面那条比的是字符串形态，这条比的是节点性质，
// 因为 expr.go 判的是 IsLeafNode() 而不是长相。
func TestString2Domain_NegativeNumberLeafIsALeaf(t *testing.T) {
	node, err := String2Domain("[('id','=',-5)]", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !node.IsLeafNode() {
		t.Fatalf("('id','=',-5) 必须是叶子，实得 %s（%d 个元素）", node.String(), node.Count())
	}
	if node.Count() != 3 {
		t.Fatalf("叶子必须是三元组，实得 %d 个元素：%s", node.Count(), node.String())
	}

	// 小数那一侧是**少**一个元素，同样过不了 IsLeafNode()。
	node, err = String2Domain("[('amount','>',0.5)]", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !node.IsLeafNode() || node.Count() != 3 {
		t.Fatalf("('amount','>',0.5) 必须是三元叶子，实得 %s（%d 个元素）——"+
			"值被丢掉时这条恒回 2 个元素", node.String(), node.Count())
	}
	if got := node.Item(2).Value; got != 0.5 {
		t.Fatalf("小数值必须是 float64 0.5，实得 %#v —— 落成字符串会被引号括起来", got)
	}
}
