package orm

import (
	"github.com/volts-dev/utils"
)

type (
	TCharField struct {
		TField
	}

	TVarcharField struct {
		TField
	}

	TTextField struct {
		TField
	}
)

func init() {
	RegisterField("char", newCharField)
	RegisterField("varchar", newVarcharField)
	RegisterField("text", newTextField)
}

func newCharField() IField {
	return new(TCharField)
}

// TODO 限制长度
func (self *TCharField) Init(ctx *TTagContext) {
	field := ctx.Field.Base()
	params := ctx.Params

	if len(params) > 0 {
		size := utils.ToInt(params[0])
		if size != 0 {
			field._attr_size = size
			field.SqlType.DefaultLength = size
		}
		field.SqlType = SQLType{Char, size, 0}
	} else {
		field.SqlType = SQLType{Char, 0, 0}
	}
	field._attr_type = Char
	field._attr_store = true
	field._symbol_c = `'%s'`
	field._symbol_f = _CharFormat
}

func newVarcharField() IField {
	return new(TVarcharField)
}

// TODO 限制长度
func (self *TVarcharField) Init(ctx *TTagContext) {
	field := ctx.Field.Base()
	params := ctx.Params

	if len(params) > 0 {
		size := utils.ToInt(params[0])
		if size != 0 {
			field._attr_size = size
			field.SqlType.DefaultLength = size
		}
		field.SqlType = SQLType{Varchar, size, 0}
	} else {
		field.SqlType = SQLType{Varchar, 0, 0}
	}
	field._attr_type = Varchar
	field._attr_store = true
	field._symbol_c = `'%s'`
	field._symbol_f = _CharFormat
}

func newTextField() IField {
	return new(TTextField)
}

func (self *TTextField) Init(ctx *TTagContext) {
	field := ctx.Field.Base()
	params := ctx.Params

	field._attr_type = Text
	field._attr_store = true
	field._symbol_c = `'%s'`
	field._symbol_f = _CharFormat

	if field.SqlType.Name == "" {
		field.SqlType = SQLType{Text, 0, 0}
	}

	if len(params) > 0 {
		size := utils.ToInt(params[0])
		if size != 0 {
			field._attr_size = size
			field.SqlType.DefaultLength = size
		}
	}
}
