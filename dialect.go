package orm

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/volts-dev/orm/core"
	"github.com/volts-dev/orm/dialect"
)

type (
	DbType string

	// a dialect is a driver's wrapper
	IDialect interface {
		String() string
		Init(core.Queryer, *TDataSource) error
		DataSource() *TDataSource
		//DB() *sql.DB
		Version(context.Context) (*core.Version, error)
		DBType() string
		SyncToSqlType(ctx *TTagContext) // 同步与数据库同步
		GetSqlType(IField) string
		FormatBytes(b []byte) string

		DriverName() string
		DataSourceName() string
		Quoter() dialect.Quoter
		IsReserved(string) bool
		AndStr() string
		OrStr() string
		EqStr() string
		RollBackStr() string
		AutoIncrStr() string

		//SupportInsertMany() bool
		//SupportEngine() bool
		//SupportCharset() bool
		SupportDropIfExists() bool
		ShowCreateNull() bool

		IndexCheckSql(tableName, idxName string) (string, []interface{})
		TableCheckSql(tableName string) (string, []interface{})
		CreateTableSql(table IModel, storeEngine, charset string) string
		DropTableSql(tableName string) string
		CreateIndexUniqueSql(tableName string, index *TIndex) string
		DropIndexUniqueSql(tableName string, index *TIndex) string
		ModifyColumnSql(tableName string, col IField) string
		ForUpdateSql(query string) string
		GenInsertSql(model string, fields, uniqueFields []string, idField string, onConflict *OnConflict) (sql string)
		GenAddColumnSQL(tableName string, field IField) string
		IsColumnExist(ctx context.Context, tableName string, colName string) (bool, error)
		IsDatabaseExist(ctx context.Context, name string) bool
		CreateDatabase(db *sql.DB, ctx context.Context, name string) error
		DropDatabase(db *sql.DB, ctx context.Context, name string) error

		//CreateTableIfNotExists(table *Table, tableName, storeEngine, charset string) error
		//MustDropTable(tableName string) error

		GetFields(ctx context.Context, tableName string) ([]string, map[string]IField, error)
		GetModels(ctx context.Context) ([]IModel, error)
		GetIndexes(ctx context.Context, tableName string) (map[string]*TIndex, error)

		Fmter() []IFmter // TODO 考虑移除 由于无法满足query获得model对象
		SetParams(params map[string]string)
	}

	TDialect struct {
		*TDataSource
		//db      *sql.DB
		dialect IDialect
		queryer core.Queryer
		quoter  dialect.Quoter
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

func (b *TDialect) _DB() core.Queryer {
	return b.queryer
}

func (b *TDialect) Init(queryer core.Queryer, dialect IDialect, datasource *TDataSource) error {
	b.queryer, b.dialect, b.TDataSource = queryer, dialect, datasource
	return nil
}

func (b *TDialect) DataSource() *TDataSource {
	return b.TDataSource
}

func (b *TDialect) DBType() string {
	return b.TDataSource.DbType
}

func (db *TDialect) SyncToSqlType(ctx *TTagContext) {

}

func (b *TDialect) FormatBytes(bs []byte) string {
	return fmt.Sprintf("0x%x", bs)
}

func (b *TDialect) Quoter() dialect.Quoter {
	return b.quoter
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
	quoter := db.dialect.Quoter()
	return fmt.Sprintf("DROP TABLE IF EXISTS %s", quoter.Quote(tableName))
}

func (db *TDialect) HasRecords(ctx context.Context, query string, args ...interface{}) (bool, error) {
	//db.LogSQL(query, args)
	rows, err := db.queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	if rows.Next() {
		return true, nil
	}
	return false, nil
}

func (db *TDialect) IsColumnExist(ctx context.Context, tableName, colName string) (bool, error) {
	query := "SELECT `COLUMN_NAME` FROM `INFORMATION_SCHEMA`.`COLUMNS` WHERE `TABLE_SCHEMA` = ? AND `TABLE_NAME` = ? AND `COLUMN_NAME` = ?"
	query = strings.Replace(query, "`", string(db.dialect.Quoter().Prefix), -1)
	return db.HasRecords(ctx, query, db.DbName, tableName, colName)
}

/*
func (db *TDialect) CreateTableIfNotExists(table *Table, tableName, storeEngine, charset string) error {
	sql, args := db.dialect.TableCheckSql(tableName)
	rows, err := db.queryer.QueryContext(sql, args...)
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

func (db *TDialect) CreateDatabase(dbx *sql.DB, ctx context.Context, tableName string) error {
	return nil

}

func (db *TDialect) DropDatabase(dbx *sql.DB, ctx context.Context, tableName string) error {
	return nil

}

func (db *TDialect) CreateIndexUniqueSql(tableName string, index *TIndex) string {
	quoter := db.dialect.Quoter().Quote
	var unique string
	var idxName string
	if index.Type == UniqueType {
		unique = " UNIQUE"
	}
	idxName = index.GetName(tableName)
	return fmt.Sprintf("CREATE%s INDEX %v ON %v (%v)", unique,
		quoter(idxName), quoter(tableName),
		quoter(strings.Join(index.Cols, quoter(","))))
}

func (db *TDialect) DropIndexUniqueSql(tableName string, index *TIndex) string {
	quoter := db.dialect.Quoter().Quote
	var name string
	if index.IsRegular {
		name = index.GetName(tableName)
	} else {
		name = index.Name
	}
	return fmt.Sprintf("DROP INDEX %v ON %s", quoter(name), quoter(tableName))
}

func (db *TDialect) ModifyColumnSql(tableName string, col IField) string {
	s, err := ColumnString(db.dialect, col, false)
	if err != nil {
		log.Warn(err)
	}
	return fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s", db.quoter.Quote(tableName), s)
}

func (self *TDialect) CreateTableSql(model IModel, storeEngine, charset string) string {
	quoter := self.dialect.Quoter()
	var b strings.Builder
	b.WriteString("CREATE TABLE IF NOT EXISTS ")
	quoter.QuoteTo(&b, fmtTableName(model.String()))
	b.WriteString(" (")

	fields := model.GetFields()
	lastIdx := len(fields)
	fieldCnt := 0 /* 用于确保第一个Field之前不会添加逗号 */
	for idx, field := range fields {
		// TODO调试 store 失效原因
		if !field.Store() {
			continue
		}

		if fieldCnt != 0 && idx < lastIdx {
			b.WriteString(", ")
		}

		s, _ := ColumnString(self.dialect, field, field.IsPrimaryKey() && len(model.GetPrimaryKeys()) == 1)
		b.WriteString(s)

		fieldCnt++
	}

	if len(model.GetPrimaryKeys()) > 1 {
		b.WriteString(", PRIMARY KEY (")
		b.WriteString(quoter.Join(model.GetPrimaryKeys(), ","))
		b.WriteString(")")
	}
	b.WriteString(")")

	if len(charset) == 0 {
		charset = self.Charset
	}
	if len(charset) != 0 {
		b.WriteString(" DEFAULT CHARSET ")
		b.WriteString(charset)
	}

	return b.String()
}

func (b *TDialect) ForUpdateSql(query string) string {
	return query + " FOR UPDATE"
}

// 生成插入SQL句子
func (db *TDialect) GenInsertSql(tableName string, fields []string, uniqueFields []string, idField string, onConflict *OnConflict) string {
	var sql strings.Builder
	cnt := len(fields)
	field_places := strings.Repeat("?,", cnt-1) + "?"
	field_list := ""

	for i, name := range fields {
		if i < cnt-1 {
			field_list = field_list + db.quoter.Quote(name) + ","
		} else {
			field_list = field_list + db.quoter.Quote(name)
		}
	}

	sql.WriteString("INSERT INTO ")
	sql.WriteString(tableName)
	sql.WriteString(" (")
	sql.WriteString(field_list)
	sql.WriteString(") ")
	sql.WriteString("VALUES (")
	sql.WriteString(field_places)
	sql.WriteString(") ")

	if len(idField) > 0 {
		sql.WriteString("RETURNING ")
		sql.WriteString(db.quoter.Quote(idField))
	}

	return sql.String()
}

func (db *TDialect) TableCheckSql(tableName string) (string, []interface{}) {
	args := []interface{}{tableName}
	return `SELECT 1 FROM $1 LIMIT 1`, args
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

// ColumnString generate column description string according dialect
func ColumnString(dialect IDialect, field IField, includePrimaryKey bool) (string, error) {
	bd := strings.Builder{}
	quoter := dialect.Quoter()
	if err := quoter.QuoteTo(&bd, field.Name()); err != nil {
		return "", err
	}

	if err := bd.WriteByte(' '); err != nil {
		return "", err
	}

	if _, err := bd.WriteString(dialect.GetSqlType(field)); err != nil {
		return "", err
	}

	if includePrimaryKey && field.IsPrimaryKey() {
		if _, err := bd.WriteString(" PRIMARY KEY"); err != nil {
			return "", err
		}
		if field.IsAutoIncrement() {
			if err := bd.WriteByte(' '); err != nil {
				return "", err
			}
			if _, err := bd.WriteString(dialect.AutoIncrStr()); err != nil {
				return "", err
			}
		}
	}

	if !field.IsDefaultEmpty() {
		if _, err := bd.WriteString(" DEFAULT "); err != nil {
			return "", err
		}

		dv := field.Base()._attr_default
		if dv == "" {
			if _, err := bd.WriteString("''"); err != nil {
				return "", err
			}
		} else {
			if field.SQLType().IsText() {
				bd.WriteByte('\'')
				bd.WriteString(dv)
				bd.WriteByte('\'')
				//dv = quoter.Quote(dv)
			} else {
				if _, err := bd.WriteString(dv); err != nil {
					return "", err
				}
			}
		}

		/*
			dv := utils.ToString(field.Base()._attr_default)
			if dv == "" {
				if _, err := bd.WriteString("''"); err != nil {
					return "", err
				}
			} else {
				///if field.SQLType().IsText() {
				//	dv = quoter.Quote(dv)
				//}
				if _, err := bd.WriteString(dv); err != nil {
					return "", err
				}
			}
		*/
	}

	if !field.Required() {
		if _, err := bd.WriteString(" NULL"); err != nil {
			return "", err
		}
	} else {
		if _, err := bd.WriteString(" NOT NULL"); err != nil {
			return "", err
		}
	}

	return bd.String(), nil
}
