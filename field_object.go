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
	// 列是 TEXT，但类型名是 html：前端按类型名选控件，报成 TEXT 的话每个 html 字段都被画成 textarea、
	// 用户看到的是一堆 HTML 标签（2026-09-20 html_editor 接进 vertex 表单时发现）。按 typeName 归类的地方
	// 经 SqlTypes["HTML"] 仍当文本处理。
	ctx.Field.Base().typeName = TYPE_HTML
}

// onConvertToRead 把 bytea 列读回来的 []byte 还原成字符串。
//
// binary() 字段的值在本仓一律是 **base64 文本**（Odoo 语义：写进去的是 base64 字符串、读回来也是；
// XML 数据的 type="base64" file=… 由导入器编好再写，前端上传同样送 base64）。列是 bytea，存的就是
// 那串文本的字节，lib/pq 交回 []byte —— 原样往上交的话，一过 JSON（API 读、跨进程读）Go 会把
// []byte **再编一遍 base64**：社交营销 social.media 的平台图标读出来是 "UEhOMlp5…"
// （= base64(base64(<svg…))），/web/image 解一遍给出去的还是 base64 文本，一排碎图，不报错。
// 同一进程里用 AsString() 读的地方（/web/image 本地那条路）从前碰巧是对的，所以这病只在跨进程 /
// 前端直读时出现。
//
// 只动 binary() 这一种：其它 blob 列（Go 结构体里的 []byte 自动映射成的）仍给 []byte。
func (self *TBinField) onConvertToRead(session *TSession, cols []string, record []any, colIndex int) any {
	value := *record[colIndex].(*any)
	if b, ok := value.([]byte); ok {
		return string(b)
	}
	return value
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
