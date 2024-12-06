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
	/* 以声明类型为主 */
	if ctx.FieldTypeValue.IsValid() {
		field.SqlType = GoType2SQLType(ctx.FieldTypeValue.Type())
	} else {
		field.SqlType = SQLType{Int, 0, 0}
	}
	field._attr_type = field.SqlType.Name
}

func (self *TBigIntField) Init(ctx *TTagContext) {
	field := ctx.Field.Base()
	field._attr_store = true

	if ctx.FieldTypeValue.IsValid() {
		field.SqlType = GoType2SQLType(ctx.FieldTypeValue.Type())
	} else {
		field.SqlType = SQLType{BigInt, 0, 0}
		field._attr_type = BigInt
	}
	field._attr_type = field.SqlType.Name
}

func (self *TFloatField) Init(ctx *TTagContext) {
	field := ctx.Field.Base()
	field._attr_store = true
	field._attr_type = Float
	field.SqlType = SQLType{Float, 0, 0}
}

func (self *TDoubleField) Init(ctx *TTagContext) {
	field := ctx.Field.Base()
	field._attr_store = true
	field._attr_type = Double
	field.SqlType = SQLType{Double, 0, 0}
}
