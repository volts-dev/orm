// 本文件部分代码源自 xorm（https://gitea.com/xorm/xorm），BSD 3-Clause 许可，
// Copyright (c) 2013 - 2015 The Xorm Authors。许可全文见仓库根目录 LICENSE-xorm。

package orm

import (
	"context"
	"database/sql"
	"database/sql/driver"
	stdErrors "errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/volts-dev/orm/core"
	"github.com/volts-dev/orm/dialect"
	ormerr "github.com/volts-dev/orm/errors"
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

	// functional key parts（表达式索引）支持情况：MySQL ≥ 8.0.13 有，MariaDB/TiDB 没有。
	// 惰性查一次 @@VERSION 并缓存；funcIdxForce 供测试与无连接场景强制指定。
	funcIdxOnce  sync.Once
	funcIdxOK    bool
	funcIdxForce *bool
	// 非唯一部分索引退化成全表索引时只告警一次（按索引名）。
	partialWarned sync.Map
}

// mysqlSupportsFunctionalIndex 判断版本是否支持 functional key parts。
func mysqlSupportsFunctionalIndex(v *core.Version) bool {
	if v == nil {
		return false
	}
	lowerAll := strings.ToLower(v.Number + " " + v.Edition + " " + v.Level)
	if strings.Contains(lowerAll, "mariadb") || strings.Contains(lowerAll, "tidb") {
		return false
	}
	return versionAtLeast(v.Number, 8, 0, 13)
}

// versionAtLeast 比较 "8.0.13" 这类点分版本号；解析不出来按不支持处理。
func versionAtLeast(number string, major, minor, patch int) bool {
	parts := strings.SplitN(strings.TrimSpace(number), ".", 3)
	nums := [3]int{}
	for i := 0; i < len(parts) && i < 3; i++ {
		digits := parts[i]
		for j, r := range digits {
			if r < '0' || r > '9' {
				digits = digits[:j]
				break
			}
		}
		if digits == "" {
			return false
		}
		n := 0
		for _, r := range digits {
			n = n*10 + int(r-'0')
		}
		nums[i] = n
	}
	want := [3]int{major, minor, patch}
	for i := 0; i < 3; i++ {
		if nums[i] != want[i] {
			return nums[i] > want[i]
		}
	}
	return true
}

func (db *mysql) supportsFunctionalIndex() bool {
	if db.funcIdxForce != nil {
		return *db.funcIdxForce
	}
	db.funcIdxOnce.Do(func() {
		if db.queryer == nil {
			return
		}
		v, err := db.Version(context.Background())
		if err != nil {
			log.Warnf("mysql: cannot read server version (%v); treating functional key parts as unsupported", err)
			return
		}
		db.funcIdxOK = mysqlSupportsFunctionalIndex(v)
	})
	return db.funcIdxOK
}

// ValidateIndex：表达式索引与**唯一**部分索引都要靠 functional key parts 表达；
// 没有它就拒绝。非唯一部分索引不在此拒绝——它能无损退化成全表索引（见 CreateIndexUniqueSql）。
func (db *mysql) ValidateIndex(index *TIndex) error {
	needsFunctional := index.HasExprs() || (index.Type == UniqueType && index.IsPartial())
	if needsFunctional && !db.supportsFunctionalIndex() {
		return ormerr.New(ormerr.ErrIndexUnsupported, fmt.Errorf(
			"index %s needs functional key parts (MySQL >= 8.0.13; MariaDB/TiDB have none): "+
				"expression indexes and partial UNIQUE indexes cannot be expressed here, and a plain "+
				"fallback would silently change uniqueness semantics", index.Name))
	}
	return nil
}

// indexOnColumns：JSON 列不能直接建索引（3152，只能对某个 JSON 路径建生成列索引）；TEXT / BLOB 列要前缀长度
// （1170）。PG 上这类索引是 GIN（properties 的 @>）或普通 btree——MySQL 上非唯一的跳过，唯一的报错
// （按前缀判唯一会改变语义）。
//
// 键的总宽超过 InnoDB 的 3072 字节（1071）同样处理：utf8mb4 按每字符 4 字节算，VARCHAR(768) 就到顶了；
// 只存 ASCII 的列标 ascii（TAG_ASCII）按 1 字节算。
func (db *mysql) indexOnColumns(model IModel, index *TIndex) (bool, string, error) {
	keyBytes := 0
	for _, col := range index.Cols {
		f := model.GetFieldByName(col)
		if f == nil {
			continue
		}
		t := db.GetSqlType(f)
		if mysqlLobType(t) {
			if index.Type == UniqueType {
				return false, "", ormerr.New(ormerr.ErrIndexUnsupported, fmt.Errorf(
					"unique index %s on %s column %s: MySQL cannot index it without a prefix, and a prefix changes uniqueness", index.Name, t, col))
			}
			return true, fmt.Sprintf("列 %s 是 %s，MySQL 不能直接建索引", col, t), nil
		}
		if n := f.Base().size; n > 0 && (strings.HasPrefix(t, Varchar) || strings.HasPrefix(t, Char)) {
			if f.Base().ascii {
				keyBytes += n
			} else {
				keyBytes += 4 * n
			}
		}
	}
	if keyBytes > mysqlMaxKeyBytes {
		if index.Type == UniqueType {
			return false, "", ormerr.New(ormerr.ErrIndexUnsupported, fmt.Errorf(
				"unique index %s on %v: key is %d bytes in utf8mb4, MySQL allows %d; shorten the columns or mark ASCII-only ones `ascii`",
				index.Name, index.Cols, keyBytes, mysqlMaxKeyBytes))
		}
		return true, fmt.Sprintf("索引键 %d 字节超过 MySQL 的 %d", keyBytes, mysqlMaxKeyBytes), nil
	}
	return false, "", nil
}

