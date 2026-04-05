package orm

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"

	"github.com/volts-dev/orm/core"
	"github.com/volts-dev/orm/dialect"
)

type sqlite struct {
	TDialect
}

func (db *sqlite) Fmter() []IFmter {
	return nil
}

func (db *sqlite) GenAddColumnSQL(tableName string, field IField) string {
	quoter := db.dialect.Quoter()
	s, _ := ColumnString(db.dialect, field, true)
	sql := fmt.Sprintf("ALTER TABLE %v ADD COLUMN %v", quoter.Quote(tableName), s)
	return sql
}

func init() {
	RegisterDialect("sqlite", func() IDialect {
		return &sqlite{}
	})
	RegisterDialect("sqlite3", func() IDialect {
		return &sqlite{}
	})
}

func (db *sqlite) Init(queryer core.Queryer, uri *TDataSource) error {
	db.quoter = dialect.Quoter{
		Prefix:     '`',
		Suffix:     '`',
		IsReserved: dialect.AlwaysReserve,
	}
	if err := db.TDialect.Init(queryer, db, uri); err != nil {
		return err
	}

	// Enable WAL mode for better concurrency with transactions.
	// WAL mode allows readers to see committed data immediately from other connections,
	// and prevents "database is locked" errors during concurrent read/write operations.
	if coreDB, ok := queryer.(*core.DB); ok {
		if _, err := coreDB.Exec("PRAGMA journal_mode=WAL"); err != nil {
			log.Warnf("Failed to set WAL journal mode: %v", err)
		}
		if _, err := coreDB.Exec("PRAGMA busy_timeout=5000"); err != nil {
			log.Warnf("Failed to set busy_timeout: %v", err)
		}
	}

	return nil
}

func (db *sqlite) String() string {
	return "sqlite"
}

func (db *sqlite) Version(ctx context.Context) (*core.Version, error) {
	return &core.Version{Number: "sqlite"}, nil
}

func (db *sqlite) SupportReturning() bool {
	return true
}

func (db *sqlite) Alias(col string) string {
	return col
}

func (db *sqlite) GetSqlType(field IField) string {
	c := field.Base()
	t := c.SQLType().Name
	if t == Serial || t == BigSerial {
		c.isAutoIncrement = true
		c.isPrimaryKey = true
		return "INTEGER"
	}
	if c.IsAutoIncrement() {
		return "INTEGER"
	}
	return t
}

func (db *sqlite) ColumnTypeKind(t string) int {
	return UNKNOW_TYPE
}

func (db *sqlite) AutoIncrStr() string {
	return "AUTOINCREMENT"
}

func (db *sqlite) IndexCheckSql(tableName, idxName string) (string, []interface{}) {
	return "SELECT name FROM sqlite_master WHERE type='index' and name=?", []interface{}{idxName}
}

func (db *sqlite) TableCheckSql(tableName string) (string, []interface{}) {
	return "SELECT name FROM sqlite_master WHERE type='table' and name=?", []interface{}{tableName}
}

func (db *sqlite) GetFields(ctx context.Context, tableName string) ([]string, map[string]IField, error) {
	// SQLite doesn't have a direct information_schema.columns-like interface for all details easily,
	// but we can use PRAGMA table_info
	s := fmt.Sprintf("PRAGMA table_info(%v)", db.quoter.Quote(tableName))
	db.LogSQL(s, nil)

	rows, err := db.queryer.QueryContext(ctx, s)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	cols := make(map[string]IField)
	colSeq := make([]string, 0)
	for rows.Next() {
		var cid int
		var name, dataType string
		var dfltVal sql.NullString
		var notNull, pk int
		if err = rows.Scan(&cid, &name, &dataType, &notNull, &dfltVal, &pk); err != nil {
			return nil, nil, err
		}

		// Simplified type mapping for SQLite
		var sqlType SQLType
		dataType = strings.ToUpper(dataType)
		if strings.Contains(dataType, "INT") {
			sqlType = SQLType{Int, 0, 0}
		} else if strings.Contains(dataType, "CHAR") || strings.Contains(dataType, "TEXT") {
			sqlType = SQLType{Varchar, 0, 0}
		} else if strings.Contains(dataType, "REAL") || strings.Contains(dataType, "FLOAT") || strings.Contains(dataType, "DOUBLE") {
			sqlType = SQLType{Double, 0, 0}
		} else {
			sqlType = SQLType{Name: dataType}
		}

		col, err := NewField(name, WithSQLType(sqlType))
		if err != nil {
			return nil, nil, err
		}
		col.Base().isPrimaryKey = (pk > 0)
		col.Base()._attr_required = (notNull > 0)
		if dfltVal.Valid && dfltVal.String != "" && dfltVal.String != "NULL" {
			col.Base()._attr_default = strings.Trim(dfltVal.String, "'")
		}

		cols[name] = col
		colSeq = append(colSeq, name)
	}

	return colSeq, cols, nil
}

func (db *sqlite) GetModels(ctx context.Context) ([]IModel, error) {
	s := "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'"
	db.LogSQL(s, nil)

	rows, err := db.queryer.QueryContext(ctx, s)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	models := make([]IModel, 0)
	for rows.Next() {
		var tableName string
		if err = rows.Scan(&tableName); err != nil {
			return nil, err
		}
		model_val := reflect.Indirect(reflect.ValueOf(new(TModel)))
		model := newModel("", tableName, model_val, model_val.Type(), nil)
		models = append(models, model)
	}
	return models, nil
}

