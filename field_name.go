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

	field.typeName = Varchar
	field.store = true
	field.SqlType = SQLType{Varchar, 0, 0}
	if len(params) > 0 {
		if size := utils.ToInt(params[0]); size != 0 {
			field.size = size
			field.SqlType.DefaultLength = size
		}
	}

	// set the id field for model
	ctx.Model.SetRecordName(field.Name())
}
