package orm

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/volts-dev/orm/core"
	"github.com/volts-dev/orm/dialect"
	"github.com/volts-dev/utils"
)

const MYSQL = "mysql"

var (
	mysqlReservedWords = map[string]bool{
		"ADD":               true,
		"ALL":               true,
		"ALTER":             true,
		"ANALYZE":           true,
		"AND":               true,
		"AS":                true,
		"ASC":               true,
		"ASENSITIVE":        true,
		"BEFORE":            true,
		"BETWEEN":           true,
		"BIGINT":            true,
		"BINARY":            true,
		"BLOB":              true,
		"BOTH":              true,
		"BY":                true,
		"CALL":              true,
		"CASCADE":           true,
		"CASE":              true,
		"CHANGE":            true,
		"CHAR":              true,
		"CHARACTER":         true,
		"CHECK":             true,
		"COLLATE":           true,
		"COLUMN":            true,
		"CONDITION":         true,
		"CONNECTION":        true,
		"CONSTRAINT":        true,
		"CONTINUE":          true,
		"CONVERT":           true,
		"CREATE":            true,
		"CROSS":             true,
		"CURRENT_DATE":      true,
		"CURRENT_TIME":      true,
		"CURRENT_TIMESTAMP": true,
		"CURRENT_USER":      true,
		"CURSOR":            true,
		"DATABASE":          true,
		"DATABASES":         true,
		"DAY_HOUR":          true,
		"DAY_MICROSECOND":   true,
		"DAY_MINUTE":        true,
		"DAY_SECOND":        true,
		"DEC":               true,
		"DECIMAL":           true,
		"DECLARE":           true,
		"DEFAULT":           true,
		"DELAYED":           true,
		"DELETE":            true,
		"DESC":              true,
		"DESCRIBE":          true,
		"DETERMINISTIC":     true,
		"DISTINCT":          true,
		"DISTINCTROW":       true,
		"DIV":               true,
		"DOUBLE":            true,
		"DROP":              true,
		"DUAL":              true,
		"EACH":              true,
		"ELSE":              true,
		"ELSEIF":            true,
		"ENCLOSED":          true,
		"ESCAPED":           true,
		"EXISTS":            true,
		"EXIT":              true,
		"EXPLAIN":           true,
		"FALSE":             true,
		"FETCH":             true,
		"FLOAT":             true,
		"FLOAT4":            true,
		"FLOAT8":            true,
		"FOR":               true,
		"FORCE":             true,
		"FOREIGN":           true,
		"FROM":              true,
		"FULLTEXT":          true,
		"GOTO":              true,
		"GRANT":             true,
		"GROUP":             true,
		"HAVING":            true,
		"HIGH_PRIORITY":     true,
		"HOUR_MICROSECOND":  true,
		"HOUR_MINUTE":       true,
		"HOUR_SECOND":       true,
		"IF":                true,
		"IGNORE":            true,
		"IN":                true, "INDEX": true,
		"INFILE": true, "INNER": true, "INOUT": true,
		"INSENSITIVE": true, "INSERT": true, "INT": true,
		"INT1": true, "INT2": true, "INT3": true,
		"INT4": true, "INT8": true, "INTEGER": true,
		"INTERVAL": true, "INTO": true, "IS": true,
		"ITERATE": true, "JOIN": true, "KEY": true,
		"KEYS": true, "KILL": true, "LABEL": true,
		"LEADING": true, "LEAVE": true, "LEFT": true,
		"LIKE": true, "LIMIT": true, "LINEAR": true,
		"LINES": true, "LOAD": true, "LOCALTIME": true,
		"LOCALTIMESTAMP": true, "LOCK": true, "LONG": true,
		"LONGBLOB": true, "LONGTEXT": true, "LOOP": true,
		"LOW_PRIORITY": true, "MATCH": true, "MEDIUMBLOB": true,
		"MEDIUMINT": true, "MEDIUMTEXT": true, "MIDDLEINT": true,
		"MINUTE_MICROSECOND": true, "MINUTE_SECOND": true, "MOD": true,
		"MODIFIES": true, "NATURAL": true, "NOT": true,
		"NO_WRITE_TO_BINLOG": true, "NULL": true, "NUMERIC": true,
		"ON	OPTIMIZE": true, "OPTION": true,
		"OPTIONALLY": true, "OR": true, "ORDER": true,
		"OUT": true, "OUTER": true, "OUTFILE": true,
		"PRECISION": true, "PRIMARY": true, "PROCEDURE": true,
		"PURGE": true, "RAID0": true, "RANGE": true,
		"READ": true, "READS": true, "REAL": true,
		"REFERENCES": true, "REGEXP": true, "RELEASE": true,
		"RENAME": true, "REPEAT": true, "REPLACE": true,
		"REQUIRE": true, "RESTRICT": true, "RETURN": true,
		"REVOKE": true, "RIGHT": true, "RLIKE": true,
		"SCHEMA": true, "SCHEMAS": true, "SECOND_MICROSECOND": true,
		"SELECT": true, "SENSITIVE": true, "SEPARATOR": true,
		"SET": true, "SHOW": true, "SMALLINT": true,
		"SPATIAL": true, "SPECIFIC": true, "SQL": true,
		"SQLEXCEPTION": true, "SQLSTATE": true, "SQLWARNING": true,
		"SQL_BIG_RESULT": true, "SQL_CALC_FOUND_ROWS": true, "SQL_SMALL_RESULT": true,
		"SSL": true, "STARTING": true, "STRAIGHT_JOIN": true,
		"TABLE": true, "TERMINATED": true, "THEN": true,
		"TINYBLOB": true, "TINYINT": true, "TINYTEXT": true,
		"TO": true, "TRAILING": true, "TRIGGER": true,
		"TRUE": true, "UNDO": true, "UNION": true,
		"UNIQUE": true, "UNLOCK": true, "UNSIGNED": true,
		"UPDATE": true, "USAGE": true, "USE": true,
		"USING": true, "UTC_DATE": true, "UTC_TIME": true,
		"UTC_TIMESTAMP": true, "VALUES": true, "VARBINARY": true,
		"VARCHAR":      true,
		"VARCHARACTER": true,
		"VARYING":      true,
		"WHEN":         true,
		"WHERE":        true,
		"WHILE":        true,
		"WITH":         true,
		"WRITE":        true,
		"X509":         true,
		"XOR":          true,
		"YEAR_MONTH":   true,
		"ZEROFILL":     true,
	}
)

