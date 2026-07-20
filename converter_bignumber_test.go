package orm

import "testing"

// TestConverterBigNumberToStringKeepsIntZero 锁死本次修复的核心语义：
// BigNumberToString 打开后，普通 int64 数据列的 0 必须原样输出 "0"，
// 只有关系字段（外键）的 0 才归成空串（= 没有关联）。
//
// 回归背景：此前大数列与真字符列共用 converter(Varchar)，那里有一句无差别的
// `if v == "0" { return "" }`，导致 DB 里 NOT NULL 存着 0 的 sequence/color 等
// 整数列，经 API 读出来是空串——「0」与「未设置」不可区分。
func TestConverterBigNumberToStringKeepsIntZero(t *testing.T) {
	cases := []struct {
		name      string
		blankZero bool
		in        any
		want      any
	}{
		{"普通int列的0原样输出", false, int64(0), "0"},
		{"普通int列的负数", false, int64(-1), "-1"},
		{"普通int列的正数", false, int64(5), "5"},
		{"雪花id不丢精度", false, int64(2079260328012550144), "2079260328012550144"},
		{"外键0归空串", true, int64(0), ""},
		{"外键非0照常输出", true, int64(2079260328012550144), "2079260328012550144"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := converterBigNumberToString(c.blankZero)(c.in)
			if got != c.want {
				t.Errorf("converterBigNumberToString(%v)(%v) = %q, want %q",
					c.blankZero, c.in, got, c.want)
			}
		})
	}
}
