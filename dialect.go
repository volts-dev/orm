package orm

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/volts-dev/orm/core"
	"github.com/volts-dev/orm/dialect"
	"github.com/volts-dev/utils"
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

		// DDL/元数据方法统一携带 schema（多 schema 隔离场景，如 per-tenant schema）。
		// schema 为空表示「方言默认」：postgres 落 datasource schema（public）、
		// mysql 落当前库（TABLE_SCHEMA=DbName）、sqlite 忽略。调用方（session/statement）
		// 负责传 session.Schema；方言只做通用处理，不含业务判断。
		IndexCheckSql(schema, tableName, idxName string) (string, []any)
		TableCheckSql(schema, tableName string) (string, []any)
		// CreateTableSql 从执行中的 session 取 schema（原实现经 model.Transaction()
		// 取，DDL 路径未绑定事务时读到陈旧/空值 → 建表落错 schema）。
		CreateTableSql(session *TSession, table IModel, storeEngine, charset string) string
		DropTableSql(schema, tableName string) string
		CreateIndexUniqueSql(schema, tableName string, index *TIndex) string
		DropIndexUniqueSql(schema, tableName string, index *TIndex) string
		DropColumnNotNullSql(schema, tableName string, col IField) string
		DropColumnDefaultSql(schema, tableName string, col IField) string
		ModifyColumnSql(schema, tableName string, col IField) string
		ForUpdateSql(query string) string
		GenInsertSql(model string, fields, uniqueFields []string, idField string, onConflict *OnConflict) (sql string)
		GenAddColumnSQL(schema, tableName string, field IField) string
		IsColumnExist(ctx context.Context, schema, tableName string, colName string) (bool, error)
		IsDatabaseExist(ctx context.Context, name string) bool
		CreateDatabase(db *sql.DB, ctx context.Context, name string) error
		DropDatabase(db *sql.DB, ctx context.Context, name string) error

		//CreateTableIfNotExists(table *Table, tableName, storeEngine, charset string) error
		//MustDropTable(tableName string) error

		GetFields(ctx context.Context, session *TSession, model IModel) ([]string, map[string]IField, error)
		GetModels(ctx context.Context, session *TSession) ([]IModel, error)
		GetIndexes(ctx context.Context, session *TSession, tableName string) (map[string]*TIndex, error)

		Fmter() []IFmter // TODO 考虑移除 由于无法满足query获得model对象
		SetParams(params map[string]string)
		SupportReturning() bool

		// MapError 把 driver 原生错误翻译为 errors 包定义的 sentinel
		// 各 dialect 必须实现；session 层统一调用
		MapError(err error) error
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

func (db *TDialect) Init(queryer core.Queryer, dialect IDialect, datasource *TDataSource) error {
	db.queryer, db.dialect, db.TDataSource = queryer, dialect, datasource
	return nil
}

func (db *TDialect) DataSource() *TDataSource {
	return db.TDataSource
}

func (db *TDialect) DBType() string {
	return db.TDataSource.DbType
}

func (db *TDialect) SyncToSqlType(ctx *TTagContext) {

}

func (db *TDialect) FormatBytes(bs []byte) string {
	return fmt.Sprintf("0x%x", bs)
}

func (db *TDialect) Quoter() dialect.Quoter {
	return db.quoter
}

func (db *TDialect) DriverName() string {
	return db.TDataSource.DbType
}

func (db *TDialect) ShowCreateNull() bool {
	return true
}

func (db *TDialect) DataSourceName() string {
	s, _ := db.TDataSource.toString()
	return s
}

func (db *TDialect) SupportReturning() bool {
	return false
}

func (db *TDialect) AndStr() string {
	return "AND"
}

func (db *TDialect) OrStr() string {
	return "OR"
}

func (db *TDialect) EqStr() string {
	return "="
}

func (db *TDialect) RollBackStr() string {
	return "ROLL BACK"
}

func (db *TDialect) SupportDropIfExists() bool {
	return true
}

func (db *TDialect) DropTableSql(schema, tableName string) string {
	quoter := db.dialect.Quoter()
	return fmt.Sprintf("DROP TABLE IF EXISTS %s", quoter.QuoteTable(schema, tableName))
}

