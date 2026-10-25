package orm

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/volts-dev/utils"
)

// 「取一部分」粒度的 domain 叶子：`(日期字段.粒度, 操作符, 整数)`，Odoo 17+ 的写法
// （`('date.month_number', '=', 3)`、`('create_date.day_of_week', 'in', [0, 6])`）。
//
// 透视表 / 图表 / 电子表格按 month_number 这类粒度分组之后，每一格的下钻条件就是它
// （formatted_read_group 回包里的 __extra_domain）。此前这种左值被闸门判成非法字段、生成
// `0 = 1`——下钻永远是空列表；上层只好给空条件，又变成「看到整个原 domain 的记录」。
//
// 取值与 ReadGroup 同一个表达式（granularityExpr），所以「这一格的数」与「点进去列出来的条数」
// 一定对得上。datetime 字段按会话 context 的 tz 取本地时间（与 ReadGroup 的 Tz 同口径）；
// 没有 tz 时按 UTC。

// resolveGranularityPath 认出 `字段.取一部分粒度`：字段必须是本模型上存储的 date / datetime。
// 认不出回 (nil, "")，调用方照旧当非法字段处理。
func resolveGranularityPath(model IModel, left string) (IField, string) {
	name, gran, found := strings.Cut(left, ".")
	if !found || name == "" || !IsNumberGranularity(gran) {
		return nil, ""
	}
	field := model.GetFieldByName(name)
	if field == nil || !field.Store() {
		return nil, ""
	}
	if !isDateLikeField(field) {
		return nil, ""
	}
	return field, gran
}

// isDateLikeField：date / datetime 字段。typeName 是大写常量（Date = "DATE"），老字段可能只有 SQL 类型，
// 两边都认、大小写无关。
func isDateLikeField(field IField) bool {
	for _, t := range []string{field.TypeName(), field.SQLType().Name} {
		switch strings.ToUpper(t) {
		case Date, DateTime, "TIMESTAMP", "TIMESTAMPZ", "TIMESTAMPTZ":
			return true
		}
	}
	return false
}

// granularityLeafToSql 生成单个粒度叶子的 SQL。只认比较与 in / not in；值必须是整数（或 false = 空）。
func granularityLeafToSql(dialect IDialect, aliasTable string, field IField, gran, operator string, vals []any, tz string) (string, []any, error) {
	if !dialect.Features().DateTrunc {
		return "", nil, fmt.Errorf("filtering on date parts needs Features.DateTrunc (postgres), current dialect is %s", dialect.String())
	}
	if tz != "" {
		if _, err := time.LoadLocation(tz); err != nil || strings.ContainsAny(tz, `'"\;`) {
			tz = ""
		}
	}
	col := fmt.Sprintf("%s.%s", aliasTable, dialect.Quoter().Quote(field.Name()))
	isDate := strings.EqualFold(field.SQLType().Name, "DATE")
	expr := granularitySql(dialect, col, gran, isDate, tz)

	isFalse := func(v any) bool { b, ok := v.(bool); return ok && !b }
	toInt := func(v any) (int64, error) {
		switch t := v.(type) {
		case int:
			return int64(t), nil
		case int64:
			return t, nil
		case float64:
			return int64(t), nil
		}
		n, err := strconv.ParseInt(strings.TrimSpace(utils.ToString(v)), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("date part value must be an integer, got %v", v)
		}
		return n, nil
	}

	switch operator {
	case "=", "!=", "<>", "<", "<=", ">", ">=":
		if len(vals) != 1 {
			return "", nil, fmt.Errorf("operator %s expects one value", operator)
		}
		if isFalse(vals[0]) || vals[0] == nil {
			switch operator {
			case "=":
				return fmt.Sprintf("%s IS NULL", col), nil, nil
			case "!=", "<>":
				return fmt.Sprintf("%s IS NOT NULL", col), nil, nil
			}
			return "", nil, fmt.Errorf("operator %s cannot compare to false", operator)
		}
		n, err := toInt(vals[0])
		if err != nil {
			return "", nil, err
		}
		op := operator
		if op == "!=" {
			op = "<>"
		}
		return fmt.Sprintf("%s %s ?", expr, op), []any{n}, nil
	case "in", "not in":
		var nums []any
		holders := []string{}
		for _, v := range vals {
			if list, ok := v.([]any); ok {
				for _, x := range list {
					n, err := toInt(x)
					if err != nil {
						return "", nil, err
					}
					nums = append(nums, n)
					holders = append(holders, "?")
				}
				continue
			}
			n, err := toInt(v)
			if err != nil {
				return "", nil, err
			}
			nums = append(nums, n)
			holders = append(holders, "?")
		}
		if len(nums) == 0 {
			if operator == "in" {
				return "0 = 1", nil, nil
			}
			return "1 = 1", nil, nil
		}
		return fmt.Sprintf("%s %s (%s)", expr, strings.ToUpper(operator), strings.Join(holders, ",")), nums, nil
	}
	return "", nil, fmt.Errorf("operator %s is not supported on date parts", operator)
}
