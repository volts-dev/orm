package orm

import (
	"fmt"
	"strings"
)

// DropNotNull 放开 schema.table.column 的 NOT NULL。只看库里现在的列，不需要模型里有这个字段——要放开的往往
// 正是模型里已经没有的旧列（SyncModel 只加不减，旧版本留下的 NOT NULL 会让新数据插不进去）。
//
//   - PG：ALTER TABLE … ALTER COLUMN … DROP NOT NULL。
//   - MySQL 没有单独放开约束的写法，MODIFY 要重写整列：类型、排序规则、默认值、注释从 information_schema 取了
//     原样带上，只把 NOT NULL 换成 NULL（2026-10-26：模块里手写 PG 那一句，MySQL 上装 delivery 整个失败）。
//   - SQLite 改不了列约束：什么都不做。
//
// 列不存在、本来就可空：什么都不做。schema 为空 = 连接当前的 schema（PG）/ 库（MySQL）。
func (self *TSession) DropNotNull(schema, table, column string) error {
	if self.IsAutoClose {
		self.IsAutoClose = false
		defer func() {
			self.IsAutoClose = true
			self.Close()
		}()
	}
	d := self.orm.dialect
	q := d.Quoter()
	switch d.DBType() {
	case POSTGRES:
		ds, err := self.Query(`SELECT is_nullable AS n FROM information_schema.columns
			WHERE table_schema = COALESCE(NULLIF(?, ''), current_schema()) AND table_name = ? AND column_name = ?`, schema, table, column)
		if err != nil {
			return err
		}
		if ds == nil || ds.Count() == 0 || ds.FieldByName("n").AsString() != "NO" {
			return nil
		}
		_, err = self.Exec(fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL", refTable(d, schema, table), q.Quote(column)))
		return err
	case MYSQL:
		ds, err := self.Query(`SELECT COLUMN_TYPE AS t, IS_NULLABLE AS n, COLUMN_DEFAULT AS d, (COLUMN_DEFAULT IS NULL) AS no_default,
				EXTRA AS x, COALESCE(COLLATION_NAME, '') AS c, COLUMN_COMMENT AS m
			FROM information_schema.COLUMNS
			WHERE TABLE_SCHEMA = COALESCE(NULLIF(?, ''), DATABASE()) AND TABLE_NAME = ? AND COLUMN_NAME = ?`, schema, table, column)
		if err != nil {
			return err
		}
		if ds == nil || ds.Count() == 0 || ds.FieldByName("n").AsString() != "NO" {
			return nil
		}
		r := ds.Record()
		extra := strings.ToLower(r.FieldByName("x").AsString())
		if strings.Contains(extra, "auto_increment") || strings.Contains(extra, "generated") && !strings.Contains(extra, "default_generated") {
			return fmt.Errorf("orm: DropNotNull %s.%s.%s: 自增 / 生成列不放开（EXTRA=%s）", schema, table, column, extra)
		}
		def := ""
		if !r.FieldByName("no_default").AsBoolean() {
			v := r.FieldByName("d").AsString()
			switch {
			case !strings.Contains(extra, "default_generated"):
				def = " DEFAULT '" + strings.ReplaceAll(strings.ReplaceAll(v, `\`, `\\`), "'", "''") + "'"
			case strings.HasPrefix(strings.ToUpper(v), "CURRENT_TIMESTAMP"):
				def = " DEFAULT " + v
			default:
				def = " DEFAULT (" + v + ")"
			}
		}
		coll := ""
		if c := r.FieldByName("c").AsString(); c != "" {
			coll = " COLLATE " + c
		}
		comment := ""
		if m := r.FieldByName("m").AsString(); m != "" {
			comment = " COMMENT '" + strings.ReplaceAll(strings.ReplaceAll(m, `\`, `\\`), "'", "''") + "'"
		}
		_, err = self.Exec(fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s %s%s NULL%s%s",
			refTable(d, schema, table), q.Quote(column), r.FieldByName("t").AsString(), coll, def, comment))
		return err
	}
	return nil
}
