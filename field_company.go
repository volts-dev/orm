package orm

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/volts-dev/utils"
)

// 按公司区分的字段（Odoo company_dependent，Odoo 18 起的存法）。
//
// 同一条记录在不同公司里可以有不同的值：客户在 A 公司用价格表 X、在 B 公司用 Y。
// 列是 jsonb：{"<公司 id>": 值, "0": 不分公司的值}。
//
//   - 读 / 条件 / 排序 / 分组：取会话**当前公司**那一格，没有就回落到 "0" 那一格：
//     COALESCE(col->>'<公司>', col->>'0')::<值类型>。会话没有公司时只看 "0"。
//   - 写：只改当前公司那一格（jsonb_set），**绝不整列覆盖** —— 整列覆盖会抹掉别的公司的值。
//     写空值 = 删掉这一格（于是读回落到 "0"）。
//   - 新建：{"<当前公司>": 值}。
//
// 字段的 SqlType 仍是值的逻辑类型（多对一是 BIGINT），值转换都照它走；只有建列时
// 方言把存储类型换成 JSONB（postgres.GetSqlType）。只支持 postgres。
//
// 当前公司从会话的 context 里来（WithCompanyId，或惰性的 WithCompanyLookup）：宿主在每个
// 请求建模型会话时放进去，Records() / Db() / Tx() 建出的会话都带着它。
//
// 公司 id 以**整数字面量**拼进 SQL，不走参数：SELECT 列和 ORDER BY 没有参数通道；
// 字面量还让「按 SQL 原文 + 参数」做键的查询缓存天然按公司分开。

type companyIdKey struct{}

// WithCompanyId 返回带「当前公司」的 context。id <= 0 表示没有当前公司。
func WithCompanyId(ctx context.Context, id int64) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, companyIdKey{}, id)
}

type companyLookupKey struct{}

// WithCompanyLookup 返回带「当前公司来源」的 context：没有显式的当前公司（WithCompanyId）时，
// **第一次真正用到**按公司区分的字段才调 fn 取。
//
// 宿主用它回落到用户自己的默认公司（Odoo：没有指定公司时 env.company 是用户的默认公司，
// 不是「不分公司」）。取默认公司要查库，所以做成惰性的：绝大多数读写根本不碰这类字段。
// fn 可能被调用多次，由宿主自己缓存（例如 sync.OnceValue）。
func WithCompanyLookup(ctx context.Context, fn func() int64) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, companyLookupKey{}, fn)
}

// CompanyIdFrom 取 context 里的当前公司：先看 WithCompanyId，没有再问 WithCompanyLookup；都没有为 0。
func CompanyIdFrom(ctx context.Context) int64 {
	if ctx == nil {
		return 0
	}
	if id, _ := ctx.Value(companyIdKey{}).(int64); id > 0 {
		return id
	}
	if fn, _ := ctx.Value(companyLookupKey{}).(func() int64); fn != nil {
		if id := fn(); id > 0 {
			return id
		}
	}
	return 0
}

// CompanyId 是这个会话的当前公司（见 WithCompanyId）；0 = 没有。
func (self *TSession) CompanyId() int64 {
	return CompanyIdFrom(self.context)
}

// IsCompanyDependent 报告字段是否按公司区分（tag company_dependent）。
func IsCompanyDependent(f IField) bool {
	return f != nil && f.Base() != nil && f.Base().companyDependent
}

func tag_company_dependent(ctx *TTagContext) error {
	f := ctx.Field
	switch f.TypeName() {
	case TYPE_O2M, TYPE_M2M, TYPE_O2O:
		return fmt.Errorf("orm: %s.%s: company_dependent is not supported on %s fields", ctx.Model.String(), f.Name(), f.TypeName())
	}
	if ctx.Orm != nil && ctx.Orm.dialect != nil && ctx.Orm.dialect.DBType() != POSTGRES {
		return fmt.Errorf("orm: %s.%s: company_dependent needs postgres (jsonb), current dialect is %s",
			ctx.Model.String(), f.Name(), ctx.Orm.dialect.DBType())
	}
	f.Base().companyDependent = true
	return nil
}

