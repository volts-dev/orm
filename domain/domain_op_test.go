package domain

import "testing"

// 回归: self 是一个没有逻辑操作符前缀的隐式 AND 列表时, 再 OP 不应 panic。
//
// 复现线上崩溃: model_request.Read 先 session.Domain([["id","in",[...]]])
// 把请求 domain 装入 statement.domain(解析成 [leaf] 这种被列表包了一层的结构),
// 紧接着 OpRead 注入 session.Where("tenant_id=?") 再次 OP, 旧实现在
// "node is not a leaf node and not a domain operator" 处 Panicf。
func TestOP_ImplicitAndList_SingleLeaf(t *testing.T) {
	// [["id","in",[1,2,3]]] -> 含 1 个 leaf 子节点的 LIST_NODE
	self, err := Any2Domain([]any{[]any{"id", "in", []any{1, 2, 3}}}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// 注入 tenant 过滤 —— 旧实现在此 panic
	self.OP(AND_OPERATOR, New("tenant_id", "=", 1))

	// 结果应为归一化的 & 二元结构: [&, (id,in,[1,2,3]), (tenant_id,=,1)]
	if got := self.Count(); got != 3 {
		t.Fatalf("count = %d, want 3 ([&, leaf, leaf])", got)
	}
	if op := self.Item(0).String(); op != AND_OPERATOR {
		t.Errorf("operator = %q, want %q", op, AND_OPERATOR)
	}
	if f := self.Item(1).Item(0).String(); f != "id" {
		t.Errorf("first leaf field = %q, want \"id\"", f)
	}
	if f := self.Item(2).Item(0).String(); f != "tenant_id" {
		t.Errorf("second leaf field = %q, want \"tenant_id\"", f)
	}
}

// 多条件隐式 AND: [["a","=",1],["b","=",2]] 再 AND 一个条件,
// 应展开成 [&, &, (a,=,1), (b,=,2), (c,=,3)] (n 个条件需 n-1 个 '&')。
func TestOP_ImplicitAndList_MultiLeaf(t *testing.T) {
	self, err := Any2Domain([]any{[]any{"a", "=", 1}, []any{"b", "=", 2}}, nil)
	if err != nil {
		t.Fatal(err)
	}

	self.OP(AND_OPERATOR, New("c", "=", 3))

	if got := self.Count(); got != 5 {
		t.Fatalf("count = %d, want 5 ([&,&,leaf,leaf,leaf])", got)
	}
	if op := self.Item(0).String(); op != AND_OPERATOR {
		t.Errorf("op[0] = %q, want %q", op, AND_OPERATOR)
	}
	if op := self.Item(1).String(); op != AND_OPERATOR {
		t.Errorf("op[1] = %q, want %q", op, AND_OPERATOR)
	}
}

// 隐式 AND 列表与 OR 组合时, 内层 AND 分组必须显式保留:
// [["id","in",[1,2,3]]] OR (x=9) -> [|, (id,in,[1,2,3]), (x,=,9)]。
func TestOP_ImplicitAndList_Or(t *testing.T) {
	self, err := Any2Domain([]any{[]any{"id", "in", []any{1, 2, 3}}}, nil)
	if err != nil {
		t.Fatal(err)
	}

	self.OP(OR_OPERATOR, New("x", "=", 9))

	if got := self.Count(); got != 3 {
		t.Fatalf("count = %d, want 3", got)
	}
	if op := self.Item(0).String(); op != OR_OPERATOR {
		t.Errorf("operator = %q, want %q", op, OR_OPERATOR)
	}
}

// 回归（2026-09-22 真栈）：vectors 列表搜索在显示名叶子上 OR 进候选 id，
// [A, B] 变成 [A, '|', B, C] —— 4 个元素、两项。再 AND 租户条件时旧实现按元素个数
// 补了 3 个 '&'，得到 ['&','&','&','&',A,'|',B,C,T]，normalize_domain 报
// arity mismatch，按名字搜 sys.attachment 直接 500。应当只补 1 个。
func TestOP_ImplicitAndList_WithNestedOr(t *testing.T) {
	self, err := Any2Domain([]any{
		[]any{"res_model", "=", "knowledge.cover"},
		"|", []any{"name", "ilike", "probe"}, []any{"id", "in", []any{"1", "2"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	self.OP(AND_OPERATOR, New("tenant_id", "=", "?"))

	want := `[('&'),('&'),('res_model','=','knowledge.cover'),('|'),('name','ilike','probe'),('id','in',['1','2']),('tenant_id','=','?')]`
	got := Domain2String(self)
	if missing := self.missingAnds(); missing != 0 {
		t.Fatalf("结果仍不是一项（还缺 %d 个 '&'）：%s", missing, got)
	}
	if n := countOps(self, AND_OPERATOR); n != 2 {
		t.Fatalf("应有 2 个 '&'，实际 %d 个：%s\nwant 形如 %s", n, got, want)
	}
}

// 以运算符开头、后面仍有隐式 AND：['|', A, B, C] = (A|B) & C。
// 再 AND D 应为 ['&', '&', '|', A, B, C, D]，而不是把 D 和 C 错位成一组。
func TestOP_OperatorPrefixedImplicitAnd(t *testing.T) {
	self, err := Any2Domain([]any{
		"|", []any{"a", "=", 1}, []any{"b", "=", 2}, []any{"c", "=", 3},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	self.OP(AND_OPERATOR, New("d", "=", 4))
	if missing := self.missingAnds(); missing != 0 {
		t.Fatalf("结果仍不是一项（还缺 %d 个 '&'）：%s", missing, Domain2String(self))
	}
	if n := countOps(self, AND_OPERATOR); n != 2 {
		t.Fatalf("应有 2 个 '&'，实际 %d 个：%s", n, Domain2String(self))
	}
	if self.Item(0).String() != AND_OPERATOR || self.Item(1).String() != AND_OPERATOR || self.Item(2).String() != OR_OPERATOR {
		t.Fatalf("前缀应为 & & |：%s", Domain2String(self))
	}
}

func countOps(n *TDomainNode, op string) int {
	c := 0
	for _, ch := range n.Nodes() {
		if ch.IsValueNode() && ch.String() == op {
			c++
		}
	}
	return c
}
