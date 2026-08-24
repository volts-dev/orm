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
	field.store = true
	/* 以声明类型为主 */
	if ctx.FieldTypeValue.IsValid() {
		field.SqlType = GoType2SQLType(ctx.FieldTypeValue.Type())
	} else {
		field.SqlType = SQLType{Int, 0, 0}
	}
	field.typeName = field.SqlType.Name
}

func (self *TBigIntField) Init(ctx *TTagContext) {
	field := ctx.Field.Base()
	field.store = true

	if ctx.FieldTypeValue.IsValid() {
		field.SqlType = GoType2SQLType(ctx.FieldTypeValue.Type())
	} else {
		field.SqlType = SQLType{BigInt, 0, 0}
		field.typeName = BigInt
	}
	field.typeName = field.SqlType.Name
}

// 浮点列的宽度取「标签」与「Go 字段类型」中**更宽**的那个，绝不收窄。
//
// # 病灶
//
// 这两个 Init 从前无条件按标签定型，把 ctx.FieldTypeValue 整个无视掉了
// （int/bigint 那两个 Init 一直是看 Go 类型的，只有浮点这里没跟上）。于是
//
//	Amount float64 `field:"float() title('金额')"`
//
// 在 PostgreSQL 上建出来的是 REAL——**单精度**，约 7 位有效数字。金额过万就开始
// 掉小数：写进去 12345.67，读出来 12345.7。不报错、不告警，只有错的数，而且错在
// 最不该错的列上。modules 全仓因此改用 double()（287 处 double 对 12 处 float），
// 并在 gamification_goal.go 留了一行"浮点一律用 double()"的注释——绕过而非修复。
//
// # 规则
//
// float32 + float()  → REAL              （调用方两边都说了单精度）
// float64 + float()  → DOUBLE PRECISION  （Go 类型更宽，以它为准）
// float32 + double() → DOUBLE PRECISION  （标签更宽，以它为准）
// 非浮点的 Go 类型     → 按标签            （行为不变，不去猜）
//
// 想要单精度就把 Go 侧也写成 float32：一个只在 Go 里是 float64、在库里是 real 的
// 字段，两侧永远对不齐，省下的 4 字节抵不上一次金额对不上账。
func widerFloatType(ctx *TTagContext, tagged SQLType) SQLType {
	if !ctx.FieldTypeValue.IsValid() {
		return tagged
	}
	if fromGo := GoType2SQLType(ctx.FieldTypeValue.Type()); fromGo.Name == Double {
		return SQLType{Double, 0, 0}
	}
	return tagged
}

func (self *TFloatField) Init(ctx *TTagContext) {
	field := ctx.Field.Base()
	field.store = true
	field.SqlType = widerFloatType(ctx, SQLType{Float, 0, 0})
	field.typeName = field.SqlType.Name
}

func (self *TDoubleField) Init(ctx *TTagContext) {
	field := ctx.Field.Base()
	field.store = true
	field.SqlType = widerFloatType(ctx, SQLType{Double, 0, 0})
	field.typeName = field.SqlType.Name
}
