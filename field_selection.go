package orm

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
)

type (
	TBooleanField struct {
		TField
	}

	TSelectionField struct {
		TField
	}
)

func init() {
	RegisterField("bool", newBooleanField)
	RegisterField("selection", newSelectionField)
}

func newBooleanField() IField {
	return new(TBooleanField)
}

func newSelectionField() IField {
	return new(TSelectionField)
}

func (self *TBooleanField) Init(ctx *TTagContext) {
	field := ctx.Field.Base()
	field.SqlType = SQLType{Bool, 0, 0}
	field.typeName = Bool
	field.store = true
}

// ###########################################################################
// TODO 方法可以是任何大小写 参考https://github.com/alangpierce/go-forceexport
// 所有的selection 函数必须是大写并返回[][]string,
func (self *TSelectionField) Init(ctx *TTagContext) {
	field := self.Base()
	params := ctx.Params

	field.store = true
	field.typeName = TYPE_SELECTION
	field.getterMethod = "" //初始化
	field.SqlType = SQLType{Varchar, 0, 0}

	if self.selection == nil {
		log.Assert(len(params) < 1, "selection field %s of model %s must including at least 1 args! %v", field.Name(), self.modelName, params)
		lStr := strings.Trim(params[0], "'")
		lStr = strings.Replace(lStr, "''", "'", -1)
		m := ctx.Model.GetBase().modelValue.MethodByName(lStr)
		if m.IsValid() {
			/* TODO 支持 func() [][]int */
			if _, ok := m.Interface().(func() [][]string); !ok {
				log.Fatalf("the selection field %s@%s method %s must func()[][]string type", field.Name(), ctx.Model.String(), lStr)
			}
			field.getterMethod = lStr
		} else {
			sel, err := parseSelectionJSON(lStr)
			if err != nil {
				log.Fatalf("selection tag response error when unmarshal json '%s' : %s", lStr, err.Error())
			}
			self.selection = sel
		}
	}
}

// parseSelectionJSON 解析 `selection('{"draft":"Draft","sent":"Sent"}')` 的对象写法，
// **保留 JSON 文档里的书写顺序**。
//
// 原实现是 `json.Unmarshal` 进 `map[string]string` 再 `for k, v := range m`——Go 的
// map 迭代顺序是**故意随机化**的，于是每次进程启动，同一个字段的选项顺序都不一样。
// 后果不止是下拉框选项乱序：statusbar 控件直接按 selection 顺序画流水线，销售单的
// 状态栏会渲成「Sales Order → Quotation → Quotation Sent」这种前后颠倒的样子，
// 而且刷新一次就换个顺序。全仓 126 个 selection 字段用的都是这种写法。
//
// 用 Decoder 走 token 流而不是 Unmarshal：encoding/json 解到 map 就把顺序丢了，
// 没有任何补救办法——顺序信息只存在于原始文档里。
func parseSelectionJSON(s string) ([][]string, error) {
	dec := json.NewDecoder(strings.NewReader(s))

	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("selection must be a json object, got %v", tok)
	}

	var out [][]string
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("selection key must be a string, got %v", keyTok)
		}
		var label string
		if err := dec.Decode(&label); err != nil {
			return nil, err
		}
		out = append(out, []string{key, label})
	}

	// 收尾的 '}'。
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	// 对象之后不能再有东西。原来的 json.Unmarshal 会拒绝尾部垃圾，Decoder 不会——
	// 少了这一步，`{"a":"b"} 手滑打的字` 会被当成合法输入静默接受。
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("selection has trailing content after the json object")
	}
	return out, nil
}

func (self *TSelectionField) Attributes(ctx *TTagContext) map[string]any {
	model := ctx.Model
	model_val := reflect.ValueOf(model) //TODO 使用Webgo对象池

	if lMehodName := self.Getter(); lMehodName != "" {
		if m := model_val.MethodByName(lMehodName); m.IsValid() {
			//results := m.Call([]reflect.Value{model.Base().modelValue}) //
			results := m.Call(nil) //
			if len(results) == 1 {
				if res, ok := results[0].Interface().([][]string); ok {
					self.selection = res
				}
			}
		}
	}

	attrs := self.Base().Attributes(ctx)
	attrs["selection"] = self.selection
	return attrs
}

func (self *TSelectionField) OnRead(ctx *TFieldContext) error {
	model := ctx.Model
	field := self

	if mehodName := field.getterMethod; mehodName != "" {
		// TODO 同一记录方法到OBJECT里使用Method
		if method := model.GetBase().modelValue.MethodByName(mehodName); method.IsValid() {
			var results []reflect.Value
			if method.Type().NumIn() == 1 {
				args := make([]reflect.Value, 0)
				args = append(args, reflect.ValueOf(ctx))
				results = method.Call(args) //

			} else {
				results = method.Call(nil) //

			}

			if len(results) == 1 {
				if res, ok := results[0].Interface().([][]string); ok {
					field.selection = res
				}
			}
		}
	}

	return nil
}
