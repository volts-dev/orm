package orm

const (
	DefaultLimit     = 500
	DefaultIdField   = "id"
	DefaultNameField = "name"
	// DefaultParentField 层级查询(child_of/parent_of)默认的父链接字段名，对齐 Odoo
	// 的 _parent_name。模型可以改用别的自引用 many2one，见 hierarchyParentField。
	DefaultParentField = "parent_id"
	// DisplayNameField 是「记录显示名」的约定字段名(对标 Odoo)。调用方通常把它当作
	// 关系字段的显示文本；模型可以没有它(此时退回 rec_name)。
	DisplayNameField    = "display_name"
	DefaultIndexPrefix  = "IDX_"
	DefaultUniquePrefix = "UQE_"

	FieldIdentifier = "field"
	TableIdentifier = "table"
)
