package orm

type (
	TDateField struct {
		TField
	}

	TDateTimeField struct {
		TField
	}
)

func init() {
	RegisterField("date", newDateField)
	RegisterField("datetime", newDateTimeField)
}

func newDateField() IField {
	return new(TDateField)
}

func newDateTimeField() IField {
	return new(TDateTimeField)
}

func (self *TDateField) Init(ctx *TTagContext) {
	field := ctx.Field.Base()
	field.SqlType = SQLType{Date, 0, 0}
	field._attr_type = Date
	field._attr_store = true
}

func (self *TDateTimeField) Init(ctx *TTagContext) {
	field := ctx.Field.Base()
	field.SqlType = SQLType{DateTime, 0, 0}
	field._attr_type = DateTime
	field._attr_store = true
}
