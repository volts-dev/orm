package domain

import (
	"strings"
	"testing"
)

// 字符串 domain 里带引号/反斜杠的值必须原样解析回来。
//
// 坏掉时的形态：String2Domain **不报错**，值变成空串。`ilike ''` 匹配全表，
// `=` 什么都查不到 —— 用户搜 "O'Brien" 拿到全部联系人。判据必须是解析出来的
// **值**，不能只看有没有报错（2026-09-17 实测，见 Unquote 的注释）。
func TestString2Domain_EscapedStringValueRoundTrips(t *testing.T) {
	// quote 是调用方该用的转义：先反斜杠、再单引号。
	quote := func(v string) string {
		v = strings.ReplaceAll(v, `\`, `\\`)
		return "'" + strings.ReplaceAll(v, `'`, `\'`) + "'"
	}
	vals := []string{
		`abc`,
		`O'Brien`,
		`ends\`,
		`a\'b`,
		`x\y`,
		`say "hi"`,
		`中文'混排`,
		`'`,
		``,
	}
	for _, v := range vals {
		// 两个条件：一个值出错时后面那条的结构也要保住。
		src := "[('name','='," + quote(v) + "),('active','=',True)]"
		node, err := String2Domain(src, nil)
		if err != nil {
			t.Errorf("%q: String2Domain(%s) err = %v", v, src, err)
			continue
		}
		if node.Count() != 2 {
			t.Errorf("%q: 解析出 %d 个条件，want 2：%s", v, node.Count(), node.String())
			continue
		}
		got := node.Item(0).Item(2).Value
		if got != v {
			t.Errorf("%q: 值解析成 %#v，src=%s", v, got, src)
		}
	}
}

func TestUnquote(t *testing.T) {
	cases := map[string]string{
		`plain`:      `plain`,
		`O\'Brien`:   `O'Brien`,
		`a\\b`:       `a\b`,
		`say \"hi\"`: `say "hi"`,
		`x\y`:        `x\y`, // 未知转义原样保留（Python 语义）
		`tab\tend`:   "tab\tend",
		`中`:     "中",
		`\u4e`:       `\u4e`, // 不完整的 \u 不吞字符
		`trail\`:     `trail\`,
		`a"b`:        `a"b`, // 旧实现在这里返回空串
	}
	for in, want := range cases {
		if got := Unquote(in); got != want {
			t.Errorf("Unquote(%q) = %q, want %q", in, got, want)
		}
	}
}