// mysqlMaxKeyBytes：InnoDB（DYNAMIC 行格式）一条索引键的上限。
const mysqlMaxKeyBytes = 3072

// columnCharset：ascii 列（TAG_ASCII）带上字符集；排序规则用 ascii_bin，与库默认的 utf8mb4_0900_as_cs 一样区分大小写。
func (db *mysql) columnCharset(field IField) string {
	if f := field.Base(); f.ascii && !f.companyDependent {
		if t := db.GetSqlType(field); strings.HasPrefix(t, Varchar) || strings.HasPrefix(t, Char) || t == Text {
			return " CHARACTER SET ascii COLLATE ascii_bin"
		}
	}
	return ""
}

// CreateIndexUniqueSql（MySQL）：
//
//   - 表达式键部分 → functional key part `((expr))`。
//   - **唯一**部分索引 → 每个键部分包成 `(CASE WHEN (谓词) THEN 键 ELSE NULL END)`：
//     不满足谓词的行所有键都是 NULL，而 NULL 不参与唯一冲突，语义与真正的部分唯一
//     索引一致。
//   - **非唯一**部分索引 → 去掉谓词建全表索引：结果集完全一致，只是多占空间；告警一次。
func (db *mysql) CreateIndexUniqueSql(schema, tableName string, index *TIndex) string {
	quoter := db.dialect.Quoter()
	var unique string
	if index.Type == UniqueType {
		unique = " UNIQUE"
	}
	idxName := db.FitIdentifier(index.GetName(tableName))

	wrap := index.IsPartial() && index.Type == UniqueType
	if index.IsPartial() && index.Type != UniqueType {
		if _, warned := db.partialWarned.LoadOrStore(idxName, true); !warned {
			log.Warnf("mysql: partial index %s on %s has no native equivalent; creating a full-table index instead (same results, more space)", idxName, tableName)
		}
	}

	parts := make([]string, 0, len(index.Cols)+len(index.Exprs))
	for _, c := range index.Cols {
		p := quoter.Quote(strings.TrimSpace(c))
		if wrap {
			p = fmt.Sprintf("(CASE WHEN (%s) THEN %s ELSE NULL END)", strings.TrimSpace(index.Where), p)
		}
		parts = append(parts, p)
	}
	for _, e := range index.Exprs {
		e = strings.TrimSpace(e)
		p := "(" + e + ")"
		if wrap {
			p = fmt.Sprintf("(CASE WHEN (%s) THEN %s ELSE NULL END)", strings.TrimSpace(index.Where), e)
		}
		parts = append(parts, p)
	}

	return fmt.Sprintf("CREATE%s INDEX %v ON %v (%v)", unique,
		quoter.Quote(idxName), db.tableRef(schema, tableName), strings.Join(parts, ","))
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
	return db.TDialect.Init(queryer, db, uri)
}

func (db *mysql) String() string {
	return "mysql"
}

// Features：MySQL 8.0 社区版。部分唯一索引由 ORM 模拟（见 index 相关），不算原生 PartialIndex；
// 表达式索引 8.0.13 起有。JSONOps / DateTrunc / Vector 要等 P3 / P6 给出 MySQL 写法才打开。
// LikeClause：库定为区分大小写的 utf8mb4_0900_as_cs（与 PG 一致，2026-10-24），于是 LIKE 本身就和 PG 的
// LIKE 一样区分大小写；ILIKE 要显式换成 as_ci——不区分大小写、仍区分重音，这才是 PG ILIKE 的语义
// （ai_ci 连重音也不分，会比 PG 宽）。此前落成裸 LIKE：在 _ci 库上碰巧对，在 _cs 库上 ilike 变成精确大小写。
func (db *mysql) LikeClause(column, operator string) string {
	op := strings.ToUpper(strings.TrimSpace(operator))
	if strings.Contains(op, "ILIKE") {
		return fmt.Sprintf("(%s %s ? COLLATE utf8mb4_0900_as_ci)", column, strings.ReplaceAll(op, "ILIKE", "LIKE"))
	}
	return fmt.Sprintf("(%s %s ?)", column, op)
}

func (db *mysql) DropPrimaryKeySql(quotedTable, table string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP PRIMARY KEY;", quotedTable)
}