type mysql struct {
	TDialect
	rowFormat string
}

var mysqlColAliases = map[string]string{
	"numeric": "decimal",
}

func init() {
	RegisterDialect("mysql", func() IDialect {
		return &mysql{}
	})
}

func (db *mysql) Init(queryer core.Queryer, uri *TDataSource) error {
	db.quoter = dialect.Quoter{
		Prefix:     '`',
		Suffix:     '`',
		IsReserved: dialect.AlwaysReserve,
	}
	return db.TDialect.Init(queryer, db.dialect, uri)
}

func (db *mysql) String() string {
	return "mysql"
}

// Alias returns a alias of column
func (db *mysql) Alias(col string) string {
	v, ok := mysqlColAliases[strings.ToLower(col)]
	if ok {
		return v
	}
	return col
}

func (db *mysql) Version(ctx context.Context) (*core.Version, error) {
	rows, err := db.queryer.QueryContext(ctx, "SELECT @@VERSION")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var version string
	if !rows.Next() {
		if rows.Err() != nil {
			return nil, rows.Err()
		}
		return nil, errors.New("unknow version")
	}

	if err := rows.Scan(&version); err != nil {
		return nil, err
	}

	fields := strings.Split(version, "-")
	if len(fields) == 3 && fields[1] == "TiDB" {
		// 5.7.25-TiDB-v3.0.3
		return &core.Version{
			Number:  strings.TrimPrefix(fields[2], "v"),
			Level:   fields[0],
			Edition: fields[1],
		}, nil
	}

	var edition string
	if len(fields) == 2 {
		edition = fields[1]
	}

	return &core.Version{
		Number:  fields[0],
		Edition: edition,
	}, nil
}

