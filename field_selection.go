package orm

import (
	"encoding/json"
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
			m := make(map[string]string)
			err := json.Unmarshal([]byte(lStr), &m)
			if err != nil {
				log.Fatalf("selection tag response error when unmarshal json '%s' : %s", lStr, err.Error())
			}

			for k, v := range m {
				self.selection = append(self.selection, []string{k, v})
			}
		}
	}
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
