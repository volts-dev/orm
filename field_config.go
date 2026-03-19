package orm

type (
	FieldOption func(*TField)
)

func WithSQLType(v SQLType) FieldOption {
	return func(field *TField) {
		/* 特定Field类型结构不可修改SQLType如使用RegisterField注册的Field类型 */
		if field._attr_type == "" {
			field.SqlType = v
		}
	}
}

func WithFieldType(v string) FieldOption {
	return func(field *TField) {
		field._attr_type = v
	}
}