func (db *mysql) SetParams(params map[string]string) {
	rowFormat, ok := params["rowFormat"]
	if ok {
		t := strings.ToUpper(rowFormat)
		switch t {
		case "COMPACT":
			fallthrough
		case "REDUNDANT":
			fallthrough
		case "DYNAMIC":
			fallthrough
		case "COMPRESSED":
			db.rowFormat = t
		}
	}
}
func (db *mysql) SyncToSqlType(ctx *TTagContext) {
	params := ctx.Params
	l := len(params)
	switch f := ctx.Field.(type) {
	case *TIntField:
		if l > 0 {
			switch utils.ToInt(params[0]) {
			case 8:
				f.SqlType = SQLType{TinyInt, 0, 0}
				f._attr_type = TinyInt
				//f._attr_size = 1 // 1 byte
			case 16:
				f.SqlType = SQLType{SmallInt, 0, 0}
				f._attr_type = SmallInt
				//f._attr_size = 2
			case 24:
				f.SqlType = SQLType{MediumInt, 0, 0}
				f._attr_type = MediumInt
				//f._attr_size = 3
			case 32:
				f.SqlType = SQLType{Int, 0, 0}
				f._attr_type = Int
				//f._attr_size = 4
			case 64:
				f.SqlType = SQLType{BigInt, 0, 0}
				f._attr_type = BigInt
				//f._attr_size = 8
			}
		}
	}
}
func (db *mysql) GetSqlType(field IField) string {
	var res string
	var isUnsigned bool
	c := field.Base()

	switch t := c.SQLType().Name; t {
	case Bool:
		res = TinyInt
		c._attr_size = 1

	case Int:
		switch c.SQLType().DefaultLength {
		case 4:
			res = TinyInt
			c._attr_size = 1 // 1 byte
		case 8:
			res = SmallInt
			c._attr_size = 2
		case 16:
			res = MediumInt
			c._attr_size = 3
		case 32:
			res = Int
			c._attr_size = 4
		case 64:
			res = BigInt
			c._attr_size = 8
		}
	case Serial:
		c.isAutoIncrement = true
		c.isPrimaryKey = true
		c._attr_required = true
		res = Int
	case BigSerial:
		c.isAutoIncrement = true
		c.isPrimaryKey = true
		c.isPrimaryKey = true
		res = BigInt
	case Bytea:
		res = Blob
	case TimeStampz:
		res = Char
		c._attr_size = 64
	case Enum: // mysql enum
		res = Enum
		res += "("
		opts := ""
		for v := range c.EnumOptions {
			opts += fmt.Sprintf(",'%v'", v)
		}
		res += strings.TrimLeft(opts, ",")
		res += ")"
	case Set: // mysql set
		res = Set
		res += "("
		opts := ""
		for v := range c.SetOptions {
			opts += fmt.Sprintf(",'%v'", v)
		}
		res += strings.TrimLeft(opts, ",")
		res += ")"
	case NVarchar:
		res = Varchar
	case Uuid:
		res = Varchar
		c._attr_size = 40
	case Json:
		res = Text
	case UnsignedInt:
		res = Int
		isUnsigned = true
	case UnsignedBigInt:
		res = BigInt
		isUnsigned = true
	case UnsignedMediumInt:
		res = MediumInt
		isUnsigned = true
	case UnsignedSmallInt:
		res = SmallInt
		isUnsigned = true
	case UnsignedTinyInt:
		res = TinyInt
		isUnsigned = true
	default:
		res = t
	}
	c.SQLType().DefaultLength = c._attr_size
	hasLen1 := (c.SQLType().DefaultLength > 0)
	hasLen2 := (c.SQLType().DefaultLength2 > 0)

	if res == BigInt && !hasLen1 && !hasLen2 {
		c.SQLType().DefaultLength = 20
		hasLen1 = true
	}

	if hasLen2 {
		res += "(" + strconv.FormatInt(int64(c.SQLType().DefaultLength), 10) + "," + strconv.FormatInt(int64(c.SQLType().DefaultLength2), 10) + ")"
	} else if hasLen1 {
		res += "(" + strconv.FormatInt(int64(c.SQLType().DefaultLength), 10) + ")"
	}

	if isUnsigned {
		res += " UNSIGNED"
	}

	return res
}

