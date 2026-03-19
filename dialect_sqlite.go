package orm

import (
	"context"
	"database/sql"
	"fmt"
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
	return db.TDialect.Init(queryer, db, uri)
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
	return nil, nil, nil
}

func (db *sqlite) GetModels(ctx context.Context) ([]IModel, error) {
	return nil, nil
}

func (db *sqlite) GetIndexes(ctx context.Context, tableName string) (map[string]*TIndex, error) {
	return nil, nil
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

func (db *sqlite) IsDatabaseExist(ctx context.Context, name string) bool { return true }
func (db *sqlite) CreateDatabase(dbx *sql.DB, ctx context.Context, name string) error { return nil }
func (db *sqlite) DropDatabase(dbx *sql.DB, ctx context.Context, name string) error { return nil }
func (db *sqlite) IsReserved(name string) bool { return false }
func (db *sqlite) IsColumnExist(ctx context.Context, tableName string, colName string) (bool, error) { return false, nil }
func (db *sqlite) DropIndexUniqueSql(tableName string, index *TIndex) string { return "" }
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
