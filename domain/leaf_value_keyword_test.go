package domain

import "testing"

// 叶子的第三项是**值**，不是逻辑运算：值恰好是 "OR"（美国俄勒冈州的代码）、"and"、"not"、"|"
// 这样的字符串，也必须按字面量落进 leaf。此前列表形式的解析对值也走通用的 parseAny，'OR' 被认成
// 「或」，leaf 只剩两个孩子，执行时报 invalid domain leaf: expected 3 elements —— registry 的演示
// 用户文件因此整份导入失败（search="[('code','=','OR')]"），sale / purchase 的演示订单跟着全跳过。
func TestLeafValueThatLooksLikeALogicalOperator(t *testing.T) {
	for _, v := range []string{"OR", "or", "AND", "not", "|", "&", "!"} {
		t.Run("list/"+v, func(t *testing.T) {
			node, err := Any2Domain([]any{[]any{"code", "=", v}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			leaf := node
			if !leaf.IsLeafNode() && leaf.Count() == 1 {
				leaf = leaf.Item(0)
			}
			if !leaf.IsLeafNode() || leaf.Item(2).String() != v {
				t.Fatalf("值 %q 应原样落在 leaf 第三项，得到 %s", v, Domain2String(node))
			}
		})
		t.Run("string/"+v, func(t *testing.T) {
			node, err := String2Domain("[('code','=','"+v+"')]", nil)
			if err != nil {
				t.Fatal(err)
			}
			leaf := node
			if !leaf.IsLeafNode() && leaf.Count() == 1 {
				leaf = leaf.Item(0)
			}
			if !leaf.IsLeafNode() || leaf.Item(2).String() != v {
				t.Fatalf("值 %q 应原样落在 leaf 第三项，得到 %s", v, Domain2String(node))
			}
		})
	}
}
