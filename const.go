package orm

const (
	DefaultLimit        = 500
	DefaultIdField      = "id"
	DefaultNameField    = "name"
	// DisplayNameField 是「记录显示名」的约定字段名(对标 Odoo)。调用方通常把它当作
	// 关系字段的显示文本；模型可以没有它(此时退回 rec_name)。
	DisplayNameField = "display_name"
	DefaultIndexPrefix  = "IDX_"
	DefaultUniquePrefix = "UQE_"

	FieldIdentifier = "field"
	TableIdentifier = "table"
)
