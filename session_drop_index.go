package orm

import "fmt"

// DropIndexIfExists 删掉 schema.table 上名叫 name 的索引（没有就什么都不做）。只看库里现在的索引，不需要模型里有它
// ——要删的往往是旧版本建的、模型里已经没有的索引。
//
//   - PG / SQLite：DROP INDEX IF EXISTS（PG 带 schema 限定）。
//   - MySQL 8.0 的 DROP INDEX 没有 IF EXISTS、还要 ON 表名：先查 information_schema 再删（2026-10-26：模块里手写 PG
//     那一句，MySQL 上每次启动报一条语法错）。
//
// schema 为空 = 连接当前的 schema（PG）/ 库（MySQL）。
func (self *TSession) DropIndexIfExists(schema, table, name string) error {
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
	case MYSQL:
		sqlStr, args := d.IndexCheckSql(schema, table, name)
		ds, err := self.Query(sqlStr, args...)
		if err != nil {
			return err
		}
		if ds == nil || ds.Count() == 0 {
			return nil
		}
		_, err = self.Exec(fmt.Sprintf("DROP INDEX %s ON %s", q.Quote(name), refTable(d, schema, table)))
		return err
	case POSTGRES:
		ref := q.Quote(name)
		if schema != "" {
			ref = q.Quote(schema) + "." + ref
		}
		_, err := self.Exec("DROP INDEX IF EXISTS " + ref)
		return err
	}
	_, err := self.Exec("DROP INDEX IF EXISTS " + q.Quote(name))
	return err
}
