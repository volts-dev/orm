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
		selection       string
		_attr_selection [][]string
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
	field._attr_type = Bool
	field._attr_store = true
}

// ###########################################################################
// TODO 方法可以是任何大小写 参考https://github.com/alangpierce/go-forceexport
// 所有的selection 函数必须是大写并返回[][]string,
func (self *TSelectionField) Init(ctx *TTagContext) {
	field := self.Base()
	params := ctx.Params

	log.Assert(len(params) < 1, "selection field %s of model %s must including at least 1 args! %v", field.Name(), self.model_name, params)
	field._attr_store = true
	field._attr_type = TYPE_SELECTION
	field._getter = "" //初始化
	lStr := strings.Trim(params[0], "'")
	lStr = strings.Replace(lStr, "''", "'", -1)
	m := ctx.Model.GetBase().modelValue.MethodByName(lStr)
	if m.IsValid() {
		/* TODO 支持 func() [][]int */
		if _, ok := m.Interface().(func() [][]string); !ok {
			log.Fatalf("the selection field %s@%s method %s must func()[][]string type", field.Name(), ctx.Model.String(), lStr)
		}
		field._getter = lStr
	} else {
		m := make(map[string]string)
		err := json.Unmarshal([]byte(lStr), &m)
		if err != nil {
			log.Fatalf("selection tag response error when unmarshal json '%s' : %s", lStr, err.Error())
		}

		for k, v := range m {
			field._attr_selection = append(field._attr_selection, []string{k, v})
		}
	}
}

func (self *TSelectionField) _description_selection() {
	// """ return the selection list (pairs (value, label)); labels are
	/////      translated according to context language
	//  """
	////!	selection = self.selection
	/*     if isinstance(selection, basestring):
	           return getattr(env[self.model_name], selection)()
	       if callable(selection):
	           return selection(env[self.model_name])

	       # translate selection labels
	       if env.lang:
	           name = "%s,%s" % (self.model_name, self.name)
	           translate = partial(
	               env['ir.translation']._get_source, name, 'selection', env.lang)
	           return [(value, translate(label) if label else label) for value, label in selection]
	       else:
	           return selection
	*/
}

func (self *TSelectionField) _setup_regular_base(model IModel) {
	//  super(Selection, self)._setup_regular_base(model)
	//  assert self.selection is not None, "Field %s without selection" % self
}
func (self *TSelectionField) _setup_related_full(model IModel) {
	// super(Selection, self)._setup_related_full(model)
	// # selection must be computed on related field
	//field = self.related_field
	///self.selection = self._description_selection(model.env)
}

// Full field setup: everything else, except recomputation triggers
//
// 配置字段内容
func (self *TSelectionField) setup_full(model IModel) {
	if self._setup_done != "full" {
		/*		if !self.IsRelated() {
				} else {

				}
		*/
		self._setup_done = "full"
	}
}

func (self *TSelectionField) GetAttributes(ctx *TTagContext) map[string]interface{} {
	model := ctx.Model
	model_val := reflect.ValueOf(model) //TODO 使用Webgo对象池

	if lMehodName := self.Compute(); lMehodName != "" {
		if m := model_val.MethodByName(lMehodName); m.IsValid() {
			//results := m.Call([]reflect.Value{model.Base().modelValue}) //
			results := m.Call(nil) //
			if len(results) == 1 {
				if res, ok := results[0].Interface().([][]string); ok {
					self._attr_selection = res
				}
			}
		}
	}

	attrs := self.Base().GetAttributes(ctx)
	attrs["selection"] = self._attr_selection
	return attrs
}

func (self *TSelectionField) OnRead(ctx *TFieldContext) error {
	model := ctx.Model
	field := self

	if mehodName := field._getter; mehodName != "" {
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
					field._attr_selection = res
				}
			}
		}
	}

	return nil
}
