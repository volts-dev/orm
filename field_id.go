package orm

import (
	"reflect"
	//"github.com/rs/xid"
	"github.com/bwmarrin/snowflake"
	"github.com/volts-dev/orm/domain"
	"github.com/volts-dev/orm/logger"
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

// 转换值到字段输出数据类型
func (self *TIdField) onConvertToRead(session *TSession, cols []string, record []interface{}, colIndex int) interface{} {
	value := reflect.ValueOf(record[colIndex]).Elem().Interface()
	l := len(cols)
	if value == nil && l > 1 {
		node := domain.NewDomainNode()

		for I := 0; I < len(cols); I++ {
			//logger.Dbg(len(cols), cols[I], "=", value2FieldTypeValue(self, reflect.ValueOf(record[I]).Elem().Interface()))
			cond := domain.NewDomainNode(cols[I], "=", value2FieldTypeValue(self, reflect.ValueOf(record[I]).Elem().Interface()))
			node.AND(cond)
		}

		id := uuid.Generate().Int64()
		session.New().Set("id", id).Domain(node).Write(nil) // 无需额外数据写入
		logger.Dbg(id, domain.Domain2String(node))
		return id
	} else {
		return value2FieldTypeValue(self, value)
	}
}
