package orm

import (
	"encoding/json"
	"strings"
	"testing"
)

// Odoo 19 Properties 的五种"关系型/界面型"规格：selection / tags / many2one / many2many / html。
// 知识库的条目属性面板这几种都能加（它的模板数据里就有 selection 与 tags），少一种
// 模块的初始数据就装不进去 —— 2026-09-20 真栈装 knowledge 时撞到的正是
// `the property type "selection" is not supported yet`。

func defs(t *testing.T, s string) []map[string]any {
	t.Helper()
	var out []map[string]any
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPropertiesDefinition_AcceptsOdooTypes(t *testing.T) {
	ok := defs(t, `[
		{"name":"priority","type":"selection","string":"Priority","selection":[["low","Low"],["high","High"]]},
		{"name":"channels","type":"tags","string":"Channels","tags":[["tv","TV",1],["radio","Radio",2]]},
		{"name":"owner","type":"many2one","string":"Owner","comodel":"res.partner"},
		{"name":"team","type":"many2many","string":"Team","comodel":"res.partner"},
		{"name":"notes_html","type":"html","string":"Notes"}
	]`)
	if err := validatePropertiesDefinition(ok); err != nil {
		t.Fatalf("合法定义被拒：%v", err)
	}
}

func TestPropertiesDefinition_RejectsMalformed(t *testing.T) {
	cases := map[string]string{
		"重复的选项":          `[{"name":"p","type":"selection","selection":[["a","A"],["a","B"]]}]`,
		"选项不是二元组":        `[{"name":"p","type":"selection","selection":[["a"]]}]`,
		"标签颜色不是整数":       `[{"name":"t","type":"tags","tags":[["a","A","red"]]}]`,
		"重复的标签":          `[{"name":"t","type":"tags","tags":[["a","A",1],["a","B",2]]}]`,
		"html 名字没有后缀":     `[{"name":"notes","type":"html"}]`,
		"非 html 带 _html":  `[{"name":"x_html","type":"char"}]`,
		"selection 参数用错类型": `[{"name":"c","type":"char","selection":[["a","A"]]}]`,
		"comodel 用在 char 上": `[{"name":"c","type":"char","comodel":"res.partner"}]`,
	}
	for name, src := range cases {
		if err := validatePropertiesDefinition(defs(t, src)); err == nil {
			t.Errorf("%s：应当被拒", name)
		}
	}
}

func TestPropertiesValues_ParsedAgainstDefinition(t *testing.T) {
	list := defs(t, `[
		{"name":"p","type":"selection","selection":[["low","Low"]],"value":"gone"},
		{"name":"q","type":"selection","selection":[["low","Low"]],"value":"low"},
		{"name":"t","type":"tags","tags":[["a","A",1],["b","B",2]],"value":["a","zz","b"]},
		{"name":"m","type":"many2one","comodel":"res.partner","value":"abc"},
		{"name":"mm","type":"many2many","comodel":"res.partner","value":[3,3,"x",4]},
		{"name":"b","type":"boolean","value":0},
		{"name":"c","type":"char","value":12}
	]`)
	parsePropertyValues(list)
	got := map[string]any{}
	for _, it := range list {
		got[it["name"].(string)] = it["value"]
	}
	b, _ := json.Marshal(got)
	s := string(b)
	for _, want := range []string{`"p":false`, `"q":"low"`, `"t":["a","b"]`, `"m":false`, `"mm":[3,4]`, `"b":false`, `"c":false`} {
		if !strings.Contains(s, want) {
			t.Errorf("缺 %s，得到 %s", want, s)
		}
	}
}

func TestPropertiesValues_DisplayNamesStrippedOnWrite(t *testing.T) {
	list := defs(t, `[
		{"name":"m","type":"many2one","comodel":"res.partner","value":[35,"Bob"]},
		{"name":"mm","type":"many2many","comodel":"res.partner","value":[[35,"Bob"],[36,"Alice"]]}
	]`)
	removePropertyDisplayNames(list)
	b, _ := json.Marshal(propertyListToDict(list))
	if s := string(b); s != `{"m":35,"mm":[35,36]}` {
		t.Errorf("剥显示名后 = %s", s)
	}
}
