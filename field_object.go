package orm

import (
	"github.com/go-xorm/core"
)

type (
	TBinField struct {
		TField
		attachment bool
	}

	THtmlField struct {
		TField
	}
)

func init() {
	RegisterField("binary", NewBinField)
	RegisterField("html", NewHtmlField)
}

func NewBinField() IField {
	return new(TBinField)
}

func NewHtmlField() IField {
	return new(THtmlField)
}

func (self *TBinField) Init(ctx *TFieldContext) {
	col := ctx.Column
	//fld := ctx.Field

	col.SQLType = core.SQLType{core.Binary, 0, 0}
	self.Base()._column_type = core.Binary
	//fld._classic_read = false
	//fld._classic_write = false
	self.Base()._attr_type = "binary"
	self.attachment = false
}
