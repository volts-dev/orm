package orm

type (
	TIdField struct {
		TField
	}
)

func init() {
	RegisterField("id", NewCharField)
}

func NewIdField() IField {
	return new(TIdField)
}
