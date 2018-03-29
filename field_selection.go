package orm

import (
	"encoding/json"
	"reflect"
	//"reflect"
	"strings"
	"vectors/logger"

	"github.com/go-xorm/core"
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
	RegisterField("bool", NewBooleanField)
	RegisterField("selection", NewSelectionField)
}

func NewBooleanField() IField {
	return new(TBooleanField)
}

func NewSelectionField() IField {
	return new(TSelectionField)
}

func (self *TBooleanField) Init(ctx *TFieldContext) {
	col := ctx.Column
	fld := ctx.Field

	col.SQLType = core.SQLType{core.Bool, 0, 0}
	fld.Base()._attr_type = FIELD_TYPE_BOOL
	fld.Base()._column_type = core.Bool
}

//###########################################################################
// TODO 方法可以是任何大小写 参考https://github.com/alangpierce/go-forceexport
// 所有的selection 函数必须是大写并返回[][]string,
func (self *TSelectionField) Init(ctx *TFieldContext) {
	fld := self
	params := ctx.Params
	//fields.Selection([('linear', 'Linear'), ('degressive', 'Degressive')]), string='Computation Method'
	//fields.Selection(['linear', 'Linear','degressive', 'Degressive']), string='Computation Method'
	//fld.Base()._attr_type = "selection"
	//fld.Base()._column_type = "selection"
	//lField.initSelection(lTag[1:]...)
	logger.Assert(len(params) > 1, "Selection(%s) of model %s must including at least 1 args!", fld.Name(), self.model_name)

	fld.Base()._compute = "" //初始化
	//logger.Dbg(arg)
	lStr := strings.Trim(params[1], "'")
	lStr = strings.Replace(lStr, "''", "'", -1)
	//logger.Dbg("tag_selection", params, lStr, ctx.Model.ModelName(), ctx.Model.Base()._cls_value, ctx.Model.Base()._cls_value.MethodByName(lStr))
	//if m := model.MethodByName(lStr); m != nil {
	if m := ctx.Model.GetBase()._cls_value.MethodByName(lStr); m.IsValid() {
		fld.Base()._compute = lStr
	} else {
		m := make(map[string]string)
		err := json.Unmarshal([]byte(lStr), &m)
		if err != nil {
			logger.Logger.Err("selection tag response error when unmarshal json '%s' : %s", lStr, err.Error())
		}

		for k, v := range m {
			fld._attr_selection = append(fld._attr_selection, []string{k, v})
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

//
// Full field setup: everything else, except recomputation triggers
//
// 配置字段内容
func (self *TSelectionField) setup_full(model IModel) {
	if self._setup_done != "full" {
		if !self.IsRelated() {
		} else {

		}
		self._setup_done = "full"
	}
}

func (self *TSelectionField) GetAttributes(ctx *TFieldContext) map[string]interface{} {
	model := ctx.Model
	model_val := reflect.ValueOf(model) //TODO 使用Webgo对象池

	if lMehodName := self.Compute(); lMehodName != "" {
		//logger.Dbg("selection:", lMehodName, self.model._cls_value.MethodByName(lMehodName))
		if m := model_val.MethodByName(lMehodName); m.IsValid() {
			//logger.Dbg("selection:", m, self.model._cls_value)
			//results := m.Call([]reflect.Value{model.Base()._cls_value}) //
			results := m.Call(nil) //
			//logger.Dbg("selection:", results)
			if len(results) == 1 {
				//fld.Selection, _ = results[0].Interface().([][]string)
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

func (self *TSelectionField) OnConvertToRead(ctx *TFieldEventContext) interface{} {
	/*	model := ctx.Model
		field := self

		if lMehodName := field.Func(); lMehodName != "" {
			//logger.Dbg("selection:", lMehodName, self.model._cls_value.MethodByName(lMehodName))
			if m := model._cls_value.MethodByName(lMehodName); m.IsValid() {
				//logger.Dbg("selection:", m, self.model._cls_value)
				results := m.Call([]reflect.Value{self.model._cls_value}) //
				//logger.Dbg("selection:", results)
				if len(results) == 1 {
					//fld.Selection, _ = results[0].Interface().([][]string)
					if res, ok := results[0].Interface().([][]string); ok {
						field._attr_selection = res
					}
				}
			}
		}
	*/
	return ctx.Value
}
