package domain

import "testing"

// TestStringDomain_MultiCharOperators 钉住**字符串条件里的多字符比较符**。
//
// 真机 2026-08-30：退货向导 action_create_returns_all 里的
// `.And("state!=?", "cancel")` 稳定 500，报
// `invalid domain leaf: expected 3 elements, got 0: state` —— 报的是字段名，
// 一个字都没提算子。病灶在 lexer：新版 lexOperator 一次只出一个 rune，
// `!=` 成了 [!][=] 两个词元，那一条就不再是叶子。
//
// 这个测试对**两版 lexer 都要绿**：老版本出完整词元（拼接条件不成立），
// 新版本出单字符词元（走 appendOperatorToken 的拼接）。
func TestStringDomain_MultiCharOperators(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`state!=?`, `("state","!=","?")`},
		{`state != ?`, `("state","!=","?")`},
		{`id<>?`, `("id","!=","?")`}, // <> 归一成 !=，否则 IsLeafNode 认不出
		{`id <> ?`, `("id","!=","?")`},
		{`date<=?`, `("date","<=","?")`},
		{`date>=?`, `("date",">=","?")`},
		{`date<?`, `("date","<","?")`},
		{`date>?`, `("date",">","?")`},
		{`state=?`, `("state","=","?")`}, // `=?` 绝不能被并成一个算子
		{`origin_returned_move_id=? AND state!=?`,
			`["&",("origin_returned_move_id","=","?"),("state","!=","?")]`},
		{`company_id=? and state=? and date<=?`,
			`["&","&",("company_id","=","?"),("state","=","?"),("date","<=","?")]`},
	}

	for _, c := range cases {
		node, err := String2Domain(c.in, nil)
		if err != nil {
			t.Errorf("String2Domain(%q) 报错: %v", c.in, err)
			continue
		}
		if got := node.String(); got != c.want {
			t.Errorf("String2Domain(%q)\n 得到 %s\n 期望 %s", c.in, got, c.want)
		}
	}
}

// TestStringDomain_MultiCharOperators_AreLeaves 单独钉住"是不是叶子"这一步。
// 上一个测试比的是文本形态；真正让 expr.go 报 `invalid domain leaf` 的是
// IsLeafNode() 返回 false，所以这里直接问它。
func TestStringDomain_MultiCharOperators_AreLeaves(t *testing.T) {
	for _, in := range []string{`state!=?`, `id<>?`, `date<=?`, `date>=?`} {
		node, err := String2Domain(in, nil)
		if err != nil {
			t.Errorf("String2Domain(%q) 报错: %v", in, err)
			continue
		}
		if !node.IsLeafNode() {
			t.Errorf("String2Domain(%q) 不是叶子: %s", in, node.String())
		}
	}
}

// TestMultiCharOperators_ExcludesEqualHolder 钉住那张表**不许**收 `=?`。
//
// `?` 是 HOLDER 不是 OPERATOR，所以今天并不会误并；但 `=?` 确实在
// TERM_OPERATORS 里，下一个人很容易"补全"到这张表里。真那样的话
// Where("x=?") ——本项目最常见的写法——会整条散架。
func TestMultiCharOperators_ExcludesEqualHolder(t *testing.T) {
	for _, banned := range []string{"=?", "==", "=<", "=>"} {
		if _, ok := MULTI_CHAR_OPERATORS[banned]; ok {
			t.Errorf("MULTI_CHAR_OPERATORS 不该收 %q", banned)
		}
	}
	// 反向：这四个必须在，且 <> 归一成 !=
	for k, want := range map[string]string{"!=": "!=", "<>": "!=", "<=": "<=", ">=": ">="} {
		if got := MULTI_CHAR_OPERATORS[k]; got != want {
			t.Errorf("MULTI_CHAR_OPERATORS[%q] = %q, 期望 %q", k, got, want)
		}
	}
}
