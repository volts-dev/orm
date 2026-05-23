package orm

type (
	FieldOption func(*TField)
)

func WithModel(m IModel) FieldOption {
	return func(field *TField) {
		field.boundModel = m
	}
}

func WithSQLType(v SQLType) FieldOption {
	return func(field *TField) {
		/* 特定Field类型结构不可修改SQLType如使用RegisterField注册的Field类型 */
		if field.typeName == "" {
			field.SqlType = v
		}
	}
}

func WithFieldType(v string) FieldOption {
	return func(field *TField) {
		field.typeName = v
	}
}
