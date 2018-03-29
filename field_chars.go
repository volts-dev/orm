package orm

import (
	"github.com/go-xorm/core"
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
	RegisterField("char", NewCharField)
	RegisterField("text", NewTextField)
}

func NewCharField() IField {
	return new(TCharField)
}

func NewTextField() IField {
	return new(TTextField)
}

func (self *TCharField) Init(ctx *TFieldContext) {
	col := ctx.Column
	fld := ctx.Field

	col.SQLType = core.SQLType{core.Varchar, 0, 0}
	fld.Base()._column_type = core.Varchar
	fld.Base()._attr_type = "char"
	fld.Base()._symbol_c = `'%s'`
	fld.Base()._symbol_f = _CharFormat
}

func (self *TTextField) Init(ctx *TFieldContext) {
	col := ctx.Column
	fld := ctx.Field

	col.SQLType = core.SQLType{core.Text, 0, 0}
	fld.Base()._column_type = core.Text
	fld.Base()._attr_type = "text"
	fld.Base()._symbol_c = `'%s'`
	fld.Base()._symbol_f = _CharFormat
}
