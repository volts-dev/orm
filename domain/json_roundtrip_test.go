package domain

import (
	"encoding/json"
	"reflect"
	"testing"
)

/*
TDomainNode 经 JSON 往返必须保真。

默认编码只输出导出的 Value 字段，nodeType/children 全丢，一棵条件树过一次 JSON 只剩
`{"Value":null}`。vectors 跨进程读记录因此只敢传字符串 domain（core/tenant/remote_records.go：
"domain.New 造出的 TDomainNode 经 JSON 往返不保真"）。现在以 Odoo 列表形态编码，
与 Any2Domain 的输入形状一致。
*/

func roundTrip(t *testing.T, n *TDomainNode) *TDomainNode {
	t.Helper()
	b, err := json.Marshal(n)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back TDomainNode
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal %s: %v", b, err)
	}
	return &back
}

func TestJSON_LeafRoundTrip(t *testing.T) {
	n := New("name", "=", "abc")
	b, _ := json.Marshal(n)
	if string(b) != `["name","=","abc"]` {
		t.Fatalf("叶子应编成列表形态，得到 %s", b)
	}
	back := roundTrip(t, n)
	if !back.IsLeafNode() || back.String(0) != "name" || back.String(1) != "=" || back.String(2) != "abc" {
		t.Fatalf("往返后叶子变形：%s", back.String())
	}
}

func TestJSON_TreeRoundTrip(t *testing.T) {
	n := New("a", "=", 1).OR(New("b", "in", 1, 2, 3))
	before := Domain2String(n)
	back := roundTrip(t, n)
	if got := Domain2String(back); got != before {
		t.Fatalf("往返后树变形：\n before=%s\n after =%s", before, got)
	}
}

// 19 位雪花 id 不能被 float64 削掉末几位。
func TestJSON_BigIntPreserved(t *testing.T) {
	const big int64 = 1234567890123456789
	back := roundTrip(t, New("id", "in", big, big+1))
	vals := back.Item(2)
	if vals.Count() != 2 {
		t.Fatalf("值列表应为 2 项：%s", back.String())
	}
	got := []any{vals.Item(0).Value, vals.Item(1).Value}
	want := []any{big, big + 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("大整数往返失真：得到 %#v 想要 %#v", got, want)
	}
}

// 空集合 `in []` 往返后仍是"空"，不能变成别的东西。
func TestJSON_EmptyListRoundTrip(t *testing.T) {
	n := NewDomainNode().IN("id")
	back := roundTrip(t, n)
	if !back.IsLeafNode() || back.Count() != 3 {
		t.Fatalf("空 IN 叶子往返后不再是叶子：%s", back.String())
	}
	right := back.Item(2)
	if right.Count() != 0 || right.Value != nil {
		t.Fatalf("空集合往返后不再为空：%#v", right)
	}
}

func TestJSON_EmptyNodeRoundTrip(t *testing.T) {
	back := roundTrip(t, NewDomainNode())
	if !back.IsEmpty() {
		t.Fatalf("空节点往返后应仍为空")
	}
}

// 接收方按 []any 走 Any2Domain 也能吃下同一份 JSON（跨进程两端不必同时升级）。
func TestJSON_CompatibleWithAny2Domain(t *testing.T) {
	n := New("a", "=", 1).AND(New("b", "!=", "x"))
	b, _ := json.Marshal(n)
	var raw []any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	back, err := Any2Domain(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if Domain2String(back) != Domain2String(n) {
		t.Fatalf("Any2Domain 读回的树与原树不同：%s vs %s", Domain2String(back), Domain2String(n))
	}
}
