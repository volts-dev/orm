package orm

import (
	//"github.com/rs/xid"
	"github.com/bwmarrin/snowflake"
	//"github.com/google/uuid"
)

type (
	// Uuid field
	TIdField struct {
		TField
	}
)

var uuid *snowflake.Node

func init() {
	RegisterField("id", newIdField)

	var err error
	uuid, err = snowflake.NewNode(1)
	if err != nil {
		logger.Err(err)
	}
}

func newIdField() IField {
	return new(TIdField)
}

func (self *TIdField) Init(ctx *TFieldContext) {
	fld := ctx.Field
	model := ctx.Model

	// set type for field
	fld.Base().SqlType = SQLType{BigInt, 0, 0}
	fld.Base()._attr_type = Int

	// set the id field for model
	model.IdField(fld.Name())
}

func (self *TIdField) OnCreate(ctx *TFieldEventContext) interface{} {
	id := uuid.Generate()
	return id.Int64()
}
