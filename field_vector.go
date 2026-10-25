package orm

import (
	"strconv"
	"strings"

	"github.com/volts-dev/utils"
)

// TVectorField 存放定长浮点向量的字段（对标 Odoo 企业版 ai 模块的 `fields.Vector`）。
//
// 列类型是 pgvector 扩展的 `vector(N)`，N 由 tag 的 size 给：`vector(1536)`。
// 用途是语义检索——余弦距离最近的 K 行，见 TSession.NearestByCosine。
//
// # 三件必须知道的事
//
//   - **要先有扩展。** `vector` 类型来自 pgvector（`CREATE EXTENSION vector`），
//     不是 Postgres 内建。扩展没装时建表那一刻报 `type "vector" does not exist`，
//     模型注册期一切正常——所以用它的模块要在建表前自己检查并给出能照做的报错。
//   - **反查是 USER-DEFINED。** information_schema 对扩展类型只报 `USER-DEFINED`，
//     GetFields 必须改用 format_type 拿到 `vector(1536)`，否则下一次启动反查时这一列
//     落进 "unknow colType"，整个模型加载失败（见 dialect_postgres.go parsePgColumn）。
//   - **值的线上形态是文本** `[0.1,0.2,...]`。lib/pq 读回来是 []byte，写入给字符串
//     字面量即可（pgvector 的 input 函数会解析）。内存里统一用 []float32。
//
// MySQL 8.0 / SQLite 没有向量类型：MySQL 落 JSON 列、SQLite 按文本存，值都是同一个 `[a,b,c]`。
// 检索用 TSession.NearestByCosine（vector_search.go）：PG 上是 `<=>` + HNSW，别处是精确扫描，
// 结果一样。别在方言层外手写 `<=>`。
type TVectorField struct {
	TField
}

func init() {
	RegisterField(TYPE_VECTOR, newVectorField)
}

func newVectorField() IField {
	return new(TVectorField)
}

func (self *TVectorField) Init(ctx *TTagContext) {
	field := ctx.Field.Base()
	size := field.size
	if len(ctx.Params) > 0 {
		if n, err := strconv.Atoi(strings.TrimSpace(ctx.Params[0])); err == nil && n > 0 {
			size = n
		}
	}
	field.SqlType = SQLType{Vector, size, 0}
	field.typeName = TYPE_VECTOR
	field.store = true
	field.size = size
}

// onConvertToRead 把 `[0.1,0.2]` 文本解成 []float32；解不动就原样返回。
func (self *TVectorField) onConvertToRead(session *TSession, cols []string, record []any, colIndex int) any {
	value := *record[colIndex].(*any)
	return ParseVector(value)
}

// onConvertToWrite 把 []float32 / []float64 编成 pgvector 的文本字面量。
func (self *TVectorField) onConvertToWrite(session *TSession, value any) any {
	return FormatVector(value)
}

// ParseVector 把驱动给回来的 `[a,b,c]`（[]byte 或 string）解成 []float32。
// 已经是切片就原样返回；nil / 空串得 nil。
func ParseVector(value any) any {
	var s string
	switch v := value.(type) {
	case nil:
		return nil
	case []float32:
		return v
	case []byte:
		s = string(v)
	case string:
		s = v
	default:
		return value
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")
	if strings.TrimSpace(s) == "" {
		return []float32{}
	}
	parts := strings.Split(s, ",")
	out := make([]float32, 0, len(parts))
	for _, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			log.Warnf("vector field decode failed, left as raw: %s", err.Error())
			return value
		}
		out = append(out, float32(f))
	}
	return out
}

// FormatVector 把向量编成 pgvector 的输入字面量 `[a,b,c]`。
//
// 空向量落 NULL：pgvector 拒绝零维（`vector must have at least 1 dimension`），
// 而"还没算出向量"本来就该是 NULL。
func FormatVector(value any) any {
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return v
	case []byte:
		if len(v) == 0 {
			return nil
		}
		return string(v)
	case []float32:
		if len(v) == 0 {
			return nil
		}
		var b strings.Builder
		b.Grow(len(v) * 10)
		b.WriteByte('[')
		for i, f := range v {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(strconv.FormatFloat(float64(f), 'g', -1, 32))
		}
		b.WriteByte(']')
		return b.String()
	case []float64:
		if len(v) == 0 {
			return nil
		}
		var b strings.Builder
		b.Grow(len(v) * 10)
		b.WriteByte('[')
		for i, f := range v {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(strconv.FormatFloat(f, 'g', -1, 32))
		}
		b.WriteByte(']')
		return b.String()
	case []any:
		if len(v) == 0 {
			return nil
		}
		fs := make([]float64, len(v))
		for i, x := range v {
			switch n := x.(type) {
			case float64:
				fs[i] = n
			case float32:
				fs[i] = float64(n)
			case int:
				fs[i] = float64(n)
			case int64:
				fs[i] = float64(n)
			default:
				fs[i] = utils.ToFloat64(x)
			}
		}
		return FormatVector(fs)
	}
	return value
}