func (db *sqlite) GetIndexes(ctx context.Context, tableName string) (map[string]*TIndex, error) {
	// Get index list using PRAGMA index_list
	s := fmt.Sprintf("PRAGMA index_list(%v)", db.quoter.Quote(tableName))
	db.LogSQL(s, nil)

	rows, err := db.queryer.QueryContext(ctx, s)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	indexes := make(map[string]*TIndex)
	for rows.Next() {
		var seq int
		var indexName string
		var unique int
		var origin string
		var partial int
		if err = rows.Scan(&seq, &indexName, &unique, &origin, &partial); err != nil {
			return nil, err
		}

		// Skip automatic indexes (like those for PRIMARY KEY if not named)
		if origin == "pk" {
			continue
		}

		// Get column names for each index using PRAGMA index_info
		infoSql := fmt.Sprintf("PRAGMA index_info(%v)", db.quoter.Quote(indexName))
		infoRows, err := db.queryer.QueryContext(ctx, infoSql)
		if err != nil {
			return nil, err
		}

		var cols []string
		for infoRows.Next() {
			var seqno, cid int
			var name string
			if err = infoRows.Scan(&seqno, &cid, &name); err != nil {
				infoRows.Close()
				return nil, err
			}
			cols = append(cols, name)
		}
		infoRows.Close()

		indexType := IndexType
		if unique > 0 {
			indexType = UniqueType
		}

		var isRegular bool
		if strings.HasPrefix(indexName, DefaultIndexPrefix+tableName) || strings.HasPrefix(indexName, DefaultUniquePrefix+tableName) {
			isRegular = true
		}

		index := newIndex(indexName, tableName, indexType, cols...)
		index.IsRegular = isRegular
		indexes[index.Name] = index
	}
	return indexes, nil
}

func (db *sqlite) CreateTableSql(model IModel, storeEngine, charset string) string {
	quoter := db.dialect.Quoter()
	var b strings.Builder
	b.WriteString("CREATE TABLE IF NOT EXISTS ")
	quoter.QuoteTo(&b, fmtTableName(model.String()))
	b.WriteString(" (")

	fields := model.GetFields()
	fieldCnt := 0
	lastIdx := len(fields)
	for idx, field := range fields {
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
	return b.String()
}

func (db *sqlite) CreateIndexUniqueSql(tableName string, index *TIndex) string {
	quoter := db.dialect.Quoter().Quote
	var unique string
	if index.Type == UniqueType {
		unique = " UNIQUE"
	}
	idxName := index.GetName(tableName)
	return fmt.Sprintf("CREATE%s INDEX IF NOT EXISTS %v ON %v (%v)", unique,
		quoter(idxName), quoter(tableName),
		quoter(strings.Join(index.Cols, quoter(","))))
}

func (db *sqlite) IsDatabaseExist(ctx context.Context, name string) bool              { return true }
func (db *sqlite) CreateDatabase(dbx *sql.DB, ctx context.Context, name string) error { return nil }
func (db *sqlite) DropDatabase(dbx *sql.DB, ctx context.Context, name string) error   { return nil }
func (db *sqlite) IsReserved(name string) bool                                        { return false }
func (db *sqlite) IsColumnExist(ctx context.Context, tableName string, colName string) (bool, error) {
	return false, nil
}
func (db *sqlite) DropIndexUniqueSql(tableName string, index *TIndex) string {
	idxName := index.GetName(tableName)
	return fmt.Sprintf("DROP INDEX IF EXISTS %v", db.quoter.Quote(idxName))
}
func (db *sqlite) ModifyColumnSql(tableName string, col IField) string { return "" }

func (db *sqlite) GenInsertSql(tableName string, fields []string, uniqueFields []string, idField string, onConflict *OnConflict) string {
	var sqlStr strings.Builder
	cnt := len(fields)
	field_places := strings.Repeat("?,", cnt-1) + "?"
	field_list := ""
	quoter := db.quoter.Quote

	insertVerb := "INSERT"
	if onConflict != nil && onConflict.DoNothing {
		insertVerb = "INSERT OR IGNORE"
	}

	for i, name := range fields {
		if i < cnt-1 {
			field_list = field_list + quoter(name) + ","
		} else {
			field_list = field_list + quoter(name)
		}
	}

	sqlStr.WriteString(insertVerb)
	sqlStr.WriteString(" INTO ")
	sqlStr.WriteString(quoter(tableName))
	sqlStr.WriteString(" (")
	sqlStr.WriteString(field_list)
	sqlStr.WriteString(") ")
	sqlStr.WriteString("VALUES (")
	sqlStr.WriteString(field_places)
	sqlStr.WriteString(") ")

	if onConflict != nil && !onConflict.DoNothing {
		sqlStr.WriteString("ON CONFLICT (")
		if len(uniqueFields) > 0 {
			for idx, f := range uniqueFields {
				if idx > 0 {
					sqlStr.WriteString(",")
				}
				sqlStr.WriteString(quoter(f))
			}
		} else {
			sqlStr.WriteString(quoter(idField))
		}
		sqlStr.WriteString(")")
		sqlStr.WriteString(" DO UPDATE SET ")
		if len(onConflict.DoUpdates) > 0 {
			for idx, field := range onConflict.DoUpdates {
				if idx > 0 {
					sqlStr.WriteByte(',')
				}
				sqlStr.WriteString(quoter(field))
				sqlStr.WriteString(" = ?")
			}
		} else {
			sqlStr.WriteString(quoter(idField))
			sqlStr.WriteString(" = ")
			sqlStr.WriteString(quoter(idField))
		}
	} else if len(idField) > 0 {
		sqlStr.WriteString("RETURNING ")
		sqlStr.WriteString(quoter(idField))
	}

	return sqlStr.String()
}
