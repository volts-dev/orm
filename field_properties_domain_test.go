package orm

import (
	"strings"
	"testing"
)

// 等值必须生成 `@>`——只有它能走 GIN 索引。写成 `->>` 也能出正确结果，
// 但那是把唯一一个可索引的场景变成全表扫，而且不会有人报 bug（"能用，就是慢"）。
func TestPropertyLeafToSql_EqualityUsesContainment(t *testing.T) {
	d := newPgDialectForTest(t)

	q, params, err := propertyLeafToSql(d, `"pro_tmpl"`, "product_properties", "aa", "=", []any{"丝"})
	if err != nil {
		t.Fatalf("propertyLeafToSql: %s", err)
	}
	if !strings.Contains(q, "@>") {
		t.Errorf("等值没走 @>，用不上 GIN: %s", q)
	}
	if len(params) != 1 || params[0] != `{"aa":"丝"}` {
		t.Errorf("params = %#v, want [{\"aa\":\"丝\"}]", params)
	}
}

// "不等于" 必须把「从没填过规格」的记录算进去：col 为 NULL 时 `NOT (col @> ...)`
// 求值是 NULL，那批记录会被整批漏掉。
func TestPropertyLeafToSql_NotEqualKeepsNullRows(t *testing.T) {
	d := newPgDialectForTest(t)

	q, _, err := propertyLeafToSql(d, `"pro_tmpl"`, "product_properties", "aa", "!=", []any{"丝"})
	if err != nil {
		t.Fatalf("propertyLeafToSql: %s", err)
	}
	if !strings.Contains(q, "IS NULL") {
		t.Errorf("!= 没有兜住 NULL 列，没填过规格的记录会被漏掉: %s", q)
	}
}

// 「没填」的三种形态都要认：键不存在、值是 json null、值是 false（写入侧把空归一成 false）。
func TestPropertyLeafToSql_BlankMatchesAllEmptyShapes(t *testing.T) {
	d := newPgDialectForTest(t)

	for _, v := range []any{nil, false, ""} {
		q, params, err := propertyLeafToSql(d, `"pro_tmpl"`, "product_properties", "aa", "=", []any{v})
		if err != nil {
			t.Fatalf("propertyLeafToSql(%#v): %s", v, err)
		}
		if !strings.Contains(q, "'null'::jsonb") || !strings.Contains(q, "'false'::jsonb") || !strings.Contains(q, "IS NULL") {
			t.Errorf("值 %#v 的空判断没盖全三种形态: %s", v, q)
		}
		for _, p := range params {
			if p != "aa" {
				t.Errorf("规格项名必须作为参数传，不能拼进 SQL: %#v", params)
			}
		}
	}
}

// 数值 0 是合法规格值，不能被当成"没填"——否则 ('功率','=',0) 查出来的是
// 一堆压根没填过功率的产品。与写入侧 propertyListToDict 的口子必须一致。
func TestPropertyLeafToSql_ZeroIsNotBlank(t *testing.T) {
	d := newPgDialectForTest(t)

	q, params, err := propertyLeafToSql(d, `"pro_tmpl"`, "product_properties", "aa", "=", []any{0})
	if err != nil {
		t.Fatalf("propertyLeafToSql: %s", err)
	}
	if !strings.Contains(q, "@>") {
		t.Errorf("0 被当成空值处理了: %s", q)
	}
	if params[0] != `{"aa":0}` {
		t.Errorf("params = %#v, want [{\"aa\":0}]", params)
	}
}

func TestPropertyLeafToSql_LikeWrapsWildcards(t *testing.T) {
	d := newPgDialectForTest(t)

	_, params, err := propertyLeafToSql(d, `"pro_tmpl"`, "product_properties", "aa", "ilike", []any{"丝"})
	if err != nil {
		t.Fatalf("propertyLeafToSql: %s", err)
	}
	if params[1] != "%丝%" {
		t.Errorf("ilike 没包通配符，退化成大小写不敏感的精确匹配: %#v", params)
	}

	// =ilike 是"原样"变体，不该再包一层。
	_, params, err = propertyLeafToSql(d, `"pro_tmpl"`, "product_properties", "aa", "=ilike", []any{"丝%"})
	if err != nil {
		t.Fatalf("propertyLeafToSql: %s", err)
	}
	if params[1] != "丝%" {
		t.Errorf("=ilike 被多包了一层通配符: %#v", params)
	}
}

func TestPropertyLeafToSql_UnsupportedOperator(t *testing.T) {
	d := newPgDialectForTest(t)

	if _, _, err := propertyLeafToSql(d, `"t"`, "props", "aa", "child_of", []any{1}); err == nil {
		t.Error("不支持的操作符应报错，而不是生成一条语义不明的 SQL")
	}
}

// GIN 索引的 DDL 与反查必须对得上，否则每次启动都 DROP + CREATE 一遍整个索引。
func TestGinIndexDDLAndIntrospectionRoundTrip(t *testing.T) {
	d := newPgDialectForTest(t)

	idx := newIndex("", "pro_tmpl", GinType, "product_properties")
	ddl := d.CreateIndexUniqueSql("public", "pro_tmpl", idx)
	if !strings.Contains(ddl, "USING GIN") {
		t.Fatalf("GIN 索引没发出 USING GIN: %s", ddl)
	}

	// pg_indexes.indexdef 里是小写的 using gin
	parsed, skip := parsePgIndex("pro_tmpl", idx.GetName("pro_tmpl"),
		`CREATE INDEX `+idx.GetName("pro_tmpl")+` ON public.pro_tmpl USING gin (product_properties)`)
	if skip {
		t.Fatal("GIN 索引被当成主键索引跳过了")
	}
	if parsed.Type != GinType {
		t.Errorf("反查出来的索引类型 = %d, want GinType(%d)——类型对不上就会每次启动重建", parsed.Type, GinType)
	}
	if !parsed.Equal(idx) {
		t.Errorf("反查索引与声明不等价: %+v vs %+v", parsed, idx)
	}
}

// jsonb 列必须落 JSONB，且不能带长度后缀——`JSONB(255)` 是语法错，
// 而这个错要到建表那一刻才暴露。
func TestPgJsonbColumnType(t *testing.T) {
	d := newPgDialectForTest(t)

	field, err := NewField("product_properties", WithSQLType(SQLType{Jsonb, 0, 0}))
	if err != nil {
		t.Fatalf("NewField: %s", err)
	}
	field.Base().size = 255 // 就算别处塞了 size 进来也不能拼到类型上

	got := d.GetSqlType(field)
	if got != "JSONB" {
		t.Errorf("GetSqlType = %q, want JSONB", got)
	}
}

// 从库里反查回来的 jsonb 列要能造出字段实例；不认的话整个模型加载失败，
// 表现是"表建得出来，下次启动起不来"。
func TestNewFieldFromJsonbSqlType(t *testing.T) {
	field, err := NewField("product_properties", WithSQLType(SQLType{Jsonb, 0, 0}))
	if err != nil {
		t.Fatalf("NewField(jsonb): %s", err)
	}
	if _, ok := field.(*TJsonField); !ok {
		t.Errorf("jsonb 列造出来的是 %T, want *TJsonField", field)
	}
}
