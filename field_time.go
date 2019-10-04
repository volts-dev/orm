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

func (self *TDateField) Init(ctx *TFieldContext) {
	fld := ctx.Field

	fld.Base().SqlType = SQLType{Date, 0, 0}
	//	fld.Base()._column_type = Date
	fld.Base()._attr_type = "date"
}

func (self *TDateTimeField) Init(ctx *TFieldContext) {
	fld := ctx.Field

	fld.Base().SqlType = SQLType{DateTime, 0, 0}
	//	fld.Base()._column_type = DateTime
	fld.Base()._attr_type = "datetime"
}
