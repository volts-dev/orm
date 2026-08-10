package orm

import (
	"encoding/json"

	"github.com/volts-dev/utils"
)

// TJsonField 存放 JSON 文档的字段。
//
// 列类型按方言分派：Postgres 落 **jsonb 真列**（可建 GIN 索引、可用 `@>` 把筛选下推到
// SQL），MySQL 退化到 JSON，SQLite 退化到 TEXT。后两者只保证"能存能读"，下推能力没有。
//
// ★ 为什么不复用 varchar/text 存 JSON 字符串：`SqlTypes[Jsonb]` 早就是 TEXT_TYPE
// （那是**类别**，用于判断值该按文本还是二进制搬运），但没有任何方言真的发出过 JSONB
// 这个列类型，也没有任何字段类型注册过 json——声明 `jsonb()` 的字段过去会在 NewField
// 里报 "could not create this new field"。文本列存 JSON 的代价不是"慢一点"，而是
// **筛选整个不存在**：没有 GIN 索引，domain 也无从下推。
type TJsonField struct {
	TField
}

func init() {
	RegisterField("json", newJsonField)
	RegisterField("jsonb", newJsonField)
}

func newJsonField() IField {
	return new(TJsonField)
}

func (self *TJsonField) Init(ctx *TTagContext) {
	field := ctx.Field.Base()
	field.SqlType = SQLType{Jsonb, 0, 0}
	field.typeName = TYPE_JSONB
	field.store = true
	field.IsJSON = true
	// ★ size 必须留 0。GetSqlType 末尾会把 size>0 拼成 `JSONB(255)`，Postgres 直接
	// 语法错——而这个错要到建表那一刻才出现，模型注册期一切正常。
	field.size = 0
}

// onConvertToRead 把驱动给回来的原始值解成 Go 结构（map / slice）。
//
// lib/pq 对 jsonb 列给的是 []byte，对 text 列给的是 string；调用方（前端 JSON 序列化、
// 模板渲染）需要的是结构，不是一坨字节。解不动就原样返回：一个坏值不该让整行读失败。
func (self *TJsonField) onConvertToRead(session *TSession, cols []string, record []any, colIndex int) any {
	value := *record[colIndex].(*any)
	return decodeJsonValue(value)
}

// onConvertToWrite 把内存里的结构序列化成 JSON 文本。
//
// 传字符串时不再包一层——调用方给的就是 JSON 文本（比如前端直接发上来的），
// 再 Marshal 一次会得到一个被转义的**字符串字面量**，jsonb 列里存进去的是
// `"{\"a\":1}"` 而不是对象，读出来永远解不成 map。
func (self *TJsonField) onConvertToWrite(session *TSession, value any) any {
	return encodeJsonValue(value)
}

// decodeJsonValue 把 []byte / string 形态的 JSON 解成 Go 值；其余类型原样返回。
func decodeJsonValue(value any) any {
	var raw []byte
	switch v := value.(type) {
	case nil:
		return nil
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		// 已经是结构（写入后回读缓存、或测试直接塞的 map）就不用动。
		return value
	}

	if len(raw) == 0 {
		return nil
	}

	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		log.Warnf("json field decode failed, left as raw: %s", err.Error())
		return value
	}
	return out
}

// encodeJsonValue 把任意值编码成可直接交给驱动的 JSON 文本。
func encodeJsonValue(value any) any {
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		// 空串在 jsonb 列上是**非法**的（不是 JSON 文档），必须落成 NULL，
		// 否则 INSERT 直接报 `invalid input syntax for type json`。
		if v == "" {
			return nil
		}
		return v
	case []byte:
		if len(v) == 0 {
			return nil
		}
		return string(v)
	}

	if utils.IsBlank(value) {
		return nil
	}

	buf, err := json.Marshal(value)
	if err != nil {
		log.Errf("json field encode failed: %s", err.Error())
		return nil
	}
	return string(buf)
}
