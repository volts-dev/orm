package orm

import (
	"fmt"
	"strings"

	"github.com/volts-dev/utils"
)

// MySQL 上的规格（properties）筛选（MySQL 兼容，2026-10-26）：口径同 PG 的 jsonb 写法（field_properties_domain.go）。
//
//	PG                          MySQL
//	col @> '{"k": v}'           JSON_CONTAINS(col, '{"k": v}')
//	col -> 'k'                  JSON_EXTRACT(col, CONCAT('$."', ?, '"'))（键作参数；resolvePropertyPath 已挡掉引号）
//	col ->> 'k'                 上面那个 JSON_UNQUOTE；JSON null 当 SQL NULL（PG 的 ->> 对 JSON null 回 NULL）
//
// MySQL 的 JSON 列没有 GIN：等值筛选也是全表扫。
func (db *mysql) propertyLeafSql(col, propName, operator string, vals []any) (string, []any, error) {
	path := "CONCAT('$.\"', ?, '\"')"
	get := "JSON_EXTRACT(" + col + ", " + path + ")"
	text := "(CASE WHEN JSON_TYPE(" + get + ") = 'NULL' THEN NULL ELSE JSON_UNQUOTE(" + get + ") END)"
	var value any
	if len(vals) > 0 {
		value = vals[0]
	}
	switch operator {
	case "=", "!=", "<>":
		negate := operator != "="
		if isBlankPropertyValue(value) {
			q := fmt.Sprintf("(%s IS NULL OR %s IS NULL OR JSON_TYPE(%s) = 'NULL' OR %s = CAST('false' AS JSON))", col, get, get, get)
			if negate {
				q = "NOT " + q
			}
			return q, []any{propName, propName, propName}, nil
		}
		literal, err := propertyContainmentLiteral(propName, value)
		if err != nil {
			return "", nil, err
		}
		q := fmt.Sprintf("JSON_CONTAINS(%s, ?)", col)
		if negate {
			q = fmt.Sprintf("(%s IS NULL OR NOT %s)", col, q)
		}
		return q, []any{literal}, nil
	case "in", "not in":
		if len(vals) == 0 {
			if operator == "in" {
				return "FALSE", nil, nil
			}
			return "TRUE", nil, nil
		}
		terms := make([]string, 0, len(vals))
		params := make([]any, 0, len(vals))
		for _, v := range vals {
			literal, err := propertyContainmentLiteral(propName, v)
			if err != nil {
				return "", nil, err
			}
			terms = append(terms, fmt.Sprintf("JSON_CONTAINS(%s, ?)", col))
			params = append(params, literal)
		}
		q := "(" + strings.Join(terms, " OR ") + ")"
		if operator == "not in" {
			q = fmt.Sprintf("(%s IS NULL OR NOT %s)", col, q)
		}
		return q, params, nil
	case "like", "ilike", "not like", "not ilike", "=like", "=ilike":
		sqlOp := strings.TrimPrefix(operator, "=")
		pattern := utils.ToString(value)
		if !strings.HasPrefix(operator, "=") {
			pattern = "%" + pattern + "%"
		}
		// text 里 get 出现两次（JSON_TYPE 与 JSON_UNQUOTE），各收一次键名，再一个收模式。
		return db.LikeClause(text, sqlOp), []any{propName, propName, pattern}, nil
	case ">", ">=", "<", "<=":
		if isNumericPropertyValue(value) {
			return fmt.Sprintf("(CAST(%s AS DECIMAL(65,30)) %s ?)", text, operator), []any{propName, propName, value}, nil
		}
		return fmt.Sprintf("(%s %s ?)", text, operator), []any{propName, propName, utils.ToString(value)}, nil
	}
	return "", nil, fmt.Errorf("operator %q is not supported on properties", operator)
}
