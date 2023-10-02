package orm

type (
	FieldOption func(*TField)

	FieldConfig struct {
		field     *TField
		SQLType   SQLType
		FieldType string
	}
)

func newFieldConfig(field *TField) *FieldConfig {
	return &FieldConfig{field: field}
}

func (self *FieldConfig) Init(opts ...FieldOption) {
	for _, opt := range opts {
		opt(self.field)
	}
}

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
