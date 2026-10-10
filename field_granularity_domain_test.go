package orm

import (
	"strings"
	"testing"
)

// 下钻条件必须与分组用同一个表达式：「这一格的数」= 「点进去的条数」。date 列先转无时区 timestamp
// 再 EXTRACT（与 granularityExpr 同一条，那里记着 date_trunc 走 timestamptz 重载错一天的坑）。
func TestGranularityLeafToSql_SameExpressionAsReadGroup(t *testing.T) {
	d := newPgDialectForTest(t)
	f, err := NewField("date_order", WithSQLType(SQLType{Date, 0, 0}))
	if err != nil {
		t.Fatal(err)
	}
	q, params, err := granularityLeafToSql(d, `"sale_order"`, f, "month_number", "=", []any{3}, "")
	if err != nil {
		t.Fatal(err)
	}
	want := granularityExpr(`"sale_order"."date_order"`, "month_number", true, "") + " = ?"
	if q != want {
		t.Errorf("got %s\nwant %s", q, want)
	}
	if len(params) != 1 || params[0] != int64(3) {
		t.Errorf("params = %#v", params)
	}
}

// 空组（值是 false）下钻到「这一列为空」，不是 EXTRACT(...) = false（那会是一条 SQL 错误）。
func TestGranularityLeafToSql_FalseMeansNull(t *testing.T) {
	d := newPgDialectForTest(t)
	f, _ := NewField("date_order", WithSQLType(SQLType{DateTime, 0, 0}))
	q, params, err := granularityLeafToSql(d, `"t"`, f, "day_of_week", "=", []any{false}, "")
	if err != nil || !strings.Contains(q, "IS NULL") || len(params) != 0 {
		t.Errorf("got %q %v %v", q, params, err)
	}
}

func TestGranularityLeafToSql_InAndBadValues(t *testing.T) {
	d := newPgDialectForTest(t)
	f, _ := NewField("d", WithSQLType(SQLType{DateTime, 0, 0}))
	q, params, err := granularityLeafToSql(d, `"t"`, f, "quarter_number", "in", []any{[]any{1, "2"}}, "")
	if err != nil || !strings.Contains(q, "IN (?,?)") || len(params) != 2 {
		t.Errorf("in: %q %v %v", q, params, err)
	}
	if _, _, err := granularityLeafToSql(d, `"t"`, f, "quarter_number", "=", []any{"1; drop table x"}, ""); err == nil {
		t.Error("非整数的值应该被拒（它会进参数，但这一类条件只该收整数）")
	}
	if _, _, err := granularityLeafToSql(d, `"t"`, f, "quarter_number", "like", []any{1}, ""); err == nil {
		t.Error("like 不该被接受")
	}
}