// companyJSON（field_company.go）：MySQL 8 的 JSON 列。
//
// 取一格：JSON_EXTRACT 对 JSON null 返回 JSON 的 null（JSON_UNQUOTE 之后是串 "null"），先 NULLIF 掉，
// 与 jsonb ->> 的「JSON null 是 SQL NULL」一致。布尔格子的文本是 true / false，MySQL 没有 BOOLEAN 的 CAST，
// 比成 = 'true'。
func (db *mysql) companySlotText(ref, slot string) string {
	return fmt.Sprintf(`JSON_UNQUOTE(NULLIF(JSON_EXTRACT(%s, '$."%s"'), CAST('null' AS JSON)))`, ref, slot)
}
func (db *mysql) companySlotExists(ref, slot string) string {
	return fmt.Sprintf(`JSON_CONTAINS_PATH(%s, 'one', '$."%s"')`, ref, slot)
}
func (db *mysql) companyCast(e string, field IField) string {
	switch field.SQLType().Name {
	case Bool, Boolean:
		return "((" + e + ") = 'true')"
	case TinyInt, SmallInt, MediumInt, Int, Integer, BigInt, UnsignedTinyInt, UnsignedSmallInt,
		UnsignedMediumInt, UnsignedInt, UnsignedBigInt, Serial, BigSerial:
		return "CAST((" + e + ") AS SIGNED)"
	case Float, Double, Real:
		return "CAST((" + e + ") AS DOUBLE)"
	case Decimal, Numeric, Money:
		return "CAST((" + e + ") AS DECIMAL(65,30))"
	case DateTime, TimeStamp, TimeStampz, SmallDateTime:
		return "CAST((" + e + ") AS DATETIME(6))"
	case Date:
		return "CAST((" + e + ") AS DATE)"
	}
	return "(" + e + ")"
}
func (db *mysql) companyAsText(expr string) string { return "CAST((" + expr + ") AS CHAR)" }

// TextOf：布尔列是 tinyint(1)，CAST 出来是 '1' / '0'，改写成 PG 的 'true' / 'false'。
func (db *mysql) TextOf(expr string, field IField) string {
	if isBoolField(field) {
		return boolText(expr)
	}
	return "CAST((" + expr + ") AS CHAR)"
}
func (db *mysql) companySetSlot(col, slot string, null bool) string {
	v := "CAST(? AS JSON)"
	if null {
		v = "CAST('null' AS JSON)"
	}
	return fmt.Sprintf(`%s = JSON_SET(COALESCE(%s, JSON_OBJECT()), '$."%s"', %s)`, col, col, slot, v)
}
func (db *mysql) companyUnsetSlot(col, slot string) string {
	return fmt.Sprintf(`%s = JSON_REMOVE(COALESCE(%s, JSON_OBJECT()), '$."%s"')`, col, col, slot)
}

// FitIdentifier：MySQL 标识符上限 64 字符，超了直接 1059（PG 是静默截断，所以一直没暴露）。
func (db *mysql) FitIdentifier(name string) string { return fitIdentifierHashed(name, 64) }

// DefaultValueSql：TEXT / BLOB / JSON 列不接受字面量默认值（1101），要写成表达式 DEFAULT ('…')
// （MySQL 8.0.13+）。反查时这种默认值带 DEFAULT_GENERATED，GetFields 里还原成字面量再比。
func (db *mysql) DefaultValueSql(field IField, s string, quoted bool) string {
	lit := s
	if quoted {
		lit = mysqlStringLiteral(s)
	}
	if mysqlLobType(db.GetSqlType(field)) {
		return "(" + lit + ")"
	}
	return lit
}

func mysqlLobType(sqlType string) bool {
	t := strings.ToUpper(sqlType)
	return strings.HasSuffix(t, "TEXT") || strings.HasSuffix(t, "BLOB") || t == "JSON" || t == "GEOMETRY"
}

