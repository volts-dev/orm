package orm

import (
	"github.com/go-xorm/core"
)

type (
	TIntField struct {
		TField
	}

	TBigIntField struct {
		TField
	}

	TFloatField struct {
		TField
	}

	TDoubleField struct {
		TField
	}
)

func init() {
	// int(size)
	RegisterField("int", NewIntField)
	RegisterField("bigint", NewBigIntField)
	RegisterField("float", NewFloatField)
	RegisterField("double", NewDoubleField)
}

func NewIntField() IField {
	return new(TIntField)
}

func NewBigIntField() IField {
	return new(TBigIntField)
}

func NewFloatField() IField {
	return new(TFloatField)
}

func NewDoubleField() IField {
	return new(TDoubleField)
}

func (self *TIntField) Init(ctx *TFieldContext) {
	col := ctx.Column
	fld := ctx.Field
	vals := ctx.Params

	if len(vals) > 0 {
		switch vals[0] {
		case "64":
			col.SQLType = core.SQLType{core.BigInt, 0, 0}
			fld.Base()._column_type = core.BigInt
			fld.Base()._attr_type = "bigint"
		default:
			col.SQLType = core.SQLType{core.Int, 0, 0}
			fld.Base()._column_type = core.Int
			fld.Base()._attr_type = "int"
		}
	} else {
		col.SQLType = core.SQLType{core.Int, 0, 0}
		fld.Base()._column_type = core.Int
		fld.Base()._attr_type = "int"
	}
}

func (self *TBigIntField) Init(ctx *TFieldContext) {
	col := ctx.Column
	fld := ctx.Field

	col.SQLType = core.SQLType{core.BigInt, 0, 0}
	fld.Base()._column_type = core.BigInt
	fld.Base()._attr_type = "bigint"
}

func (self *TFloatField) Init(ctx *TFieldContext) {
	col := ctx.Column
	fld := ctx.Field

	col.SQLType = core.SQLType{core.Float, 0, 0}
	fld.Base()._column_type = core.Float
	//fld._type = core.Float
	fld.Base()._attr_type = "float"
}

func (self *TDoubleField) Init(ctx *TFieldContext) {
	col := ctx.Column
	fld := ctx.Field

	col.SQLType = core.SQLType{core.Double, 0, 0}
	fld.Base()._column_type = core.Double
	fld.Base()._attr_type = "double"
}