func (db *TDialect) HasRecords(ctx context.Context, query string, args ...any) (bool, error) {
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

func (db *TDialect) IsColumnExist(ctx context.Context, schema, tableName, colName string) (bool, error) {
	// MySQL 语义：TABLE_SCHEMA 即数据库名；schema 为空时退回当前库。
	if schema == "" {
		schema = db.DbName
	}
	query := "SELECT `COLUMN_NAME` FROM `INFORMATION_SCHEMA`.`COLUMNS` WHERE `TABLE_SCHEMA` = ? AND `TABLE_NAME` = ? AND `COLUMN_NAME` = ?"
	query = strings.Replace(query, "`", string(db.dialect.Quoter().Prefix), -1)
	return db.HasRecords(ctx, query, schema, tableName, colName)
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

func (db *TDialect) CreateIndexUniqueSql(schema, tableName string, index *TIndex) string {
	quoter := db.dialect.Quoter()
	var unique string
	var idxName string
	if index.Type == UniqueType {
		unique = " UNIQUE"
	}
	// 索引名从裸表名派生（不带 schema）——索引与表同 schema，名字里不掺限定符。
	idxName = index.GetName(tableName)
	return fmt.Sprintf("CREATE%s INDEX %v ON %v (%v)", unique,
		quoter.Quote(idxName), quoter.QuoteTable(schema, tableName),
		quoter.Join(index.Cols, ","))
}

func (db *TDialect) DropIndexUniqueSql(schema, tableName string, index *TIndex) string {
	quoter := db.dialect.Quoter()
	var name string
	if index.IsRegular {
		name = index.GetName(tableName)
	} else {
		name = index.Name
	}
	return fmt.Sprintf("DROP INDEX %v ON %s", quoter.Quote(name), quoter.QuoteTable(schema, tableName))
}

// DropColumnNotNullSql returns SQL to align a column's NOT NULL constraint
// with the passed column definition.
//
// Default behavior (MySQL-like): re-apply full column definition.
// Dialects with dedicated ALTER COLUMN syntax (e.g. Postgres) should override.
func (db *TDialect) DropColumnNotNullSql(schema, tableName string, col IField) string {
	return db.ModifyColumnSql(schema, tableName, col)
}

// DropColumnDefaultSql returns SQL to align a column's DEFAULT clause
// with the passed column definition.
//
// Default behavior (MySQL-like): re-apply full column definition.
// Dialects with dedicated ALTER COLUMN syntax (e.g. Postgres) should override.
func (db *TDialect) DropColumnDefaultSql(schema, tableName string, col IField) string {
	return db.ModifyColumnSql(schema, tableName, col)
}

func (db *TDialect) ModifyColumnSql(schema, tableName string, col IField) string {
	s, err := ColumnString(db.dialect, col, false)
	if err != nil {
		log.Warn(err)
	}
	return fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s", db.quoter.QuoteTable(schema, tableName), s)
}

func (db *TDialect) CreateTableSql(session *TSession, model IModel, storeEngine, charset string) string {
	_ = session // 基类（MySQL 风格）无 schema 概念；postgres 覆盖实现使用 session.Schema
	quoter := db.dialect.Quoter()
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

		s, _ := ColumnString(db.dialect, field, field.IsPrimaryKey() && len(model.GetPrimaryKeys()) == 1)
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
		charset = db.Charset
	}
	if len(charset) != 0 {
		b.WriteString(" DEFAULT CHARSET ")
		b.WriteString(charset)
	}

	return b.String()
}

func (db *TDialect) ForUpdateSql(query string) string {
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

func (db *TDialect) TableCheckSql(schema, tableName string) (string, []any) {
	_ = schema // 基类占位实现；具体方言各自覆盖并处理 schema
	args := []any{tableName}
	return `SELECT 1 FROM $1 LIMIT 1`, args
}

func (db *TDialect) LogSQL(sql string, args []any) {
	/*	if db.logger != nil && db.logger.IsShowSQL() {
		if len(args) > 0 {
			db.logger.Infof("[SQL] %v %v", sql, args)
		} else {
			db.logger.Infof("[SQL] %v", sql)
		}
	}*/
}

func (db *TDialect) SetParams(params map[string]string) {
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

	// Autoincrement columns get their default from the dialect's sequence
	// (BIGSERIAL / AUTO_INCREMENT / AUTOINCREMENT). Emitting an explicit
	// DEFAULT alongside it makes Postgres reject the column definition with
	// "multiple default values specified".
	if !field.IsAutoIncrement() && !field.IsDefaultEmpty() {
		if _, err := bd.WriteString(" DEFAULT "); err != nil {
			return "", err
		}

		dvStr := utils.ToString(field.Default())
		if dvStr == "" {
			if field.SQLType().IsNumeric() {
				if _, err := bd.WriteString("0"); err != nil {
					return "", err
				}
			} else {
				if _, err := bd.WriteString("''"); err != nil {
					return "", err
				}
			}

		} else {
			if field.SQLType().IsText() {
				bd.WriteByte('\'')
				bd.WriteString(dvStr)
				bd.WriteByte('\'')
				//dvStr = quoter.Quote(dvStr)
			} else {
				if _, err := bd.WriteString(dvStr); err != nil {
					return "", err
				}
			}
		}
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

func (db *TDialect) GetFields(ctx context.Context, session *TSession, model IModel) ([]string, map[string]IField, error) {
	return db.dialect.GetFields(ctx, session, model)
}

func (db *TDialect) GetModels(ctx context.Context, session *TSession) ([]IModel, error) {
	return db.dialect.GetModels(ctx, session)
}

func (db *TDialect) GetIndexes(ctx context.Context, session *TSession, tableName string) (map[string]*TIndex, error) {
	return db.dialect.GetIndexes(ctx, session, tableName)
}