// companyValueType 是字段值的 SQL 类型（取出一格之后 cast 成它）。
func companyValueType(d IDialect, field IField) (string, error) {
	vt, ok := d.(interface{ valueSqlType(IField) string })
	if !ok {
		return "", fmt.Errorf("orm: company_dependent field %s needs postgres", field.Name())
	}
	return vt.valueSqlType(field), nil
}

// companyValueExpr 渲染「ref 这一列在公司 cid 里的值」。ref 是已经引用好的列（"t"."col"）。
func companyValueExpr(d IDialect, ref string, field IField, cid int64) (string, error) {
	typ, err := companyValueType(d, field)
	if err != nil {
		return "", err
	}
	if cid > 0 {
		return fmt.Sprintf(`(COALESCE(%s->>'%d', %s->>'0'))::%s`, ref, cid, ref, typ), nil
	}
	return fmt.Sprintf(`(%s->>'0')::%s`, ref, typ), nil
}

func companySlot(cid int64) string {
	if cid <= 0 {
		return "0"
	}
	return strconv.FormatInt(cid, 10)
}

// companyValueUnset：这个值写进来等于「这一格没有值」。
//
// 多对一的空是 nil / 0 / -1 / "" / false；其它非文本类型的空串和 false 也是空（除了布尔
// 字段，它的 false 是一个真值）。文本字段的空串照存 —— 与普通列一致。
func companyValueUnset(field IField, v any) bool {
	if v == nil {
		return true
	}
	switch field.TypeName() {
	case TYPE_M2O:
		switch x := v.(type) {
		case bool:
			return !x
		case string:
			return x == "" || x == "0" || x == "-1"
		}
		return utils.ToInt64(v) <= 0
	case Bool, "boolean":
		return false
	}
	if s, ok := v.(string); ok && s == "" {
		switch strings.ToUpper(field.SQLType().Name) {
		case Varchar, Char, Text, "VARCHAR", "CHAR", "TEXT":
			return false
		}
		return true
	}
	if b, ok := v.(bool); ok && !b {
		return true
	}
	return false
}

// companyJSONValue 把一个值编码成存进 jsonb 那一格的 JSON。
func companyJSONValue(v any) (json.RawMessage, error) {
	switch x := v.(type) {
	case time.Time:
		v = x.Format("2006-01-02 15:04:05.999999")
	case []byte:
		v = string(x)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("orm: company_dependent value %v is not JSON-encodable: %w", v, err)
	}
	return b, nil
}

// companySetClause 渲染 UPDATE 里这一列的 SET：只改当前公司那一格。
// col 是已经引用好的列名。返回的片段里至多一个 ? 占位符。
func companySetClause(col string, field IField, cid int64, v any) (string, []any, error) {
	slot := companySlot(cid)
	if companyValueUnset(field, v) {
		return fmt.Sprintf(`%s = COALESCE(%s, '{}'::jsonb) - '%s'`, col, col, slot), nil, nil
	}
	raw, err := companyJSONValue(v)
	if err != nil {
		return "", nil, err
	}
	return fmt.Sprintf(`%s = jsonb_set(COALESCE(%s, '{}'::jsonb), '{%s}', ?::jsonb)`, col, col, slot), []any{string(raw)}, nil
}

// companyInsertValue 是新建时这一列的值：{"<当前公司>": 值}。值为空时返回 ok=false，
// 调用方不写这一列（列保持 NULL，读出来就是空）。
func companyInsertValue(field IField, cid int64, v any) (string, bool, error) {
	if companyValueUnset(field, v) {
		return "", false, nil
	}
	raw, err := companyJSONValue(v)
	if err != nil {
		return "", false, err
	}
	b, _ := json.Marshal(map[string]json.RawMessage{companySlot(cid): raw})
	return string(b), true, nil
}

