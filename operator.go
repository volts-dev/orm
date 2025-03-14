package orm

// An Operator inside an SQL WHERE clause
type Operator string

// Operators
const (
	Equals         Operator = "="
	NotEquals      Operator = "!="
	Greater        Operator = ">"
	GreaterOrEqual Operator = ">="
	Lower          Operator = "<"
	LowerOrEqual   Operator = "<="
	Like           Operator = "=like"
	Contains       Operator = "like"
	NotContains    Operator = "not like"
	IContains      Operator = "ilike"
	NotIContains   Operator = "not ilike"
	ILike          Operator = "=ilike"
	In             Operator = "in"
	NotIn          Operator = "not in"
	ChildOf        Operator = "child_of"
)
