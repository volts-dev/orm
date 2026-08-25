package orm

import "testing"

// 驱动交回 []byte 时，各数值类型的解码器要认得它。
//
// 这是 read_group 整数 measure 恒为 0 的**机制判据**（现象判据在
// test/read_group_sum_bigint_pg_test.go，那条要连真库）。
//
// 来由：Postgres 的聚合会抬升类型 —— `SUM(bigint)` 出来是 numeric，`AVG` 一律
// 是 numeric —— 而 numeric 在 Go 里没有对应类型，lib/pq 一概以 []byte 交回。
// 此前 utils.ToInt64 不认这个形态，静默返回 0。
func TestConverter_DriverBytes(t *testing.T) {
	cases := []struct {
		name string
		typ  string
		in   any
		want any
		why  string
	}{
		{"bigint/整数文本", BigInt, []byte("21"), int64(21), "SUM(bigint) 回的就是 numeric"},
		{"bigint/带小数", BigInt, []byte("3.5"), int64(4), "AVG 一律回 numeric；取整好过恒零"},
		{"bigint/原生 int64 不受影响", BigInt, int64(9), int64(9), ""},
		{"int/整数文本", Int, []byte("7"), int(7), ""},
		{"double/小数文本", Double, []byte("2.5"), float64(2.5), ""},
		{"float/小数文本", Float, []byte("2.5"), float32(2.5), ""},
		{"decimal/原样文本", Decimal, []byte("12.34"), "12.34", "别让它以 []byte 进 JSON 编成 base64"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := converter(c.typ)(c.in)
			if got != c.want {
				t.Errorf("converter(%s)(%#v) = %#v（%T），应为 %#v（%T）。%s",
					c.typ, c.in, got, got, c.want, c.want, c.why)
			}
		})
	}
}

// 文本列不受影响：它本来就走 utils.ToString，[]byte 一直是认得的；
// 而 "0" 是合法的字符串值，不能被归成空串（见 converter 里那段说明）。
func TestConverter_DriverBytes_TextUntouched(t *testing.T) {
	if got := converter(Varchar)([]byte("0")); got != "0" {
		t.Errorf("varchar 列的 []byte(\"0\") 读成了 %#v，应为 \"0\"", got)
	}
}
