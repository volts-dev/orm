package orm

import (
	"github.com/go-xorm/core"
)

type (
	TDateField struct {
		TField
	}

	TDateTimeField struct {
		TField
	}
)

func init() {
	RegisterField("date", NewDateField)
	RegisterField("datetime", NewDateTimeField)
}

func NewDateField() IField {
	return new(TDateField)
}

func NewDateTimeField() IField {
	return new(TDateTimeField)
}

func (self *TDateField) Init(ctx *TFieldContext) {
	col := ctx.Column
	fld := ctx.Field

	col.SQLType = core.SQLType{core.Date, 0, 0}
	fld.Base()._column_type = core.Date
	fld.Base()._attr_type = "date"
}

func (self *TDateTimeField) Init(ctx *TFieldContext) {
	col := ctx.Column
	fld := ctx.Field

	col.SQLType = core.SQLType{core.DateTime, 0, 0}
	fld.Base()._column_type = core.DateTime
	fld.Base()._attr_type = "datetime"
}
