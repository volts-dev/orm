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
	fld := ctx.Field

	fld.Base().SqlType = SQLType{Binary, 0, 0}
	//fld._classic_read = false
	//fld._classic_write = false
	self.Base()._attr_type = "binary"
	fld.Base()._attr_store = true

	self.attachment = false
}