// mysqlGeneratedDefault 把 DEFAULT_GENERATED 列的 COLUMN_DEFAULT（表达式原文，如 _utf8mb4\'[]\'）
// 还原成字面量；认不出的形状原样返回。
func mysqlGeneratedDefault(expr string) string {
	e := strings.TrimSpace(expr)
	if strings.HasPrefix(e, "_") {
		if i := strings.Index(e, `\'`); i > 0 {
			e = e[i:]
		}
	}
	if len(e) >= 4 && strings.HasPrefix(e, `\'`) && strings.HasSuffix(e, `\'`) {
		e = e[2 : len(e)-2]
		e = strings.ReplaceAll(e, `\'`, "'")
		e = strings.ReplaceAll(e, `''`, "'")
		return strings.ReplaceAll(e, `\\`, `\`)
	}
	return expr
}

// BindTime：把墙上字段原样贴上 UTC 标签。连接串是 loc=UTC（datasource.go mysqlQuery），
// go-sql-driver 会先 t.In(loc) 再格式化——本地标签的 time.Now() 不重贴就落成 UTC 的墙上时间，
// 与 PG 差一个时区（2026-10-24 探针的「差 8 小时」）。
func (db *mysql) BindTime(t time.Time) any {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
}

func (db *mysql) Features() DialectFeatures {
	// XactAdvisoryLock：用锁表的行锁模拟（advisory_lock.go），语义与 PG 的 pg_advisory_xact_lock 一致。
	return DialectFeatures{FunctionalIndex: true, XactAdvisoryLock: true}
}

func (db *mysql) SupportReturning() bool {
	return false
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
		return nil, stdErrors.New("unknow version")
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
				f.typeName = TinyInt
				//f._attr_size = 1 // 1 byte
			case 16:
				f.SqlType = SQLType{SmallInt, 0, 0}
				f.typeName = SmallInt
				//f._attr_size = 2
			case 24:
				f.SqlType = SQLType{MediumInt, 0, 0}
				f.typeName = MediumInt
				//f._attr_size = 3
			case 32:
				f.SqlType = SQLType{Int, 0, 0}
				f.typeName = Int
				//f._attr_size = 4
			case 64:
				f.SqlType = SQLType{BigInt, 0, 0}
				f.typeName = BigInt
				//f._attr_size = 8
			}
		}
	}
}
func (db *mysql) GetSqlType(field IField) string {
	if field.Base().companyDependent {
		return Json // 按公司分开存：{"<公司 id>": 值}（见 field_company.go）
	}
	var res string
	var isUnsigned bool
	c := field.Base()

	switch t := c.SQLType().Name; t {
	case Bool:
		res = TinyInt
		c.size = 1

	case Int:
		switch c.SQLType().DefaultLength {
		case 4:
			res = TinyInt
			c.size = 1 // 1 byte
		case 8:
			res = SmallInt
			c.size = 2
		case 16:
			res = MediumInt
			c.size = 3
		case 32:
			res = Int
			c.size = 4
		case 64:
			res = BigInt
			c.size = 8
		default:
			// int() 不写位数：与 PG 的 INTEGER 对齐。此前这里不赋值，建表语句里出现空类型
			// （`qty` NULL COMMENT …，1064），2026-10-24 方言矩阵上 101 个用例卡在这里。
			res = Int
			c.size = 0
		}
	case Serial:
		c.isAutoIncrement = true
		c.isPrimaryKey = true
		c.required = true
		res = Int
	case BigSerial:
		c.isAutoIncrement = true
		c.isPrimaryKey = true
		c.isPrimaryKey = true
		res = BigInt
	case Bytea, Binary, VarBinary, Blob:
		// PG 的 bytea 不限大小；MySQL 的 BLOB 上限 64KB（附件 / 图片会超），BINARY 不写长度是 BINARY(1)。
		res = LongBlob
		c.size = 0
	case DateTime, TimeStamp, TimeStampz:
		// 微秒精度与 PG 的 timestamp 对齐：DATETIME 默认只到秒，写进去的值读回来与原值不等。
		// timestamptz 以前落成 CHAR(64)（xorm 遗留），按字符串比较排序都不对。MySQL 没有带时区的
		// 日期时间类型，值按连接的 loc（见 mysqlQuery）解读。
		res = DateTime
		c.size = 6
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
		c.size = 40
	case Vector:
		// MySQL 8.0 没有向量类型（9.0 才有 VECTOR）。先落 JSON：pgvector 的文本字面量 [a,b,c] 本身就是合法的
		// JSON 数组，读写原样往返；向量检索（<=>、HNSW）在 MySQL 上不可用，归 MySQL 兼容 P6。
		res = Json
	case Json, Jsonb:
		// MySQL 没有 jsonb，JSON 列本身已是解析后的二进制存储；
		// 但它没有 GIN 这类通用倒排索引，`@>` 的下推能力在这里不存在。
		res = Json
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
	case MediumText:
		res = MediumText
	case LongText:
		res = LongText
	case TinyText:
		res = TinyText
	case TinyBlob:
		res = TinyBlob
	case MediumBlob:
		res = MediumBlob
	case LongBlob:
		res = LongBlob
	default:
		res = t
	}
	if c.unbounded && (res == Varchar || res == Char) {
		res = Text
	} else if res == Varchar && c.size == 0 {
		// 不带长度的 varchar：PG 上不限长，MySQL 必须给长度。2026-10-24 定为 255；要更长的字段在标签上
		// 写 text() 或显式长度（逐字段清单见 docs/mysql/varchar_classification.md）。
		c.size = 255
	}
	c.SQLType().DefaultLength = c.size
	hasLen1 := (c.SQLType().DefaultLength > 0)
	hasLen2 := (c.SQLType().DefaultLength2 > 0)

	if res == BigInt && !hasLen1 && !hasLen2 {
		c.SQLType().DefaultLength = 20
		hasLen1 = true
	}

	noLenTypes := map[string]bool{
		Json:       true,
		TinyText:   true,
		Text:       true,
		MediumText: true,
		LongText:   true,
		TinyBlob:   true,
		Blob:       true,
		MediumBlob: true,
		LongBlob:   true,
	}

	if !noLenTypes[res] {
		if hasLen2 {
			res += "(" + utils.ToString(c.SQLType().DefaultLength) + "," + utils.ToString(c.SQLType().DefaultLength2) + ")"
		} else if hasLen1 {
			res += "(" + utils.ToString(c.SQLType().DefaultLength) + ")"
		}
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

func (db *mysql) CreateDatabase(dbx *sql.DB, ctx context.Context, name string) error {
	ds := *db.TDataSource
	ds.DbName = ""
	db_driver := ds.DbType
	db_src, err := ds.toString()
	if err != nil {
		return err
	}

	dbConn, err := sql.Open(db_driver, db_src)
	if err != nil {
		return err
	}
	defer dbConn.Close()

	query := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", db.quoter.Quote(name))
	_, err = dbConn.ExecContext(ctx, query)
	return err
}

func (db *mysql) DropDatabase(dbx *sql.DB, ctx context.Context, name string) error {
	query := fmt.Sprintf("DROP DATABASE IF EXISTS %s", db.quoter.Quote(name))
	_, err := dbx.ExecContext(ctx, query)
	return err
}

func (db *mysql) IsDatabaseExist(ctx context.Context, name string) bool {
	s := "SELECT SCHEMA_NAME FROM INFORMATION_SCHEMA.SCHEMATA WHERE SCHEMA_NAME = ?"
	db.LogSQL(s, []any{name})

	rows, err := db.queryer.QueryContext(ctx, s, name)
	if err != nil {
		return false
	}
	defer rows.Close()

	return rows.Next()
}

func (db *mysql) AutoIncrStr() string {
	return "AUTO_INCREMENT"
}

// MySQL 语义：schema 即数据库名，空 schema 退回当前库（DbName）。PG 的 public 是「默认 schema」，在 MySQL
// 上对应的就是当前库——上层显式传 "public"（与 PG 共用的代码）时同样落当前库，而不是去找一个叫 public 的库。
func (db *mysql) schemaOr(schema string) string {
	if schema != "" && schema != DefaultPostgresSchema {
		return schema
	}
	return db.DbName
}

// getSchema：会话的 schema（SetSchema）优先，没有就是当前库。建表与反查都要经它——此前写死 DbName，
// SetSchema("system") 的会话把 S 模型的表全建进了默认库，反查也只看默认库（探针去掉 SystemSchema 补丁后
// 启动即 FATAL：system.sys_tenant doesn't exist）。
func (db *mysql) getSchema(session *TSession) string {
	if session != nil {
		return db.schemaOr(session.Schema)
	}
	return db.DbName
}

// normalizeSchema（schemaNormalizer）：public 与当前库名都是「当前库」，归一成空串。
func (db *mysql) normalizeSchema(schema string) string {
	if schema == DefaultPostgresSchema || schema == db.DbName {
		return ""
	}
	return schema
}

// tableRef：schema 参数版的 qualifiedTable（"public" 与空串都是当前库，见 schemaOr）。
func (db *mysql) tableRef(schema, table string) string {
	if sch := db.schemaOr(schema); sch != db.DbName {
		return db.dialect.Quoter().QuoteTable(sch, table)
	}
	return db.dialect.Quoter().Quote(table)
}

// qualifiedTable：表在会话 schema 里的全名；就在当前库时不加限定（与此前的 DDL 逐字一致）。
func (db *mysql) qualifiedTable(session *TSession, table string) string {
	if sch := db.getSchema(session); sch != db.DbName {
		return db.dialect.Quoter().QuoteTable(sch, table)
	}
	return db.dialect.Quoter().Quote(table)
}

func (db *mysql) IndexCheckSql(schema, tableName, idxName string) (string, []any) {
	args := []any{db.schemaOr(schema), tableName, idxName}
	sql := "SELECT `INDEX_NAME` FROM `INFORMATION_SCHEMA`.`STATISTICS`"
	sql += " WHERE `TABLE_SCHEMA` = ? AND `TABLE_NAME` = ? AND `INDEX_NAME`=?"
	return sql, args
}

func (db *mysql) TableCheckSql(schema, tableName string) (string, []any) {
	args := []any{db.schemaOr(schema), tableName}
	return "SELECT `TABLE_NAME` FROM `INFORMATION_SCHEMA`.`TABLES` WHERE `TABLE_SCHEMA` = ? AND `TABLE_NAME` = ?", args
}

func (db *mysql) GenAddColumnSQL(schema, tableName string, field IField) string {
	s, _ := ColumnString(db.dialect, field, true)
	sql := fmt.Sprintf("ALTER TABLE %v ADD %v", db.tableRef(schema, tableName), s)
	if len(field.Label()) > 0 {
		sql += " COMMENT " + mysqlStringLiteral(field.Label())
	}
	return sql
}

func (db *mysql) GenInsertSql(tableName string, fields []string, uniqueFields []string, idField string, onConflict *OnConflict) string {
	var sql strings.Builder
	cnt := len(fields)
	field_places := strings.Repeat("?,", cnt-1) + "?"
	field_list := ""
	quoter := db.quoter.Quote

	// 「冲突时什么都不做」不用 INSERT IGNORE：IGNORE 把截断、NOT NULL 违例、外键错也一并降成警告，
	// 数据导入经不起。照 GORM 写成 ON DUPLICATE KEY UPDATE 某列 = 某列——只吞重复键（下面）。
	insertVerb := "INSERT"

	for i, name := range fields {
		if i < cnt-1 {
			field_list = field_list + quoter(name) + ","
		} else {
			field_list = field_list + quoter(name)
		}
	}

	sql.WriteString(insertVerb)
	sql.WriteString(" INTO ")
	sql.WriteString(tableName)
	sql.WriteString(" (")
	sql.WriteString(field_list)
	sql.WriteString(") ")
	sql.WriteString("VALUES (")
	sql.WriteString(field_places)
	sql.WriteString(") ")

	if onConflict != nil && onConflict.DoNothing {
		noop := idField
		if noop == "" && len(uniqueFields) > 0 {
			noop = uniqueFields[0]
		}
		if noop == "" && len(fields) > 0 {
			noop = fields[0]
		}
		sql.WriteString("ON DUPLICATE KEY UPDATE ")
		sql.WriteString(quoter(noop))
		sql.WriteString(" = ")
		sql.WriteString(quoter(noop))
	} else if onConflict != nil {
		sql.WriteString("ON DUPLICATE KEY UPDATE ")
		if len(onConflict.DoUpdates) > 0 {
			for idx, field := range onConflict.DoUpdates {
				if idx > 0 {
					sql.WriteByte(',')
				}
				sql.WriteString(quoter(field))
				sql.WriteString(" = ?") // Use placeholder to consume params from session_crwd.go
			}
		} else {
			sql.WriteString(quoter(idField))
			sql.WriteString(" = ")
			sql.WriteString(quoter(idField))
		}
	}

	return sql.String()
}

func (db *mysql) GetFields(ctx context.Context, session *TSession, model IModel) ([]string, map[string]IField, error) {
	args := []any{db.getSchema(session), model.Table()}
	alreadyQuoted := "(INSTR(VERSION(), 'maria') > 0 && " +
		"(SUBSTRING_INDEX(VERSION(), '.', 1) > 10 || " +
		"(SUBSTRING_INDEX(VERSION(), '.', 1) = 10 && " +
		"(SUBSTRING_INDEX(SUBSTRING(VERSION(), 4), '.', 1) > 2 || " +
		"(SUBSTRING_INDEX(SUBSTRING(VERSION(), 4), '.', 1) = 2 && " +
		"SUBSTRING_INDEX(SUBSTRING(VERSION(), 6), '-', 1) >= 7)))))"
	s := "SELECT `COLUMN_NAME`, `IS_NULLABLE`, `COLUMN_DEFAULT`, `COLUMN_TYPE`," +
		" `COLUMN_KEY`, `EXTRA`, `COLUMN_COMMENT`, `CHARACTER_MAXIMUM_LENGTH`, COALESCE(`CHARACTER_SET_NAME`, '') AS CS, " +
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
		var columnName, nullableStr, colType, colKey, extra, comment, charset string
		var alreadyQuoted, isUnsigned bool
		var colDefault, maxLength *string
		err = rows.Scan(&columnName, &nullableStr, &colDefault, &colType, &colKey, &extra, &comment, &maxLength, &charset, &alreadyQuoted)
		if err != nil {
			return nil, nil, err
		}

		// COLUMN_TYPE 形如 `int unsigned`、`varchar(255)`、`decimal(16,2) unsigned zerofill`、
		// `set('Select','Insert',...)`。修饰词在最后一个右括号之后（没有括号时在第一个空格之后）；
		// 括号里的整段是长度或选项。此前先按空格切：选项里带空格的 SET / ENUM（mysql 库里就有）把右括号
		// 切掉，下标 -1 直接 panic；`unsigned zerofill` 也认不出无符号。
		head, mods := strings.TrimSpace(colType), ""
		if j := strings.LastIndexByte(head, ')'); j >= 0 {
			head, mods = head[:j+1], head[j+1:]
		} else if i := strings.IndexByte(head, ' '); i >= 0 {
			head, mods = head[:i], head[i:]
		}
		isUnsigned = strings.Contains(strings.ToLower(mods), "unsigned")
		colName, args, hasArgs := head, "", false
		if i := strings.IndexByte(head, '('); i >= 0 && strings.HasSuffix(head, ")") {
			colName, args, hasArgs = head[:i], head[i+1:len(head)-1], true
		}
		// Remove the /* mariadb-5.3 */ suffix from coltypes
		colName = strings.TrimSuffix(colName, "/* mariadb-5.3 */")
		colType = strings.ToUpper(strings.TrimSpace(colName))

		enumOptions := make(map[string]int)
		setOptions := make(map[string]int)
		var len1, len2 int64
		if hasArgs {
			if (colType == Enum || colType == Set) && strings.HasPrefix(args, "'") {
				options := enumOptions
				if colType == Set {
					options = setOptions
				}
				for k, v := range strings.Split(args, ",") {
					options[strings.Trim(strings.TrimSpace(v), "'")] = k
				}
			} else {
				lens := strings.Split(args, ",")
				len1, err = strconv.ParseInt(strings.TrimSpace(lens[0]), 10, 64)
				if err != nil {
					return nil, nil, fmt.Errorf("column %s.%s type %q: %w", model.Table(), columnName, colType, err)
				}
				if len(lens) == 2 {
					len2, err = strconv.ParseInt(strings.TrimSpace(lens[1]), 10, 64)
					if err != nil {
						return nil, nil, fmt.Errorf("column %s.%s type %q: %w", model.Table(), columnName, colType, err)
					}
				}
			}
		}
		// TEXT / MEDIUMTEXT / LONGTEXT 不记长度（此前取 CHARACTER_MAXIMUM_LENGTH 的 65535…，模型里 text() 是 0，
		// 同一进程里第二次同步就判成「加宽」发 MODIFY——kylin 首次启动 155 条之一；类型名已足以区分）。
		if isUnsigned {
			colType = "UNSIGNED " + colType
		}

		if _, ok := SqlTypes[colType]; !ok {
			return nil, nil, fmt.Errorf("unknown colType %v (%s.%s)", colType, model.Table(), columnName)
		}

		col, err := NewField(strings.Trim(columnName, `" `), WithSQLType(SQLType{Name: colType, DefaultLength: int(len1), DefaultLength2: int(len2)}), WithModel(model))
		if err != nil {
			return nil, nil, err
		}
		col.Base().size = int(len1)
		col.Base().ascii = charset == "ascii"
		col.Base().EnumOptions = enumOptions
		col.Base().SetOptions = setOptions

		// 默认值规整成与 PG 反查同一个口径（parsePgColumn）：字符串不带引号、布尔是 true / false。
		// 此前文本默认值被包成 'x'、tinyint(1) 是 1 / 0，与模型侧的 x / false 比不上，同一进程里第二次
		// 同步每列都发 MODIFY（kylin 首次启动 155 条）。
		if colDefault != nil && strings.Contains(extra, "DEFAULT_GENERATED") && mysqlLobType(colType) {
			col.Base().SetDefault(mysqlGeneratedDefault(*colDefault))
		} else if colDefault != nil && (!alreadyQuoted || *colDefault != "NULL") {
			dv := *colDefault
			if alreadyQuoted { // MariaDB 10.2.7+：COLUMN_DEFAULT 本身带引号
				dv = strings.Trim(dv, "'")
			}
			if colType == TinyInt && len1 == 1 {
				switch dv {
				case "1":
					dv = "true"
				case "0":
					dv = "false"
				}
			}
			col.Base().SetDefault(dv)
		}

		col.Base().label = comment
		col.Base().required = (nullableStr == "NO")

		// 主键：此前 PRI 只被当成「自增」，主键标记从不设——雪花 id（pk、非自增）的表每次同步都报
		// 「主键与库里不一致」并给出 ADD PRIMARY KEY。自增只看 EXTRA。
		if colKey == "PRI" {
			col.Base().isPrimaryKey = true
			col.Base().isUnique = true
		}
		// if colKey == "UNI" {
		// col.is
		// }

		if extra == "auto_increment" {
			col.Base().isAutoIncrement = true
		}



		cols[col.Name()] = col
		colSeq = append(colSeq, col.Name())
	}

	if rows.Err() != nil {
		return nil, nil, rows.Err()
	}
	return colSeq, cols, nil
}

func (db *mysql) GetModels(ctx context.Context, session *TSession) ([]IModel, error) {
	//func (db *mysql) GetTables(queryer Queryer, ctx context.Context) ([]*Table, error) {
	args := []any{db.getSchema(session)}
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
		model := newModel("", name, model_val, model_type, nil)
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

func (db *mysql) GetIndexes(ctx context.Context, session *TSession, tableName string) (map[string]*TIndex, error) {
	args := []any{db.getSchema(session), tableName}
	// functional key parts 在 STATISTICS 里 COLUMN_NAME 为 NULL、表达式在 EXPRESSION 列
	//（8.0.13+ 才有这一列，老版本查它会直接报错）。
	withExpr := db.supportsFunctionalIndex()
	s := "SELECT `INDEX_NAME`, `NON_UNIQUE`, `COLUMN_NAME` FROM `INFORMATION_SCHEMA`.`STATISTICS` WHERE `TABLE_SCHEMA` = ? AND `TABLE_NAME` = ? ORDER BY `SEQ_IN_INDEX`"
	if withExpr {
		s = "SELECT `INDEX_NAME`, `NON_UNIQUE`, `COLUMN_NAME`, `EXPRESSION` FROM `INFORMATION_SCHEMA`.`STATISTICS` WHERE `TABLE_SCHEMA` = ? AND `TABLE_NAME` = ? ORDER BY `SEQ_IN_INDEX`"
	}

	rows, err := db.queryer.QueryContext(ctx, s, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	indexes := make(map[string]*TIndex)
	for rows.Next() {
		var indexType int
		var indexName, nonUnique string
		var colNull, exprNull sql.NullString
		if withExpr {
			err = rows.Scan(&indexName, &nonUnique, &colNull, &exprNull)
		} else {
			err = rows.Scan(&indexName, &nonUnique, &colNull)
		}
		if err != nil {
			return nil, err
		}
		colName := colNull.String

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
		if strings.HasPrefix(indexName, DefaultIndexPrefix+tableName) || strings.HasPrefix(indexName, DefaultUniquePrefix+tableName) {
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
		if !colNull.Valid && exprNull.Valid {
			index.Exprs = append(index.Exprs, exprNull.String)
			continue
		}
		if colName != "" {
			index.AddColumn(colName)
		}
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return indexes, nil
}

func (db *mysql) CreateTableSql(session *TSession, model IModel, storeEngine, charset string) string {

	//func (db *mysql) CreateTableSQL(ctx context.Context, table *Table, tableName string) (string, bool, error) {

	quoter := db.dialect.Quoter()
	var b strings.Builder
	b.WriteString("CREATE TABLE IF NOT EXISTS ")
	b.WriteString(db.qualifiedTable(session, fmtTableName(model.String())))
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

		if len(field.Label()) > 0 {
			b.WriteString(" COMMENT ")
			b.WriteString(mysqlStringLiteral(field.Label()))
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
		b.WriteString(" COMMENT=")
		b.WriteString(mysqlStringLiteral(model.GetTableDescription()))
	}

	return b.String()
}

func (db *mysql) Fmter() []IFmter {
	return []IFmter{}
}

// MapError 把 MySQL driver 错误翻译为 ormerr sentinel
// LockClause MySQL 的行锁语法与 PG 有三处不同，故覆写：
//   - `OF <别名>` 直到 8.0.1 才有，且 MySQL 不会因为 LEFT JOIN 的可空侧拒绝
//     FOR UPDATE(PG 会)，所以这里直接忽略 tableAlias，锁语句涉及的全部表。
//   - 共享锁在 5.7 只有 `LOCK IN SHARE MODE`；8.0.1 起才有 `FOR SHARE`。
//     默认发前者(两个版本都认)，只有需要 NOWAIT/SKIP LOCKED 时才发后者。
//   - NOWAIT / SKIP LOCKED 同样是 8.0.1+；低版本会报语法错误，这是有意的：
//     悄悄降级成阻塞等待会让调用方以为拿到了非阻塞语义。
func (db *mysql) LockClause(lock *TLock, tableAlias string) (string, error) {
	if !lock.IsLocking() {
		return "", nil
	}

	var b strings.Builder
	switch lock.Mode {
	case LockUpdate:
		b.WriteString("FOR UPDATE")
	case LockShare:
		if lock.Wait == LockWaitBlock {
			return "LOCK IN SHARE MODE", nil
		}
		b.WriteString("FOR SHARE")
	default:
		return "", fmt.Errorf("orm: unknown lock mode %d", lock.Mode)
	}

	switch lock.Wait {
	case LockWaitNoWait:
		b.WriteString(" NOWAIT")
	case LockWaitSkip:
		b.WriteString(" SKIP LOCKED")
	}

	return b.String(), nil
}

// badNullColumn 从 "Column 'name' cannot be null" 里取出列名；取不到返回空串。
func badNullColumn(msg string) string {
	i := strings.Index(msg, "'")
	if i < 0 {
		return ""
	}
	j := strings.Index(msg[i+1:], "'")
	if j < 0 {
		return ""
	}
	return msg[i+1 : i+1+j]
}

// mysqlErrColumn 从 MySQL 错误原文里取列名。消息形状不一："Column 'x' cannot be null" /
// "Field 'x' doesn't have a default value" / "Data too long for column 'x' at row 1" /
// "Incorrect integer value: 'abc' for column 'x' at row 1"——最后一种第一个引号里是值不是列，
// 所以先认 "column '"，再退回第一个引号。
func mysqlErrColumn(msg string) string {
	if i := strings.Index(strings.ToLower(msg), "column '"); i >= 0 {
		return badNullColumn(msg[i:])
	}
	return badNullColumn(msg)
}

func (db *mysql) MapError(err error) error {
	if err == nil {
		return nil
	}

	var me *mysqldriver.MySQLError
	if stdErrors.As(err, &me) {
		switch me.Number {
		case 1062: // ER_DUP_ENTRY
			return ormerr.New(ormerr.ErrDuplicate, err)
		case 1061: // ER_DUP_KEYNAME：索引名已在（见 ErrIndexExists）
			return ormerr.New(ormerr.ErrIndexExists, err)
		case 1213, 1205, 3572: // ER_LOCK_DEADLOCK / ER_LOCK_WAIT_TIMEOUT / ER_LOCK_NOWAIT
			// 3572：FOR UPDATE NOWAIT 抢锁失败。与死锁同属"并发没抢到"，
			// 归 ErrConflict 让调用方能用同一条 errors.Is 分支重试。
			return ormerr.New(ormerr.ErrConflict, err)
		case 2006, 2013: // CR_SERVER_GONE_ERROR / CR_SERVER_LOST
			return ormerr.New(ormerr.ErrConnection, err)
		case 1048: // ER_BAD_NULL_ERROR: Column 'x' cannot be null
			// 必填。此前根本没映射，整条驱动原文一路走到出口被脱敏。
			// 理由同 postgres 的 23502。
			e := ormerr.New(ormerr.ErrRequired, err)
			if col := badNullColumn(me.Message); col != "" {
				e = e.WithFields(col)
			}
			return e
		case 1452, 1451: // ER_NO_REFERENCED_ROW_2 / ER_ROW_IS_REFERENCED_2
			return ormerr.New(ormerr.ErrValidation, err)
		case 1364: // ER_NO_DEFAULT_FOR_FIELD: Field 'x' doesn't have a default value
			// 严格模式下 INSERT 漏了一个 NOT NULL 且无默认值的列。PG 上同一件事是 23502，归必填。
			e := ormerr.New(ormerr.ErrRequired, err)
			if col := mysqlErrColumn(me.Message); col != "" {
				e = e.WithFields(col)
			}
			return e
		case 1406, 1264, 1265, 1366, 1292:
			// ER_DATA_TOO_LONG / ER_WARN_DATA_OUT_OF_RANGE / WARN_DATA_TRUNCATED /
			// ER_TRUNCATED_WRONG_VALUE_FOR_FIELD / ER_TRUNCATED_WRONG_VALUE：值放不进这一列。
			// 严格 sql_mode（datasource.go MySQLSqlMode）下这些是错误而不是警告——非严格时 MySQL
			// 会静默截断后照写。PG 上对应 22001 / 22003 / 22P02 / 22007。
			e := ormerr.New(ormerr.ErrValidation, err)
			if col := mysqlErrColumn(me.Message); col != "" {
				e = e.WithField(col)
			}
			return e
		case 3988: // ER_CANNOT_CONVERT_STRING：参数换不进列的字符集（非 ASCII 写进 ascii 列，见 TAG_ASCII）
			return ormerr.New(ormerr.ErrValidation, err)
		case 3024, 1317: // ER_QUERY_TIMEOUT（max_execution_time）/ ER_QUERY_INTERRUPTED（KILL QUERY）
			// 与 PG 的 57014 同一类：库这边把语句收掉了。
			return ormerr.New(ormerr.ErrTimeout, err)
		case 4031: // ER_CLIENT_INTERACTION_TIMEOUT：wait_timeout 到点，服务端断了这条空闲连接
			return ormerr.New(ormerr.ErrConnection, err)
		}
	}
	// 驱动自己发现连接坏了（服务端已断、包序错乱）：不是 MySQLError。
	if stdErrors.Is(err, mysqldriver.ErrInvalidConn) || stdErrors.Is(err, driver.ErrBadConn) {
		return ormerr.New(ormerr.ErrConnection, err)
	}

	if stdErrors.Is(err, context.DeadlineExceeded) {
		return ormerr.New(ormerr.ErrTimeout, err)
	}
	if stdErrors.Is(err, sql.ErrNoRows) {
		return ormerr.New(ormerr.ErrNotFound, err)
	}
	return err
}
