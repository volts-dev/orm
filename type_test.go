package orm

import "testing"

// 字符类型的转换器曾把任何值为 "0" 的列一律归成空串。该归一化只对「数值来源」成立
// （BigNumberToString 打开时 int64 列走字符类型格式化，空外键的 0 必须变空串，否则
// 前端会把字符串 "0" 当成有效 id）；真字符串列存 "0" 是合法值，必须原样读回，
// 否则 selection 的 "0"/"1"、设置项默认值、编码等一律被读成空。
func TestVarcharConverterKeepsLiteralZeroString(t *testing.T) {
	conv := converter(Varchar)

	cases := []struct {
		name string
		in   any
		want any
	}{
		{"字符串 0 原样保留", "0", "0"},
		{"[]byte 0 原样保留", []byte("0"), "0"},
		{"普通字符串", "kg", "kg"},
		{"空字符串", "", ""},
		{"数值 0（空外键经 BigNumberToString）归空串", int64(0), ""},
		{"数值非 0 仍转字符串", int64(42), "42"},
		{"nil 归空串", nil, ""},
	}
	for _, c := range cases {
		if got := conv(c.in); got != c.want {
			t.Errorf("%s: converter(Varchar)(%#v) = %#v, want %#v", c.name, c.in, got, c.want)
		}
	}
}

func TestTextConverterKeepsLiteralZeroString(t *testing.T) {
	if got := converter(Text)("0"); got != "0" {
		t.Errorf(`converter(Text)("0") = %#v, want "0"`, got)
	}
}
