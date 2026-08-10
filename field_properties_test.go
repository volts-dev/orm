package orm

import (
	"encoding/json"
	"testing"
)

// 定义 ∪ 值 → 完整列表：定义里没有的键必须被丢掉。
// 容器上删掉一个规格项后，明细列里那个孤儿值还在（本仓不做存量清洗，代价太大），
// 若这里不丢，前端会收到一个没有 string/type 的项，控件渲染成空白行。
func TestDictToPropertyList_DropsValuesMissingFromDefinition(t *testing.T) {
	definition := []map[string]any{
		{"name": "aa", "string": "材质", "type": "char", "default": "棉"},
		{"name": "bb", "string": "防水", "type": "boolean"},
	}
	values := map[string]any{
		"aa": "丝",
		"zz": "孤儿值", // 定义里已经没有它了
	}

	got := dictToPropertyList(values, definition)
	if len(got) != 2 {
		t.Fatalf("want 2 items, got %d: %v", len(got), got)
	}
	if got[0]["value"] != "丝" {
		t.Errorf("aa.value = %v, want 丝", got[0]["value"])
	}
	if _, has := got[1]["value"]; has {
		t.Errorf("bb 没填过值，不该带 value 键: %v", got[1])
	}
	for _, item := range got {
		if item["name"] == "zz" {
			t.Errorf("定义里没有的值不该出现在读出结果里: %v", got)
		}
	}
}

// 合并不能改到定义本身：定义是从容器读来的共享数据，一次读多条产品时，
// 给第一条产品填的 value 若写进了那份 map，后面所有产品都会带上它。
func TestDictToPropertyList_DoesNotMutateDefinition(t *testing.T) {
	definition := []map[string]any{
		{"name": "aa", "string": "材质", "type": "char"},
	}

	_ = dictToPropertyList(map[string]any{"aa": "丝"}, definition)

	if v, has := definition[0]["value"]; has {
		t.Fatalf("定义被就地改写了，混入了 value=%v", v)
	}
}

// 数值的 0 是合法值，不能被当成"空"丢掉；非数值的空值统一落 false。
func TestPropertyListToDict_ZeroIsAValue(t *testing.T) {
	list := []map[string]any{
		{"name": "n1", "type": "integer", "value": 0},
		{"name": "n2", "type": "float", "value": 0.0},
		{"name": "n3", "type": "monetary", "value": 0},
		{"name": "c1", "type": "char", "value": ""},
		{"name": "b1", "type": "boolean", "value": false},
		{"name": "s1", "type": "separator", "value": "标题"}, // 分隔符没有值
		{"name": "x1", "type": "char"},                     // 没填过
	}

	got := propertyListToDict(list)

	for _, name := range []string{"n1", "n2", "n3"} {
		v, has := got[name]
		if !has {
			t.Errorf("%s: 数值 0 被丢了", name)
			continue
		}
		if v != 0 && v != 0.0 {
			t.Errorf("%s = %v, want 0", name, v)
		}
	}
	if got["c1"] != false {
		t.Errorf("c1 = %v, want false（空字符串归一成 false）", got["c1"])
	}
	if got["b1"] != false {
		t.Errorf("b1 = %v, want false", got["b1"])
	}
	if _, has := got["s1"]; has {
		t.Errorf("separator 不该有值: %v", got["s1"])
	}
	if _, has := got["x1"]; has {
		t.Errorf("没填过的项不该进库（读时会从定义补 default）: %v", got["x1"])
	}
}

// 新增项由服务端生成 name。前端造 id 的话，两个人同时加规格项会撞名，
// 撞上的表现是两项的值互相覆盖。
func TestAddMissingPropertyNames(t *testing.T) {
	list := []map[string]any{
		{"string": "新项", "type": "char"},
		{"name": "已有", "string": "旧项", "type": "char"},
	}

	addMissingPropertyNames(list)

	name := list[0]["name"].(string)
	if len(name) != 16 {
		t.Errorf("生成的 name 长度 = %d, want 16: %q", len(name), name)
	}
	if list[1]["name"] != "已有" {
		t.Errorf("已有 name 被改写成了 %v", list[1]["name"])
	}
}

func TestValidatePropertiesDefinition(t *testing.T) {
	cases := []struct {
		name    string
		list    []map[string]any
		wantErr bool
	}{
		{"正常", []map[string]any{{"name": "a", "type": "char"}}, false},
		{"缺 name", []map[string]any{{"type": "char"}}, true},
		{"缺 type", []map[string]any{{"name": "a"}}, true},
		{"重名", []map[string]any{{"name": "a", "type": "char"}, {"name": "a", "type": "text"}}, true},
		{"未支持的类型", []map[string]any{{"name": "a", "type": "many2one"}}, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validatePropertiesDefinition(c.list)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, c.wantErr)
			}
		})
	}
}

// 已是 JSON 文本的值不能再 Marshal 一次：那样存进 jsonb 列的是个被转义的字符串
// 字面量 "{\"a\":1}"，读出来永远解不成对象。
func TestEncodeJsonValue_DoesNotDoubleEncode(t *testing.T) {
	raw := `{"aa":"丝"}`

	got := encodeJsonValue(raw)

	if got != raw {
		t.Fatalf("encodeJsonValue(%q) = %v, 被重复编码了", raw, got)
	}

	var out map[string]any
	if err := json.Unmarshal([]byte(got.(string)), &out); err != nil {
		t.Fatalf("结果不是合法 JSON 对象: %s", err)
	}
	if out["aa"] != "丝" {
		t.Errorf("解出来的值不对: %v", out)
	}
}

// 空串在 jsonb 列上是非法输入（不是 JSON 文档），必须落 NULL——
// 否则 INSERT 直接报 invalid input syntax for type json。
func TestEncodeJsonValue_EmptyBecomesNull(t *testing.T) {
	for _, v := range []any{"", []byte(nil), []byte{}, nil} {
		if got := encodeJsonValue(v); got != nil {
			t.Errorf("encodeJsonValue(%#v) = %v, want nil", v, got)
		}
	}
}

func TestDecodeJsonValue(t *testing.T) {
	got := decodeJsonValue([]byte(`[{"name":"aa","type":"char"}]`))
	list, err := toPropertyList(got)
	if err != nil {
		t.Fatalf("toPropertyList: %s", err)
	}
	if len(list) != 1 || list[0]["name"] != "aa" {
		t.Fatalf("解出来的定义不对: %v", list)
	}

	// 解不动就原样返回，不能让一个坏值把整行读挂掉。
	bad := decodeJsonValue([]byte("{not json"))
	if _, ok := bad.([]byte); !ok {
		t.Errorf("坏值应原样返回，got %T", bad)
	}
}

func TestM2ORawId(t *testing.T) {
	cases := []struct {
		in   any
		want any
	}{
		{int64(7), int64(7)},
		{[]any{int64(7), "Office"}, int64(7)}, // 前端提交的经典元组
		{map[string]any{"id": int64(7), "display_name": "Office"}, int64(7)},
		{nil, nil},
	}
	for _, c := range cases {
		if got := m2oRawId(c.in); got != c.want {
			t.Errorf("m2oRawId(%#v) = %#v, want %#v", c.in, got, c.want)
		}
	}
}
