package orm

import (
	"database/sql"
	"fmt"
	"strings"
)

type (
	DbType string

	// a dialect is a driver's wrapper
	IDialect interface {
		Init(*sql.DB, *TDataSource) error
		DataSource() *TDataSource
		DB() *sql.DB
		DBType() string
		GenSqlType(IField) string
		FormatBytes(b []byte) string

		DriverName() string
		DataSourceName() string

		QuoteStr() string
		IsReserved(string) bool
		Quote(string) string
		AndStr() string
		OrStr() string
		EqStr() string
		RollBackStr() string
		AutoIncrStr() string

		SupportInsertMany() bool
		SupportEngine() bool
		SupportCharset() bool
		SupportDropIfExists() bool
		IndexOnTable() bool
		ShowCreateNull() bool

		IndexCheckSql(tableName, idxName string) (string, []interface{})
		TableCheckSql(tableName string) (string, []interface{})

		IsColumnExist(tableName string, colName string) (bool, error)
		IsDatabaseExist(name string) bool
		CreateDatabase(name string) error
		DropDatabase(name string) error

		CreateTableSql(table IModel, storeEngine, charset string) string
		DropTableSql(tableName string) string
		CreateIndexSql(tableName string, index *TIndex) string
		DropIndexSql(tableName string, index *TIndex) string

		ModifyColumnSql(tableName string, col IField) string

		ForUpdateSql(query string) string

		//CreateTableIfNotExists(table *Table, tableName, storeEngine, charset string) error
		//MustDropTable(tableName string) error

		GetFields(tableName string) ([]string, map[string]IField, error)
		GetModels() ([]IModel, error)
		GetIndexes(tableName string) (map[string]*TIndex, error)

		GenInsertSql(model string, fields []string, idField string) (sql string, isquery bool)
		Fmter() []IFmter
		SetParams(params map[string]string)
	}

	TDialect struct {
		*TDataSource
		db      *sql.DB
		dialect IDialect
	}
)

var (
	dialect_creators = make(map[string]func() IDialect)
)

// RegisterDialect register database dialect
func RegisterDialect(dbName DbType, dialectFunc func() IDialect) {
	if dialectFunc == nil {
		panic("Register dialect is nil")
	}

	dialect_creators[strings.ToLower(string(dbName))] = dialectFunc // !nashtsai! allow override dialect
}

func OpenDialect(dialect IDialect) (*sql.DB, error) {
	return sql.Open(dialect.DriverName(), dialect.DataSourceName())
}

// QueryDialect query if registed database dialect
func QueryDialect(dbName string) IDialect {
	if d, ok := dialect_creators[strings.ToLower(dbName)]; ok {
		return d()
	}

	return nil
}

func (b *TDialect) DB() *sql.DB {
	return b.db
}

func (b *TDialect) Init(db *sql.DB, dialect IDialect, datasource *TDataSource) error {
	b.db, b.dialect, b.TDataSource = db, dialect, datasource
	return nil
}

func (b *TDialect) DataSource() *TDataSource {
	return b.TDataSource
}

func (b *TDialect) DBType() string {
	return b.TDataSource.DbType
}

func (b *TDialect) FormatBytes(bs []byte) string {
	return fmt.Sprintf("0x%x", bs)
}

func (b *TDialect) DriverName() string {
	return b.TDataSource.DbType
}

func (b *TDialect) ShowCreateNull() bool {
	return true
}

func (b *TDialect) DataSourceName() string {
	return b.TDataSource.toString()
}

func (b *TDialect) AndStr() string {
	return "AND"
}

func (b *TDialect) OrStr() string {
	return "OR"
}

func (b *TDialect) EqStr() string {
	return "="
}

func (db *TDialect) RollBackStr() string {
	return "ROLL BACK"
}

func (db *TDialect) SupportDropIfExists() bool {
	return true
}

func (db *TDialect) DropTableSql(tableName string) string {
	quote := db.dialect.Quote
	return fmt.Sprintf("DROP TABLE IF EXISTS %s", quote(tableName))
}

func (db *TDialect) HasRecords(query string, args ...interface{}) (bool, error) {
	//db.LogSQL(query, args)
	rows, err := db.DB().Query(query, args...)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	if rows.Next() {
		return true, nil
	}
	return false, nil
}

