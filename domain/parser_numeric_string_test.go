package domain

import "testing"

// 看起来像数、但转成数就写不回原样的字符串必须仍是字符串（见 intLiteral）。
func TestString2Domain_NumericLookingStringKeepsItsText(t *testing.T) {
	cases := []struct {
		domain string
		want   any
	}{
		{"[('mobile','=','+32456001122')]", "+32456001122"},
		{"[('code','=','007')]", "007"},
		{"[('code','in',['+1','0042'])]", nil}, // 列表另验
		// 写得回原串的照旧转成整数（雪花 id、普通数字）。
		{"[('id','=','2103849853409824768')]", int64(2103849853409824768)},
		{"[('id','=',5)]", int64(5)},
	}
	for _, c := range cases {
		node, err := String2Domain(c.domain, nil)
		if err != nil {
			t.Fatalf("%s: %v", c.domain, err)
		}
		leaf := node
		if leaf.Count() == 1 && leaf.Item(0).IsListNode() {
			leaf = leaf.Item(0)
		}
		if c.want == nil {
			vals := leaf.Item(2)
			got := []any{}
			for i := 0; i < vals.Count(); i++ {
				got = append(got, vals.Item(i).Value)
			}
			if len(got) != 2 || got[0] != "+1" || got[1] != "0042" {
				t.Errorf("%s: 列表值 %#v，应为 [\"+1\" \"0042\"]", c.domain, got)
			}
			continue
		}
		if got := leaf.Item(2).Value; got != c.want {
			t.Errorf("%s: 值 %#v (%T)，应为 %#v (%T)", c.domain, got, got, c.want, c.want)
		}
	}
}
