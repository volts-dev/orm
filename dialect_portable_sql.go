package orm

import (
	"strings"
)

// PG 写法在 MySQL 上没有的几种语句形状（MySQL 兼容，2026-10-26）。2026-10-26 在 MySQL 8.0.46 / 8.4.13 / 9.7.4
// 上逐条试过：UPDATE … FROM、DELETE … USING、IS [NOT] DISTINCT FROM、ON CONFLICT、string_agg 三个版本都是语法错
// 或没有这个函数，所以不能等「换个新版本就好了」。调用方给零件，方言拼成本库认的写法：
//
//	PG / SQLite                                   MySQL
//	UPDATE t x SET c = e FROM src WHERE w          UPDATE t x, src SET x.c = e WHERE w
//	DELETE FROM t x USING src WHERE w              DELETE x FROM t x, src WHERE w          （SQLite：WHERE EXISTS）
//	a IS DISTINCT FROM b                           NOT (a <=> b)                           （SQLite：a IS NOT b）
//	… ON CONFLICT DO NOTHING                       … ON DUPLICATE KEY UPDATE t.c = t.c
//	string_agg(e::text, ',')                       GROUP_CONCAT(e SEPARATOR ',')           （SQLite：group_concat）
//
// 所有片段都原样拼进语句：限定、引号由调用方给，**别把外部输入拼进来**，值走 ? 参数。

// UpdateFrom 是「照另一张表（或子查询）改这张表的行」。
//
// ★ MySQL 的多表 UPDATE 不保证 SET 的求值顺序，而单表 UPDATE 是从左到右、后面的表达式读到前面刚改过的列
// （PG 一律读旧值）——Set 里的表达式别读本条语句改的列。
type UpdateFrom struct {
	Table string    // 要改的表（限定、引号由调用方给）
	Alias string    // 它的别名，必填：Set 的表达式与 Where 里用它指这张表
	Set   []SetExpr // 列 = 表达式
	From  string    // 来源：表 + 别名、(子查询) 别名，或用 JOIN 连起来的几张
	Where string    // 连接条件与过滤，可空
}

// SetExpr 是 SET 的一项：Column 是本表的列名（不带别名、不加引号，方言负责），Expr 是值表达式。
type SetExpr struct {
	Column, Expr string
}

// DeleteUsing 是「删掉这张表里与来源对得上的行」。
type DeleteUsing struct {
	Table string // 要删的表
	Alias string // 必填
	Using string // 来源：表 + 别名、(子查询) 别名
	Where string // 连接条件与过滤
}

func (db *TDialect) UpdateFromSql(u UpdateFrom) string {
	q := db.dialect.Quoter()
	var b strings.Builder
	b.WriteString("UPDATE " + u.Table + " " + u.Alias + " SET ")
	for i, s := range u.Set {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(q.Quote(s.Column) + " = " + s.Expr) // PG 的 SET 列不许带别名
	}
	b.WriteString(" FROM " + u.From)
	if strings.TrimSpace(u.Where) != "" {
		b.WriteString(" WHERE " + u.Where)
	}
	return b.String()
}

func (db *TDialect) DeleteUsingSql(d DeleteUsing) string {
	s := "DELETE FROM " + d.Table + " " + d.Alias + " USING " + d.Using
	if strings.TrimSpace(d.Where) != "" {
		s += " WHERE " + d.Where
	}
	return s
}

func (db *TDialect) IsDistinctSql(a, b string) string {
	return "(" + a + " IS DISTINCT FROM " + b + ")"
}

func (db *TDialect) IsNotDistinctSql(a, b string) string {
	return "(" + a + " IS NOT DISTINCT FROM " + b + ")"
}

func (db *TDialect) OnConflictDoNothingSql(table, column string) string {
	return " ON CONFLICT DO NOTHING"
}

func (db *TDialect) StringAggSql(expr, sep string, distinct bool) string {
	d := ""
	if distinct {
		d = "DISTINCT "
	}
	return "string_agg(" + d + "CAST((" + expr + ") AS TEXT), " + sqlStringLiteral(sep) + ")"
}

