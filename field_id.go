package orm

import (
	"github.com/bwmarrin/snowflake"
	"github.com/volts-dev/orm/domain"
	"github.com/volts-dev/utils"
	//"github.com/google/uuid"
	//"github.com/rs/xid"
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
		log.Err(err)
	}
}

func newIdField() IField {
	return new(TIdField)
}

func (self *TIdField) Init(ctx *TTagContext) {
	field := ctx.Field.Base()
	model := ctx.Model
	/*field_name := fld.Name()

	// set type for field

		// set unique
		var index *TIndex
		var ok bool
		indexName := generate_index_name(UniqueType, model.Table(), []string{field_name})

		if index, ok = model.Obj().indexes[indexName]; ok {
			index.AddColumn(field_name)
		} else {
			index = newIndex(indexName, UniqueType)
			index.AddColumn(field_name)
			model.Obj().AddIndex(index)
		}
	*/
	field.isPrimaryKey = true
	field.isUnique = true
	field._attr_required = true
	field._attr_store = true

	//if field.SqlType.Name == "" {
	field.SqlType = SQLType{BigInt, 0, 0}
	field._attr_type = "bigint"
	//}

	// set the id field for model
	model.IdField(field.Name())
}

func (self *TIdField) OnCreate(ctx *TFieldContext) interface{} {
	id := uuid.Generate()
	return id.Int64()
}

// 转换值到字段输出数据类型
func (self *TIdField) onConvertToRead(session *TSession, cols []string, record []interface{}, colIndex int) interface{} {
	value := *record[colIndex].(*interface{})
	l := len(cols)
	if value == nil && l > 1 {
		node := domain.NewDomainNode()
		for I, name := range cols {
			// 过滤某些大内容的字段
			if name != self.Name() {
				fieldValue := *record[I].(*interface{})
				if !utils.IsBlank(fieldValue) {
					// only use those not null value as condition for where clause
					// because of null/0/nil maybe the same when read from database
					cond := domain.NewDomainNode(name, "=", fieldValue)
					node.AND(cond)
				}
			}
		}

		id := uuid.Generate().Int64()
		session.New().Set("id", id).Domain(node).Limit(1).Write(nil) // 无需额外数据写入
		return id
	} else {
		return value2FieldTypeValue(self, value)
	}
}