func (db *TDialect) IsColumnExist(tableName, colName string) (bool, error) {
	query := "SELECT `COLUMN_NAME` FROM `INFORMATION_SCHEMA`.`COLUMNS` WHERE `TABLE_SCHEMA` = ? AND `TABLE_NAME` = ? AND `COLUMN_NAME` = ?"
	query = strings.Replace(query, "`", db.dialect.QuoteStr(), -1)
	return db.HasRecords(query, db.DbName, tableName, colName)
}

/*
func (db *TDialect) CreateTableIfNotExists(table *Table, tableName, storeEngine, charset string) error {
	sql, args := db.dialect.TableCheckSql(tableName)
	rows, err := db.DB().Query(sql, args...)
	if db.Logger != nil {
		db.Logger.Info("[sql]", sql, args)
	}
	if err != nil {
		return err
	}
	defer rows.Close()

	if rows.Next() {
		return nil
	}

	sql = db.dialect.CreateTableSql(table, tableName, storeEngine, charset)
	_, err = db.DB().Exec(sql)
	if db.Logger != nil {
		db.Logger.Info("[sql]", sql)
	}
	return err
}*/

func (db *TDialect) CreateDatabase(tableName string) error {
	return nil

}

func (db *TDialect) DropDatabase(tableName string) error {
	return nil

}

func (db *TDialect) CreateIndexSql(tableName string, index *TIndex) string {
	quote := db.dialect.Quote
	var unique string
	var idxName string
	if index.Type == UniqueType {
		unique = " UNIQUE"
	}
	idxName = index.XName(tableName)
	return fmt.Sprintf("CREATE%s INDEX %v ON %v (%v)", unique,
		quote(idxName), quote(tableName),
		quote(strings.Join(index.Cols, quote(","))))
}

func (db *TDialect) DropIndexSql(tableName string, index *TIndex) string {
	quote := db.dialect.Quote
	var name string
	if index.IsRegular {
		name = index.XName(tableName)
	} else {
		name = index.Name
	}
	return fmt.Sprintf("DROP INDEX %v ON %s", quote(name), quote(tableName))
}

func (db *TDialect) ModifyColumnSql(tableName string, col IField) string {
	return fmt.Sprintf("alter table %s MODIFY COLUMN %s", tableName, col.StringNoPk(db.dialect))
}

func (b *TDialect) CreateTableSql(model IModel, storeEngine, charset string) string {
	var sql string
	sql = "CREATE TABLE IF NOT EXISTS "
	sql += b.dialect.Quote(model.GetName())
	sql += " ("

	fields := model.GetFields()
	if len(fields) > 0 {
		pkList := model.GetPrimaryKeys()

		for _, field := range fields {
			if !field.Store() {
				continue
			}

			//col := model.GetColumn(colName)
			col := field.Base()

			if col.isPrimaryKey && len(pkList) == 1 {
				sql += col.String(b.dialect)
			} else {
				sql += col.StringNoPk(b.dialect)
			}
			sql = strings.TrimSpace(sql)
			if b.DriverName() == MYSQL && len(col.Comment) > 0 {
				sql += " COMMENT '" + col.Comment + "'"
			}
			sql += ", "
		}

		if len(pkList) > 1 {
			sql += "PRIMARY KEY ( "
			sql += b.dialect.Quote(strings.Join(pkList, b.dialect.Quote(",")))
			sql += " ), "
		}

		sql = sql[:len(sql)-2]
	}

	sql += ")"

	if b.dialect.SupportEngine() && storeEngine != "" {
		sql += " ENGINE=" + storeEngine
	}
	if b.dialect.SupportCharset() {
		if len(charset) == 0 {
			charset = b.dialect.DataSource().Charset
		}
		if len(charset) > 0 {
			sql += " DEFAULT CHARSET " + charset
		}
	}

	return sql
}

func (b *TDialect) ForUpdateSql(query string) string {
	return query + " FOR UPDATE"
}

func (b *TDialect) LogSQL(sql string, args []interface{}) {
	/*	if b.logger != nil && b.logger.IsShowSQL() {
		if len(args) > 0 {
			b.logger.Infof("[SQL] %v %v", sql, args)
		} else {
			b.logger.Infof("[SQL] %v", sql)
		}
	}*/
}

func (b *TDialect) SetParams(params map[string]string) {
}
