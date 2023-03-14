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

func (self *TNameField) Init(ctx *TFieldContext) {
	fld := ctx.Field
	params := ctx.Params

	if len(params) > 0 {
		size := utils.ToInt(params[0])
		if size != 0 {
			fld.Base()._attr_size = size
		}
	}
	// set type for field
	fld.Base().SqlType = SQLType{Varchar, 0, 0}
	fld.Base()._attr_type = "char"

	// set the id field for model
	ctx.Model.SetRecordName(fld.Name())
}
