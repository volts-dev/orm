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

// Init 让 html 与 text 落成同一种列。
//
// ★ **早先 THtmlField 根本没有 Init。** 后果不是报错，是**静默不落库**：
// `RegisterField("html", …)` 让 `field:"html()"` 解析得过去，但 Init 缺席意味着
// `store` 保持零值 false、`SqlType` 为空 —— SyncModel 不给它建列，写入被丢掉，
// 读回来那个键**根本不存在**（不是空串）。表现是"备注框里写的东西保存后就没了"，
// 全程无报错、无日志。2026-08-26 由 crm.lead.description（线索备注）撞出来，
// crm.lead.lost.lost_feedback（丢单结案说明）与 crm.activity.report.body 同病。
//
// 与 text 的差别只在**语义**（这一列存的是 HTML，前端该用富文本控件渲染），
// 列类型本来就该一样；Odoo 的 fields.Html 也是继承 fields.Text。
func (self *THtmlField) Init(ctx *TTagContext) {
	initCharField(ctx, Text)
}

func (self *TBinField) Init(ctx *TTagContext) {
	field := ctx.Field.Base()
	//if field.SqlType.Name == "" {
	field.SqlType = SQLType{Binary, 0, 0}
	field.typeName = Binary
	//}
	//fld._classic_read = false
	//fld._classic_write = false
	field.store = true
	self.attachment = false
}
