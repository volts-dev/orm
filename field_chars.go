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

func newVarcharField() IField {
	return new(TVarcharField)
}

func newTextField() IField {
	return new(TTextField)
}

func initCharField(ctx *TTagContext, fieldType string) {
	field := ctx.Field.Base()
	params := ctx.Params

	field._attr_type = fieldType
	field._attr_store = true
	field._symbol_c = `'%s'`
	field._symbol_f = _CharFormat
	field.SqlType = SQLType{fieldType, 0, 0}
	if len(params) > 0 {
		if size := utils.ToInt(params[0]); size != 0 {
			field._attr_size = size
			field.SqlType.DefaultLength = size
		}
	}
}

func (self *TCharField) Init(ctx *TTagContext) {
	initCharField(ctx, Char)
}

func (self *TVarcharField) Init(ctx *TTagContext) {
	initCharField(ctx, Varchar)
}

func (self *TTextField) Init(ctx *TTagContext) {
	initCharField(ctx, Text)
}
