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

	field.typeName = fieldType
	field.store = true
	field.formatChar = `'%s'`
	field.formatFunc = _CharFormat
	field.SqlType = SQLType{fieldType, 0, 0}
	if len(params) > 0 {
		if size := utils.ToInt(params[0]); size != 0 {
			field.size = size
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
