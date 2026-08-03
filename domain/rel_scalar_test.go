package domain

import "testing"

// 域里引用一个 many2one 列时，变量的值是从数据集取的——而经典读之后那一列已经是
// {id,name,display_name} 映射了。映射进 SQL 参数会在驱动层报
// "unsupported type map[string]interface {}"，字段的 OnRead 拿到 error 就提前返回，
// 于是整个关系字段的键从读结果里消失，前后端都不报错。
// 真机 2026-08-04：pro.tmpl.attr.item.value_ids 的 Values 列整列空白。
func TestRelScalar(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want any
	}{
		{"经典读的关系映射取 id", map[string]any{"id": int64(7), "name": "Brand", "display_name": "Brand"}, int64(7)},
		{"雪花 id 以字符串流转时原样取出", map[string]any{"id": "2079551142995431424", "name": "Brand"}, "2079551142995431424"},
		{"name_get 元组取第一位", []any{int64(7), "Brand"}, int64(7)},
		{"裸标量不动", int64(7), int64(7)},
		{"字符串不动", "draft", "draft"},
		{"没有 id 键的映射不动（不是关系值）", map[string]any{"x": 1}, map[string]any{"x": 1}},
		{"nil 不动", nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := relScalar(c.in)
			if m, ok := c.want.(map[string]any); ok {
				gm, ok2 := got.(map[string]any)
				if !ok2 || len(gm) != len(m) {
					t.Fatalf("relScalar(%#v) = %#v, want %#v", c.in, got, c.want)
				}
				return
			}
			if got != c.want {
				t.Fatalf("relScalar(%#v) = %#v, want %#v", c.in, got, c.want)
			}
		})
	}
}

// 'in' 的值本来就是切片，不能被当成 name_get 元组拆掉——拆了就把
// `('id','in',[7,'x'])` 变成 `('id','in',7)`。
func TestRelScalarLeavesOtherSlicesAlone(t *testing.T) {
	for _, in := range []any{
		[]any{int64(1), int64(2)},          // 两个 id：第二位不是字符串，不动
		[]any{int64(1), int64(2), int64(3)}, // 三元素：不动
		[]any{},                             // 空切片：不动
	} {
		got := relScalar(in)
		gs, ok := got.([]any)
		if !ok || len(gs) != len(in.([]any)) {
			t.Fatalf("relScalar(%#v) 不该改动它，得到 %#v", in, got)
		}
	}
}

func TestRelScalarsCopiesInsteadOfMutating(t *testing.T) {
	// 数据集后面还要用这些值，归一必须产出新切片。
	src := []any{map[string]any{"id": int64(7), "name": "Brand"}, int64(8)}
	out := relScalars(src)
	if out[0] != int64(7) || out[1] != int64(8) {
		t.Fatalf("relScalars 结果不对: %#v", out)
	}
	if _, stillMap := src[0].(map[string]any); !stillMap {
		t.Fatal("relScalars 改动了传入的切片")
	}
}
