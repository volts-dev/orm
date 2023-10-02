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

func (self *TIntField) Init(ctx *TTagContext) {
	field := ctx.Field.Base()
	field._attr_store = true
	//if field.SqlType.Name != Int {
	field.SqlType = SQLType{Int, 0, 0}
	field._attr_type = Int
	//}
}

func (self *TBigIntField) Init(ctx *TTagContext) {
	field := ctx.Field.Base()
	field._attr_store = true
	//if field.SqlType.Name != BigInt {
	field.SqlType = SQLType{BigInt, 0, 0}
	field._attr_type = BigInt
	//}
}

func (self *TFloatField) Init(ctx *TTagContext) {
	field := ctx.Field.Base()
	field._attr_store = true
	//if field.SqlType.Name != Float {
	field.SqlType = SQLType{Float, 0, 0}
	field._attr_type = Float
	//}
}

func (self *TDoubleField) Init(ctx *TTagContext) {
	field := ctx.Field.Base()
	field._attr_store = true
	//if field.SqlType.Name != Double {
	field.SqlType = SQLType{Double, 0, 0}
	field._attr_type = Double
	//}
}