func (db *mysql) ColumnTypeKind(t string) int {
	switch strings.ToUpper(t) {
	case "DATETIME":
		return TIME_TYPE
	case "CHAR", "VARCHAR", "TINYTEXT", "TEXT", "MEDIUMTEXT", "LONGTEXT", "ENUM", "SET":
		return TEXT_TYPE
	case "BIGINT", "TINYINT", "SMALLINT", "MEDIUMINT", "INT", "FLOAT", "REAL", "DOUBLE PRECISION", "DECIMAL", "NUMERIC", "BIT":
		return NUMERIC_TYPE
	case "BINARY", "VARBINARY", "TINYBLOB", "BLOB", "MEDIUMBLOB", "LONGBLOB":
		return BLOB_TYPE
	default:
		return UNKNOW_TYPE
	}
}

func (db *mysql) IsReserved(name string) bool {
	_, ok := mysqlReservedWords[strings.ToUpper(name)]
	return ok
}

func (db *mysql) IsDatabaseExist(ctx context.Context, name string) bool {
	s := "SELECT SCHEMA_NAME FROM INFORMATION_SCHEMA.SCHEMATA WHERE SCHEMA_NAME = $1"
	db.LogSQL(s, []interface{}{name})

	rows, err := db.queryer.QueryContext(ctx, s, name)
	if err != nil {
		return false
	}
	defer rows.Close()

	for rows.Next() {
		var database_name string
		err = rows.Scan(&database_name)
		if err != nil {
			log.Panicf(err.Error())
			return false
		}

		return database_name == name
	}

	return false
}

func (db *mysql) AutoIncrStr() string {
	return "AUTO_INCREMENT"
}

func (db *mysql) IndexCheckSql(tableName, idxName string) (string, []interface{}) {
	args := []interface{}{db.DbName, tableName, idxName}
	sql := "SELECT `INDEX_NAME` FROM `INFORMATION_SCHEMA`.`STATISTICS`"
	sql += " WHERE `TABLE_SCHEMA` = ? AND `TABLE_NAME` = ? AND `INDEX_NAME`=?"
	return sql, args
}

func (db *mysql) GenAddColumnSQL(tableName string, field IField) string {
	quoter := db.dialect.Quoter()
	s, _ := ColumnString(db.dialect, field, true)
	sql := fmt.Sprintf("ALTER TABLE %v ADD %v", quoter.Quote(tableName), s)
	if len(field.Title()) > 0 {
		sql += " COMMENT '" + field.Title() + "'"
	}
	return sql
}

