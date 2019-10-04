package orm

import (
	"github.com/volts-dev/utils"
)

type (
	TCharField struct {
		TField
	}

	TTextField struct {
		TField
	}
)

func init() {
	RegisterField("char", newCharField)
	RegisterField("text", newTextField)
}

func newCharField() IField {
	return new(TCharField)
}

func newTextField() IField {
	return new(TTextField)
}

// TODO 限制长度
func (self *TCharField) Init(ctx *TFieldContext) {
	fld := ctx.Field
	params := ctx.Params

	if len(params) > 0 {
		size := utils.StrToInt(params[0])
		if size != 0 {
			fld.Base()._attr_size = size
		}
	}

	fld.Base().SqlType = SQLType{Varchar, 0, 0}
	fld.Base()._attr_type = "char"
	fld.Base()._symbol_c = `'%s'`
	fld.Base()._symbol_f = _CharFormat
}

func (self *TTextField) Init(ctx *TFieldContext) {
	fld := ctx.Field
	params := ctx.Params

	if len(params) > 0 {
		size := utils.StrToInt(params[0])
		if size != 0 {
			fld.Base()._attr_size = size
		}
	}

	fld.Base().SqlType = SQLType{Text, 0, 0}
	fld.Base()._attr_type = "text"
	fld.Base()._symbol_c = `'%s'`
	fld.Base()._symbol_f = _CharFormat
}