// companyLeafToSql 编译一枚以按公司区分字段为左值的条件。语义与普通列一致：
// = False 是「没有值」，!= / not in 包含没有值的行，like/ilike 自动包 %。
func companyLeafToSql(d IDialect, aliasTable string, field IField, cid int64, operator string, vals []any) (string, []any, error) {
	col := aliasTable + "." + d.Quoter().Quote(field.Name())
	expr, err := companyValueExpr(d, col, field, cid)
	if err != nil {
		return "", nil, err
	}
	vals = flattenLeafValues(vals)
	falsy := len(vals) == 0 || (len(vals) == 1 && (vals[0] == nil || vals[0] == false))
	isBool := field.TypeName() == Bool || field.TypeName() == "boolean"

	switch operator {
	case "=", "==":
		if falsy {
			if isBool {
				return fmt.Sprintf(`(%s IS NULL OR %s = false)`, expr, expr), nil, nil
			}
			return fmt.Sprintf(`(%s IS NULL)`, expr), nil, nil
		}
		return fmt.Sprintf(`(%s = ?)`, expr), []any{vals[0]}, nil
	case "!=", "<>":
		if falsy {
			if isBool {
				return fmt.Sprintf(`(%s = true)`, expr), nil, nil
			}
			return fmt.Sprintf(`(%s IS NOT NULL)`, expr), nil, nil
		}
		return fmt.Sprintf(`((%s != ?) OR %s IS NULL)`, expr, expr), []any{vals[0]}, nil
	case "<", "<=", ">", ">=":
		if falsy {
			return "", nil, fmt.Errorf("orm: %s %s False is not a valid condition", field.Name(), operator)
		}
		return fmt.Sprintf(`(%s %s ?)`, expr, operator), []any{vals[0]}, nil
	case "in", "not in":
		var ps []any
		hasNull := false
		for _, v := range vals {
			if v == nil || v == false {
				hasNull = true
				continue
			}
			ps = append(ps, v)
		}
		var parts []string
		if len(ps) > 0 {
			ph := strings.TrimSuffix(strings.Repeat("?,", len(ps)), ",")
			if operator == "in" {
				parts = append(parts, fmt.Sprintf(`%s IN (%s)`, expr, ph))
			} else {
				parts = append(parts, fmt.Sprintf(`%s NOT IN (%s)`, expr, ph))
			}
		}
		if operator == "in" {
			if hasNull {
				parts = append(parts, expr+" IS NULL")
			}
			if len(parts) == 0 {
				return "FALSE", nil, nil
			}
			return "(" + strings.Join(parts, " OR ") + ")", ps, nil
		}
		if len(parts) == 0 { // not in 一个空集合 / 只有 False
			if hasNull {
				return fmt.Sprintf(`(%s IS NOT NULL)`, expr), nil, nil
			}
			return "TRUE", nil, nil
		}
		if hasNull {
			return fmt.Sprintf(`(%s AND %s IS NOT NULL)`, parts[0], expr), ps, nil
		}
		return fmt.Sprintf(`(%s OR %s IS NULL)`, parts[0], expr), ps, nil
	case "like", "ilike", "not like", "not ilike", "=like", "=ilike":
		sqlOp := strings.TrimPrefix(operator, "=")
		v := ""
		if len(vals) > 0 {
			v = utils.ToString(vals[0])
		}
		if !strings.HasPrefix(operator, "=") {
			v = "%" + v + "%"
		}
		return d.LikeClause("("+expr+")::text", sqlOp), []any{v}, nil
	}
	return "", nil, fmt.Errorf("orm: operator %q is not supported on company_dependent field %s", operator, field.Name())
}

// flattenLeafValues：in/not in 的值可能以「一个切片」的形式整个作为一个参数传进来。
func flattenLeafValues(vals []any) []any {
	if len(vals) != 1 || vals[0] == nil {
		return vals
	}
	rv := reflect.ValueOf(vals[0])
	if rv.Kind() != reflect.Slice || rv.Type().Elem().Kind() == reflect.Uint8 {
		return vals
	}
	out := make([]any, rv.Len())
	for i := range out {
		out[i] = rv.Index(i).Interface()
	}
	return out
}
