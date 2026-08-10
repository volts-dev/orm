package orm

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/volts-dev/utils"
)

// 规格筛选：把 domain 叶子 `('product_properties.3adf37f3', '=', '丝')` 下推成
// jsonb 条件，而不是取回全表在内存里过滤。
//
// 两种形态，取舍只有一条标准——能不能用上 GIN 索引：
//
//	等值    col @> '{"key":"值"}'::jsonb     ← GIN 认这个，走索引
//	其余    col ->> 'key' <op> 值            ← 提取成文本再比，全表扫
//
// 所以别"图统一"把等值也写成 `->>`：那是把唯一一个能走索引的场景也变成顺序扫描，
// 而且症状是"能用，就是慢"，不会有人报 bug。

// resolvePropertyPath 把 `<字段>.<规格项名>` 拆开；不是 properties 字段就返回 nil。
func resolvePropertyPath(model IModel, left string) (IField, string) {
	name, key, found := strings.Cut(left, ".")
	if !found || name == "" || key == "" {
		return nil, ""
	}
	// 规格项名来自用户数据，只允许 name 该有的字符集。它要作为**参数**传给
	// `->`/`->>`（不是拼进 SQL），这道校验是第二层保险，也顺带挡住 `a.b.c` 这种
	// 走错路的左值。
	if strings.ContainsAny(key, `"'`+"`;() \t\r\n") {
		return nil, ""
	}
	field := model.GetFieldByName(name)
	if field == nil {
		return nil, ""
	}
	if _, ok := field.(*TPropertiesField); !ok {
		return nil, ""
	}
	return field, key
}

// propertyLeafToSql 生成单个规格项的 SQL 条件。
func propertyLeafToSql(dialect IDialect, aliasTable, colName, propName, operator string, vals []any) (string, []any, error) {
	if dialect.DBType() != POSTGRES {
		// MySQL 的 `->>`、SQLite 的 json_extract 语法各不相同，且都没有 GIN。
		// 与其发一条在别的库上语义漂移的 SQL，不如明确报错——本仓的目标库是 PG。
		return "", nil, fmt.Errorf("filtering on properties is only implemented for postgres, got %s", dialect.DBType())
	}

	quoter := dialect.Quoter()
	col := fmt.Sprintf("%s.%s", aliasTable, quoter.Quote(colName))

	var value any
	if len(vals) > 0 {
		value = vals[0]
	}

	switch operator {
	case "=", "!=", "<>":
		negate := operator != "="
		if isBlankPropertyValue(value) {
			// "没填" = 键不在 / 值是 null / 值是 false。三种都得认：写入路径把
			// 空值归一成 false，而从没填过的项压根没有这个键。
			q := fmt.Sprintf("(%s IS NULL OR (%s -> ?) IS NULL OR (%s -> ?) IN ('null'::jsonb, 'false'::jsonb))", col, col, col)
			if negate {
				q = "NOT " + q
			}
			return q, []any{propName, propName}, nil
		}

		literal, err := propertyContainmentLiteral(propName, value)
		if err != nil {
			return "", nil, err
		}
		q := fmt.Sprintf("(%s @> ?::jsonb)", col)
		if negate {
			// NULL 列 @> 出来是 NULL，NOT NULL 还是 NULL——不补这个 IS NULL，
			// "规格不等于 X" 会把从没填过规格的记录整批漏掉。
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
			terms = append(terms, fmt.Sprintf("%s @> ?::jsonb", col))
			params = append(params, literal)
		}
		q := "(" + strings.Join(terms, " OR ") + ")"
		if operator == "not in" {
			q = fmt.Sprintf("(%s IS NULL OR NOT %s)", col, q)
		}
		return q, params, nil

	case "like", "ilike", "not like", "not ilike", "=like", "=ilike":
		sqlOp := strings.TrimPrefix(operator, "=")
		needWildcard := !strings.HasPrefix(operator, "=")
		pattern := utils.ToString(value)
		if needWildcard {
			pattern = "%" + pattern + "%"
		}
		// ->> 取文本；键不存在时是 NULL，NULL LIKE ... 为 NULL，语义上等于不匹配，
		// 正合 like 的预期，不必额外补 IS NULL。
		return fmt.Sprintf("((%s ->> ?) %s ?)", col, sqlOp), []any{propName, pattern}, nil

	case ">", ">=", "<", "<=":
		if isNumericPropertyValue(value) {
			// ::numeric 对非数字文本会在**运行时**报错，所以只在比较值本身是数字时
			// 才走这条：拿数字去比一列字符串规格是调用方的问题，让它报出来。
			return fmt.Sprintf("((%s ->> ?)::numeric %s ?)", col, operator), []any{propName, value}, nil
		}
		// 日期/日期时间在 jsonb 里存的是 ISO 字符串，字典序与时间序一致，直接文本比。
		return fmt.Sprintf("((%s ->> ?) %s ?)", col, operator), []any{propName, utils.ToString(value)}, nil
	}

	return "", nil, fmt.Errorf("operator %q is not supported on properties", operator)
}

// propertyContainmentLiteral 生成 `{"key": value}` 这个用于 @> 的 jsonb 文本。
func propertyContainmentLiteral(propName string, value any) (string, error) {
	buf, err := json.Marshal(map[string]any{propName: value})
	if err != nil {
		return "", fmt.Errorf("cannot encode property value %v: %s", value, err.Error())
	}
	return string(buf), nil
}

// isBlankPropertyValue 判断筛选值是不是"空"。
// 数值 0 **不算空**——0 是合法的规格值，跟写入侧那条口子必须保持一致，
// 否则 ('功率','=',0) 会去查"没填过功率的产品"。
func isBlankPropertyValue(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case bool:
		return !v
	case string:
		return v == ""
	}
	if isNumericPropertyValue(value) {
		return false
	}
	return utils.IsBlank(value)
}

func isNumericPropertyValue(value any) bool {
	switch value.(type) {
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return true
	case json.Number:
		return true
	}
	return false
}
