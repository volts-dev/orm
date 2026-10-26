package orm

import (
	"fmt"
	"strings"
)

// 按系统目录读列、按列类型取文本（MySQL 兼容，跨库搬数据，2026-10-25）。
//
// 给不经模型、按库里「实际有哪些表、哪些列」干活的代码用（vectors 的租户归档 / 硬删）。两件事两家写法不同：
//
//   - 列类型：PG 的 information_schema 有 udt_name（int8 / varchar / jsonb / bytea…），MySQL 没有，它的布尔是
//     tinyint(1)、只在 column_type 里看得出来。CatalogColumns 把两家都归一成 **PG udt_name 的口径**——这是
//     归档 header 里一直记着的写法，老归档、新归档、两家导出的归档读起来是同一套词。
//   - 列的文本：ColumnTextSql 按上面的类型给出「这一列的文本」的表达式：PG 的 ::text；MySQL 的 CAST AS CHAR，
//     布尔改写成 'true' / 'false'；二进制两家都给十六进制（MySQL CAST 二进制出来的是原始字节，不是文本）。

// CatalogColumn 是系统目录里一张基表的一列。Type 是归一后的类型名（PG udt_name 口径），见 CatalogColumns。
type CatalogColumn struct {
	Schema, Table, Column, Type string
}

// catalogDialect：方言怎么列出目录里的列、怎么取一列的文本。
type catalogDialect interface {
	// catalogColumnsSql 列出 schemas 里全部基表的列：结果列 s / t / col / dt / ct（ct 只有 MySQL 有用），
	// 按 schema、表、列序排。
	catalogColumnsSql(schemas []string) (string, []any)
	// catalogType 把 dt / ct 归一成 PG udt_name 口径。
	catalogType(dt, ct string) string
	// columnTextSql 是 expr（类型为 typ，归一后的）的文本表达式；bytea 给十六进制。
	columnTextSql(expr, typ string) string
}

func catalogSchemaList(schemas []string) (string, []any) {
	args := make([]any, len(schemas))
	for i, s := range schemas {
		args[i] = s
	}
	return strings.TrimSuffix(strings.Repeat("?,", len(schemas)), ","), args
}

func (db *postgres) catalogColumnsSql(schemas []string) (string, []any) {
	ph, args := catalogSchemaList(schemas)
	return `SELECT c.table_schema AS s, c.table_name AS t, c.column_name AS col, c.udt_name AS dt, '' AS ct
		FROM information_schema.columns c
		JOIN information_schema.tables t ON t.table_schema = c.table_schema AND t.table_name = c.table_name
		WHERE t.table_type = 'BASE TABLE' AND c.table_schema IN (` + ph + `)
		ORDER BY c.table_schema, c.table_name, c.ordinal_position`, args
}

func (db *postgres) catalogType(dt, _ string) string { return dt }

func (db *postgres) columnTextSql(expr, typ string) string {
	if typ == "bytea" {
		return "encode(" + expr + ", 'hex')"
	}
	return "(" + expr + ")::text"
}

func (db *mysql) catalogColumnsSql(schemas []string) (string, []any) {
	ph, args := catalogSchemaList(schemas)
	// 别名小写：MySQL 8 的 information_schema 列名回大写，dataset 按名取区分大小写。
	return `SELECT c.table_schema AS s, c.table_name AS t, c.column_name AS col, c.data_type AS dt, c.column_type AS ct
		FROM information_schema.columns c
		JOIN information_schema.tables t ON t.table_schema = c.table_schema AND t.table_name = c.table_name
		WHERE t.table_type = 'BASE TABLE' AND c.table_schema IN (` + ph + `)
		ORDER BY c.table_schema, c.table_name, c.ordinal_position`, args
}

// catalogType：MySQL 的 data_type / column_type → PG udt_name。与 GetSqlType 的映射反过来（bool → tinyint(1)、
// 日期 → DATETIME(6)、json → JSON、二进制 → LONGBLOB…）。
func (db *mysql) catalogType(dt, ct string) string {
	dt, ct = strings.ToLower(dt), strings.ToLower(ct)
	switch dt {
	case "tinyint":
		if strings.HasPrefix(ct, "tinyint(1)") {
			return "bool"
		}
		return "int2"
	case "smallint", "year":
		return "int2"
	case "mediumint", "int", "integer":
		return "int4"
	case "bigint":
		return "int8"
	case "decimal", "numeric":
		return "numeric"
	case "float":
		return "float4"
	case "double", "real":
		return "float8"
	case "char":
		return "bpchar"
	case "varchar", "enum", "set":
		return "varchar"
	case "tinytext", "text", "mediumtext", "longtext":
		return "text"
	case "datetime", "timestamp":
		return "timestamp"
	case "date":
		return "date"
	case "time":
		return "time"
	case "json":
		return "jsonb"
	case "binary", "varbinary", "tinyblob", "blob", "mediumblob", "longblob":
		return "bytea"
	case "bit":
		return "bit"
	}
	return dt
}

func (db *mysql) columnTextSql(expr, typ string) string {
	switch typ {
	case "bool":
		return boolText(expr)
	case "bytea":
		return "LOWER(HEX(" + expr + "))"
	case "float4":
		// CAST(FLOAT AS CHAR) 只给 6 位有效数字（27.142857 → 27.1429）：先放宽成 DOUBLE，文本才解得回同一个 float32。
		return "CAST(CAST((" + expr + ") AS DOUBLE) AS CHAR)"
	}
	return "CAST((" + expr + ") AS CHAR)"
}

// CatalogColumns 列出 schemas 里全部基表的列（在本会话上查：事务里就是事务的快照），类型名按 PG udt_name 口径归一。
func (self *TSession) CatalogColumns(schemas ...string) ([]CatalogColumn, error) {
	d, ok := self.orm.dialect.(catalogDialect)
	if !ok {
		return nil, fmt.Errorf("orm: CatalogColumns: dialect %s is not supported", self.orm.dialect.DBType())
	}
	if len(schemas) == 0 {
		return nil, nil
	}
	q, args := d.catalogColumnsSql(schemas)
	ds, err := self.Query(q, args...)
	if err != nil {
		return nil, err
	}
	var out []CatalogColumn
	if ds == nil {
		return out, nil
	}
	for ds.First(); !ds.Eof(); ds.Next() {
		out = append(out, CatalogColumn{
			Schema: ds.FieldByName("s").AsString(),
			Table:  ds.FieldByName("t").AsString(),
			Column: ds.FieldByName("col").AsString(),
			Type:   d.catalogType(ds.FieldByName("dt").AsString(), ds.FieldByName("ct").AsString()),
		})
	}
	return out, nil
}

// ColumnTextSql 是 expr（catalogType 是 CatalogColumns 给的类型名）的文本表达式：布尔 'true' / 'false'，
// bytea 小写十六进制（不带 \x），其余是库的文本形式。方言不支持时原样返回 expr。
func (self *TOrm) ColumnTextSql(expr, catalogType string) string {
	if d, ok := self.dialect.(catalogDialect); ok {
		return d.columnTextSql(expr, catalogType)
	}
	return expr
}
