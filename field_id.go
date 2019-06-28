package orm

import (
	//"github.com/rs/xid"
	"github.com/bwmarrin/snowflake"
	"github.com/go-xorm/core"
)

type (
	// Uuid field
	TIdField struct {
		TField
	}
)

var uuid *snowflake.Node

func init() {
	RegisterField("id", NewIdField)

	var err error
	uuid, err = snowflake.NewNode(1)
	if err != nil {
		logger.Err(err)
	}
}

func NewIdField() IField {
	return new(TIdField)
}

func (self *TIdField) Init(ctx *TFieldContext) {
	col := ctx.Column
	fld := ctx.Field
	model := ctx.Model

	// set type for field
	col.SQLType = core.SQLType{core.BigInt, 0, 0}
	fld.Base()._column_type = core.BigInt
	fld.Base()._attr_type = "int"

	// set the id field for model
	model.IdField(fld.Name())
}

func (self *TIdField) OnCreate(ctx *TFieldEventContext) interface{} {
	id := uuid.Generate()
	return id.Int64()
}