// —— SQLite ——————————————————————————————————————————————————————————————————————————

// SQLite 的 UPDATE 别名要写 AS；UPDATE … FROM 从 3.33 起有。
func (db *sqlite) UpdateFromSql(u UpdateFrom) string {
	u.Alias = "AS " + u.Alias
	s := db.TDialect.UpdateFromSql(u)
	return s
}

// SQLite 没有 DELETE … USING：改成 EXISTS（子查询里用别名指回本表——SQLite 3.x 的 DELETE 认别名）。
func (db *sqlite) DeleteUsingSql(d DeleteUsing) string {
	w := "1 = 1"
	if strings.TrimSpace(d.Where) != "" {
		w = d.Where
	}
	return "DELETE FROM " + d.Table + " AS " + d.Alias + " WHERE EXISTS (SELECT 1 FROM " + d.Using + " WHERE " + w + ")"
}

func (db *sqlite) IsDistinctSql(a, b string) string    { return "(" + a + " IS NOT " + b + ")" }
func (db *sqlite) IsNotDistinctSql(a, b string) string { return "(" + a + " IS " + b + ")" }

func (db *sqlite) StringAggSql(expr, sep string, distinct bool) string {
	if distinct { // SQLite 的 group_concat(DISTINCT x) 不能带分隔符
		return "replace(group_concat(DISTINCT CAST((" + expr + ") AS TEXT)), ',', " + sqlStringLiteral(sep) + ")"
	}
	return "group_concat(CAST((" + expr + ") AS TEXT), " + sqlStringLiteral(sep) + ")"
}

// —— MySQL ———————————————————————————————————————————————————————————————————————————

func (db *mysql) UpdateFromSql(u UpdateFrom) string {
	var b strings.Builder
	b.WriteString("UPDATE " + u.Table + " " + u.Alias + ", " + u.From + " SET ")
	for i, s := range u.Set {
		if i > 0 {
			b.WriteString(", ")
		}
		// 多表 UPDATE 里两边同名的列不带别名是 1052（Column is ambiguous）。
		b.WriteString(u.Alias + "." + db.quoter.Quote(s.Column) + " = " + s.Expr)
	}
	if strings.TrimSpace(u.Where) != "" {
		b.WriteString(" WHERE " + u.Where)
	}
	return b.String()
}

func (db *mysql) DeleteUsingSql(d DeleteUsing) string {
	s := "DELETE " + d.Alias + " FROM " + d.Table + " " + d.Alias + ", " + d.Using
	if strings.TrimSpace(d.Where) != "" {
		s += " WHERE " + d.Where
	}
	return s
}

// <=> 是 MySQL 的 NULL 安全等于：两边都是 NULL 为真、一边是 NULL 为假——正是 IS NOT DISTINCT FROM。
func (db *mysql) IsDistinctSql(a, b string) string    { return "(NOT (" + a + " <=> " + b + "))" }
func (db *mysql) IsNotDistinctSql(a, b string) string { return "(" + a + " <=> " + b + ")" }

// ON DUPLICATE KEY UPDATE 必须改点什么：把一列赋给它自己。列要带表名——INSERT … SELECT 的来源里有同名列时，
// 不带就是 1052。与 ON CONFLICT DO NOTHING 一样对任何唯一键的冲突生效。table 照 INSERT INTO 后面的写法给。
func (db *mysql) OnConflictDoNothingSql(table, column string) string {
	c := table + "." + db.quoter.Quote(column)
	return " ON DUPLICATE KEY UPDATE " + c + " = " + c
}

// GROUP_CONCAT 的结果长度受 group_concat_max_len 限制（默认 1024 字节，超了**静默截断**、只给一条警告）；
// orm 的连接把它设成 16MB（datasource.go mysqlQuery）。
func (db *mysql) StringAggSql(expr, sep string, distinct bool) string {
	d := ""
	if distinct {
		d = "DISTINCT "
	}
	return "GROUP_CONCAT(" + d + "(" + expr + ") SEPARATOR " + mysqlStringLiteral(sep) + ")"
}
