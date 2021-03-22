package orm

type (
	TIntField struct {
		TField
	}

	TBigIntField struct {
		TField
	}

	TFloatField struct {
		TField
	}

	TDoubleField struct {
		TField
	}
)

func init() {
	// int(size)
	RegisterField("int", newIntField)
	RegisterField("bigint", newBigIntField)
	RegisterField("float", newFloatField)
	RegisterField("double", newDoubleField)
}

func newIntField() IField {
	return new(TIntField)
}

func newBigIntField() IField {
	return new(TBigIntField)
}

func newFloatField() IField {
	return new(TFloatField)
}

func newDoubleField() IField {
	return new(TDoubleField)
}

func (self *TIntField) Init(ctx *TFieldContext) {
	fld := ctx.Field
	vals := ctx.Params

	if len(vals) > 0 {
		switch vals[0] {
		case "64":
			fld.Base().SqlType = SQLType{BigInt, 0, 0}
			fld.Base()._attr_type = "bigint"
		default:
			fld.Base().SqlType = SQLType{Int, 0, 0}
			fld.Base()._attr_type = "int"
		}
	} else {
		fld.Base().SqlType = SQLType{Int, 0, 0}
		fld.Base()._attr_type = "int"
	}
}

func (self *TBigIntField) Init(ctx *TFieldContext) {
	fld := ctx.Field

	fld.Base().SqlType = SQLType{BigInt, 0, 0}
	fld.Base()._attr_type = "bigint"
}

func (self *TFloatField) Init(ctx *TFieldContext) {
	fld := ctx.Field

	fld.Base().SqlType = SQLType{Float, 0, 0}
	fld.Base()._attr_type = "float"
}

func (self *TDoubleField) Init(ctx *TFieldContext) {
	fld := ctx.Field

	fld.Base().SqlType = SQLType{Double, 0, 0}
	fld.Base()._attr_type = "double"
}
