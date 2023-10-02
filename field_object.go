package orm

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
	RegisterField("binary", newBinField)
	RegisterField("html", newHtmlField)
}

func newBinField() IField {
	return new(TBinField)
}

func newHtmlField() IField {
	return new(THtmlField)
}

func (self *TBinField) Init(ctx *TTagContext) {
	field := ctx.Field.Base()
	//if field.SqlType.Name == "" {
	field.SqlType = SQLType{Binary, 0, 0}
	field._attr_type = Binary
	//}
	//fld._classic_read = false
	//fld._classic_write = false
	field._attr_store = true
	field.attachment = false
}