func (db *mysql) GetFields(ctx context.Context, tableName string) ([]string, map[string]IField, error) {
	args := []interface{}{db.DbName, tableName}
	alreadyQuoted := "(INSTR(VERSION(), 'maria') > 0 && " +
		"(SUBSTRING_INDEX(VERSION(), '.', 1) > 10 || " +
		"(SUBSTRING_INDEX(VERSION(), '.', 1) = 10 && " +
		"(SUBSTRING_INDEX(SUBSTRING(VERSION(), 4), '.', 1) > 2 || " +
		"(SUBSTRING_INDEX(SUBSTRING(VERSION(), 4), '.', 1) = 2 && " +
		"SUBSTRING_INDEX(SUBSTRING(VERSION(), 6), '-', 1) >= 7)))))"
	s := "SELECT `COLUMN_NAME`, `IS_NULLABLE`, `COLUMN_DEFAULT`, `COLUMN_TYPE`," +
		" `COLUMN_KEY`, `EXTRA`, `COLUMN_COMMENT`, `CHARACTER_MAXIMUM_LENGTH`, " +
		alreadyQuoted + " AS NEEDS_QUOTE " +
		"FROM `INFORMATION_SCHEMA`.`COLUMNS` WHERE `TABLE_SCHEMA` = ? AND `TABLE_NAME` = ?" +
		" ORDER BY `COLUMNS`.ORDINAL_POSITION ASC"

	rows, err := db.queryer.QueryContext(ctx, s, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	cols := make(map[string]IField)
	colSeq := make([]string, 0)
	for rows.Next() {
		var columnName, nullableStr, colType, colKey, extra, comment string
		var alreadyQuoted, isUnsigned bool
		var colDefault, maxLength *string
		err = rows.Scan(&columnName, &nullableStr, &colDefault, &colType, &colKey, &extra, &comment, &maxLength, &alreadyQuoted)
		if err != nil {
			return nil, nil, err
		}

		fields := strings.Fields(colType)
		if len(fields) == 2 && fields[1] == "unsigned" {
			isUnsigned = true
		}
		colType = fields[0]
		cts := strings.Split(colType, "(")
		colName := cts[0]
		// Remove the /* mariadb-5.3 */ suffix from coltypes
		colName = strings.TrimSuffix(colName, "/* mariadb-5.3 */")
		colType = strings.ToUpper(colName)

		enumOptions := make(map[string]int)
		setOptions := make(map[string]int)
		var len1, len2 int64
		if len(cts) == 2 {
			idx := strings.Index(cts[1], ")")
			if colType == Enum && cts[1][0] == '\'' { // enum
				options := strings.Split(cts[1][0:idx], ",")
				for k, v := range options {
					v = strings.TrimSpace(v)
					v = strings.Trim(v, "'")
					enumOptions[v] = k
				}
			} else if colType == Set && cts[1][0] == '\'' {
				options := strings.Split(cts[1][0:idx], ",")
				for k, v := range options {
					v = strings.TrimSpace(v)
					v = strings.Trim(v, "'")
					setOptions[v] = k
				}
			} else {
				lens := strings.Split(cts[1][0:idx], ",")
				len1, err = strconv.ParseInt(strings.TrimSpace(lens[0]), 10, 64)
				if err != nil {
					return nil, nil, err
				}
				if len(lens) == 2 {
					len2, err = strconv.ParseInt(lens[1], 10, 64)
					if err != nil {
						return nil, nil, err
					}
				}
			}
		} else {
			switch colType {
			case "MEDIUMTEXT", "LONGTEXT", "TEXT":
				len1, err = strconv.ParseInt(*maxLength, 10, 64)
				if err != nil {
					return nil, nil, err
				}
			}
		}
		if isUnsigned {
			colType = "UNSIGNED " + colType
		}

		if _, ok := SqlTypes[colType]; !ok {
			return nil, nil, fmt.Errorf("unknown colType %v", colType)
		}

		col, err := NewField(strings.Trim(columnName, `" `), WithSQLType(SQLType{Name: colType, DefaultLength: int(len1), DefaultLength2: int(len2)}))
		if err != nil {
			return nil, nil, err
		}
		col.Base().EnumOptions = enumOptions
		col.Base().SetOptions = setOptions

		if colDefault != nil && (!alreadyQuoted || *colDefault != "NULL") {
			col.Base()._attr_default = *colDefault
			//col.Base().defaultIsEmpty = false
		} else {
			//col.Base().defaultIsEmpty = true
		}

		col.Base()._attr_title = comment
		col.Base()._attr_required = (nullableStr == "NO")

		if colKey == "PRI" {
			col.Base().isAutoIncrement = true
		}
		// if colKey == "UNI" {
		// col.is
		// }

		if extra == "auto_increment" {
			col.Base().isAutoIncrement = true
		}

		if !col.IsDefaultEmpty() {
			if !alreadyQuoted && col.SQLType().IsText() {
				col.Base()._attr_default = "'" + col.Default() + "'"
			} else if col.SQLType().IsTime() && !alreadyQuoted && col.Default() != "CURRENT_TIMESTAMP" {
				col.Base()._attr_default = "'" + col.Default() + "'"
			}
		}

		cols[col.Name()] = col
		colSeq = append(colSeq, col.Name())
	}

	if rows.Err() != nil {
		return nil, nil, rows.Err()
	}
	return colSeq, cols, nil
}

func (db *mysql) GetModels(ctx context.Context) ([]IModel, error) {
	//func (db *mysql) GetTables(queryer Queryer, ctx context.Context) ([]*Table, error) {
	args := []interface{}{db.DbName}
	s := "SELECT `TABLE_NAME`, `ENGINE`, `AUTO_INCREMENT`, `TABLE_COMMENT` from " +
		"`INFORMATION_SCHEMA`.`TABLES` WHERE `TABLE_SCHEMA`=? AND (`ENGINE`='MyISAM' OR `ENGINE` = 'InnoDB' OR `ENGINE` = 'TokuDB')"

	rows, err := db.queryer.QueryContext(ctx, s, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	models := make([]IModel, 0)
	for rows.Next() {
		var name, engine string
		var autoIncr, comment *string
		err = rows.Scan(&name, &engine, &autoIncr, &comment)
		if err != nil {
			return nil, err
		}

		// new a base model instance
		model_val := reflect.Indirect(reflect.ValueOf(new(TModel)))
		model_type := model_val.Type()
		model := newModel("", name, model_val, model_type)
		if comment != nil {
			model.GetBase().description = *comment
		}

		models = append(models, model)
	}

	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return models, nil
}

func (db *mysql) GetIndexes(ctx context.Context, tableName string) (map[string]*TIndex, error) {
	args := []interface{}{db.DbName, tableName}
	s := "SELECT `INDEX_NAME`, `NON_UNIQUE`, `COLUMN_NAME` FROM `INFORMATION_SCHEMA`.`STATISTICS` WHERE `TABLE_SCHEMA` = ? AND `TABLE_NAME` = ? ORDER BY `SEQ_IN_INDEX`"

	rows, err := db.queryer.QueryContext(ctx, s, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	indexes := make(map[string]*TIndex)
	for rows.Next() {
		var indexType int
		var indexName, colName, nonUnique string
		err = rows.Scan(&indexName, &nonUnique, &colName)
		if err != nil {
			return nil, err
		}

		if indexName == "PRIMARY" {
			continue
		}

		if nonUnique == "YES" || nonUnique == "1" {
			indexType = IndexType
		} else {
			indexType = UniqueType
		}

		colName = strings.Trim(colName, "` ")
		var isRegular bool
		if strings.HasPrefix(indexName, "IDX_"+tableName) || strings.HasPrefix(indexName, "UQE_"+tableName) {
			indexName = indexName[5+len(tableName):]
			isRegular = true
		}

		var index *TIndex
		var ok bool
		if index, ok = indexes[indexName]; !ok {
			index = new(TIndex)
			index.IsRegular = isRegular
			index.Type = indexType
			index.Name = indexName
			indexes[indexName] = index
		}
		index.AddColumn(colName)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return indexes, nil
}

func (db *mysql) CreateTableSql(model IModel, storeEngine, charset string) string {

	//func (db *mysql) CreateTableSQL(ctx context.Context, table *Table, tableName string) (string, bool, error) {

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

		if len(field.Title()) > 0 {
			b.WriteString(" COMMENT '")
			b.WriteString(field.Title())
			b.WriteString("'")
		}
		fieldCnt++
	}

	if len(model.GetPrimaryKeys()) > 1 {
		b.WriteString(", PRIMARY KEY (")
		b.WriteString(quoter.Join(model.GetPrimaryKeys(), ","))
		b.WriteString(")")
	}
	b.WriteString(")")

	if storeEngine != "" {
		b.WriteString(" ENGINE=")
		b.WriteString(storeEngine)
	}

	if len(charset) == 0 {
		charset = db.Charset
	}
	if len(charset) != 0 {
		b.WriteString(" DEFAULT CHARSET ")
		b.WriteString(charset)
	}

	if db.rowFormat != "" {
		b.WriteString(" ROW_FORMAT=")
		b.WriteString(db.rowFormat)
	}

	if model.GetTableDescription() != "" {
		b.WriteString(" COMMENT='")
		b.WriteString(model.GetTableDescription())
		b.WriteString("'")
	}

	return b.String()
}

func (db *mysql) Fmter() []IFmter {
	return []IFmter{}
}
