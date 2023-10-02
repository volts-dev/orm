package orm

import "github.com/volts-dev/utils"

type (
	// Uuid field
	TNameField struct {
		TField
	}
)

func init() {
	RegisterField("recname", newNameField)
}

func newNameField() IField {
	return new(TNameField)
}

func (self *TNameField) Init(ctx *TTagContext) {
	field := ctx.Field.Base()
	params := ctx.Params

	if len(params) > 0 {
		size := utils.ToInt(params[0])
		if size != 0 {
			field._attr_size = size
			field.SqlType.DefaultLength = size
		}
		field.SqlType = SQLType{Varchar, size, 0}
	} else {
		field.SqlType = SQLType{Varchar, 0, 0}
	}
	field._attr_type = Varchar
	field._attr_store = true

	// set the id field for model
	ctx.Model.SetRecordName(field.Name())
}
