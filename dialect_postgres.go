package orm

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/volts-dev/orm/logger"
	"github.com/volts-dev/utils"
)

const (
	POSTGRES = "postgres"
)

// from http://www.postgresql.org/docs/current/static/sql-keywords-appendix.html
var (
	postgresReservedWords = map[string]bool{
		"A":                                true,
		"ABORT":                            true,
		"ABS":                              true,
		"ABSENT":                           true,
		"ABSOLUTE":                         true,
		"ACCESS":                           true,
		"ACCORDING":                        true,
		"ACTION":                           true,
		"ADA":                              true,
		"ADD":                              true,
		"ADMIN":                            true,
		"AFTER":                            true,
		"AGGREGATE":                        true,
		"ALL":                              true,
		"ALLOCATE":                         true,
		"ALSO":                             true,
		"ALTER":                            true,
		"ALWAYS":                           true,
		"ANALYSE":                          true,
		"ANALYZE":                          true,
		"AND":                              true,
		"ANY":                              true,
		"ARE":                              true,
		"ARRAY":                            true,
		"ARRAY_AGG":                        true,
		"ARRAY_MAX_CARDINALITY":            true,
		"AS":                               true,
		"ASC":                              true,
		"ASENSITIVE":                       true,
		"ASSERTION":                        true,
		"ASSIGNMENT":                       true,
		"ASYMMETRIC":                       true,
		"AT":                               true,
		"ATOMIC":                           true,
		"ATTRIBUTE":                        true,
		"ATTRIBUTES":                       true,
		"AUTHORIZATION":                    true,
		"AVG":                              true,
		"BACKWARD":                         true,
		"BASE64":                           true,
		"BEFORE":                           true,
		"BEGIN":                            true,
		"BEGIN_FRAME":                      true,
		"BEGIN_PARTITION":                  true,
		"BERNOULLI":                        true,
		"BETWEEN":                          true,
		"BIGINT":                           true,
		"BINARY":                           true,
		"BIT":                              true,
		"BIT_LENGTH":                       true,
		"BLOB":                             true,
		"BLOCKED":                          true,
		"BOM":                              true,
		"BOOLEAN":                          true,
		"BOTH":                             true,
		"BREADTH":                          true,
		"BY":                               true,
		"C":                                true,
		"CACHE":                            true,
		"CALL":                             true,
		"CALLED":                           true,
		"CARDINALITY":                      true,
		"CASCADE":                          true,
		"CASCADED":                         true,
		"CASE":                             true,
		"CAST":                             true,
		"CATALOG":                          true,
		"CATALOG_NAME":                     true,
		"CEIL":                             true,
		"CEILING":                          true,
		"CHAIN":                            true,
		"CHAR":                             true,
		"CHARACTER":                        true,
		"CHARACTERISTICS":                  true,
		"CHARACTERS":                       true,
		"CHARACTER_LENGTH":                 true,
		"CHARACTER_SET_CATALOG":            true,
		"CHARACTER_SET_NAME":               true,
		"CHARACTER_SET_SCHEMA":             true,
		"CHAR_LENGTH":                      true,
		"CHECK":                            true,
		"CHECKPOINT":                       true,
		"CLASS":                            true,
		"CLASS_ORIGIN":                     true,
		"CLOB":                             true,
		"CLOSE":                            true,
		"CLUSTER":                          true,
		"COALESCE":                         true,
		"COBOL":                            true,
		"COLLATE":                          true,
		"COLLATION":                        true,
		"COLLATION_CATALOG":                true,
		"COLLATION_NAME":                   true,
		"COLLATION_SCHEMA":                 true,
		"COLLECT":                          true,
		"COLUMN":                           true,
		"COLUMNS":                          true,
		"COLUMN_NAME":                      true,
		"COMMAND_FUNCTION":                 true,
		"COMMAND_FUNCTION_CODE":            true,
		"COMMENT":                          true,
		"COMMENTS":                         true,
		"COMMIT":                           true,
		"COMMITTED":                        true,
		"CONCURRENTLY":                     true,
		"CONDITION":                        true,
		"CONDITION_NUMBER":                 true,
		"CONFIGURATION":                    true,
		"CONNECT":                          true,
		"CONNECTION":                       true,
		"CONNECTION_NAME":                  true,
		"CONSTRAINT":                       true,
		"CONSTRAINTS":                      true,
		"CONSTRAINT_CATALOG":               true,
		"CONSTRAINT_NAME":                  true,
		"CONSTRAINT_SCHEMA":                true,
		"CONSTRUCTOR":                      true,
		"CONTAINS":                         true,
		"CONTENT":                          true,
		"CONTINUE":                         true,
		"CONTROL":                          true,
		"CONVERSION":                       true,
		"CONVERT":                          true,
		"COPY":                             true,
		"CORR":                             true,
		"CORRESPONDING":                    true,
		"COST":                             true,
		"COUNT":                            true,
		"COVAR_POP":                        true,
		"COVAR_SAMP":                       true,
		"CREATE":                           true,
		"CROSS":                            true,
		"CSV":                              true,
		"CUBE":                             true,
		"CUME_DIST":                        true,
		"CURRENT":                          true,
		"CURRENT_CATALOG":                  true,
		"CURRENT_DATE":                     true,
		"CURRENT_DEFAULT_TRANSFORM_GROUP":  true,
		"CURRENT_PATH":                     true,
		"CURRENT_ROLE":                     true,
		"CURRENT_ROW":                      true,
		"CURRENT_SCHEMA":                   true,
		"CURRENT_TIME":                     true,
		"CURRENT_TIMESTAMP":                true,
		"CURRENT_TRANSFORM_GROUP_FOR_TYPE": true,
		"CURRENT_USER":                     true,
		"CURSOR":                           true,
		"CURSOR_NAME":                      true,
		"CYCLE":                            true,
		"DATA":                             true,
		"DATABASE":                         true,
		"DATALINK":                         true,
		"DATE":                             true,
		"DATETIME_INTERVAL_CODE":           true,
		"DATETIME_INTERVAL_PRECISION":      true,
		"DAY":                              true,
		"DB":                               true,
		"DEALLOCATE":                       true,
		"DEC":                              true,
		"DECIMAL":                          true,
		"DECLARE":                          true,
		"DEFAULT":                          true,
		"DEFAULTS":                         true,
		"DEFERRABLE":                       true,
		"DEFERRED":                         true,
		"DEFINED":                          true,
		"DEFINER":                          true,
		"DEGREE":                           true,
		"DELETE":                           true,
		"DELIMITER":                        true,
		"DELIMITERS":                       true,
		"DENSE_RANK":                       true,
		"DEPTH":                            true,
		"DEREF":                            true,
		"DERIVED":                          true,
		"DESC":                             true,
		"DESCRIBE":                         true,
		"DESCRIPTOR":                       true,
		"DETERMINISTIC":                    true,
		"DIAGNOSTICS":                      true,
		"DICTIONARY":                       true,
		"DISABLE":                          true,
		"DISCARD":                          true,
		"DISCONNECT":                       true,
		"DISPATCH":                         true,
		"DISTINCT":                         true,
		"DLNEWCOPY":                        true,
		"DLPREVIOUSCOPY":                   true,
		"DLURLCOMPLETE":                    true,
		"DLURLCOMPLETEONLY":                true,
		"DLURLCOMPLETEWRITE":               true,
		"DLURLPATH":                        true,
		"DLURLPATHONLY":                    true,
		"DLURLPATHWRITE":                   true,
		"DLURLSCHEME":                      true,
		"DLURLSERVER":                      true,
		"DLVALUE":                          true,
		"DO":                               true,
		"DOCUMENT":                         true,
		"DOMAIN":                           true,
		"DOUBLE":                           true,
		"DROP":                             true,
		"DYNAMIC":                          true,
		"DYNAMIC_FUNCTION":                 true,
		"DYNAMIC_FUNCTION_CODE":            true,
		"EACH":                             true,
		"ELEMENT":                          true,
		"ELSE":                             true,
		"EMPTY":                            true,
		"ENABLE":                           true,
		"ENCODING":                         true,
		"ENCRYPTED":                        true,
		"END":                              true,
		"END-EXEC":                         true,
		"END_FRAME":                        true,
		"END_PARTITION":                    true,
		"ENFORCED":                         true,
		"ENUM":                             true,
		"EQUALS":                           true,
		"ESCAPE":                           true,
		"EVENT":                            true,
		"EVERY":                            true,
		"EXCEPT":                           true,
		"EXCEPTION":                        true,
		"EXCLUDE":                          true,
		"EXCLUDING":                        true,
		"EXCLUSIVE":                        true,
		"EXEC":                             true,
		"EXECUTE":                          true,
		"EXISTS":                           true,
		"EXP":                              true,
		"EXPLAIN":                          true,
		"EXPRESSION":                       true,
		"EXTENSION":                        true,
		"EXTERNAL":                         true,
		"EXTRACT":                          true,
		"FALSE":                            true,
		"FAMILY":                           true,
		"FETCH":                            true,
		"FILE":                             true,
		"FILTER":                           true,
		"FINAL":                            true,
		"FIRST":                            true,
		"FIRST_VALUE":                      true,
		"FLAG":                             true,
		"FLOAT":                            true,
		"FLOOR":                            true,
		"FOLLOWING":                        true,
		"FOR":                              true,
		"FORCE":                            true,
		"FOREIGN":                          true,
		"FORTRAN":                          true,
		"FORWARD":                          true,
		"FOUND":                            true,
		"FRAME_ROW":                        true,
		"FREE":                             true,
		"FREEZE":                           true,
		"FROM":                             true,
		"FS":                               true,
		"FULL":                             true,
		"FUNCTION":                         true,
		"FUNCTIONS":                        true,
		"FUSION":                           true,
		"G":                                true,
		"GENERAL":                          true,
		"GENERATED":                        true,
		"GET":                              true,
		"GLOBAL":                           true,
		"GO":                               true,
		"GOTO":                             true,
		"GRANT":                            true,
		"GRANTED":                          true,
		"GREATEST":                         true,
		"GROUP":                            true,
		"GROUPING":                         true,
		"GROUPS":                           true,
		"HANDLER":                          true,
		"HAVING":                           true,
		"HEADER":                           true,
		"HEX":                              true,
		"HIERARCHY":                        true,
		"HOLD":                             true,
		"HOUR":                             true,
		"ID":                               true,
		"IDENTITY":                         true,
		"IF":                               true,
		"IGNORE":                           true,
		"ILIKE":                            true,
		"IMMEDIATE":                        true,
		"IMMEDIATELY":                      true,
		"IMMUTABLE":                        true,
		"IMPLEMENTATION":                   true,
		"IMPLICIT":                         true,
		"IMPORT":                           true,
		"IN":                               true,
		"INCLUDING":                        true,
		"INCREMENT":                        true,
		"INDENT":                           true,
		"INDEX":                            true,
		"INDEXES":                          true,
		"INDICATOR":                        true,
		"INHERIT":                          true,
		"INHERITS":                         true,
		"INITIALLY":                        true,
		"INLINE":                           true,
		"INNER":                            true,
		"INOUT":                            true,
		"INPUT":                            true,
		"INSENSITIVE":                      true,
		"INSERT":                           true,
		"INSTANCE":                         true,
		"INSTANTIABLE":                     true,
		"INSTEAD":                          true,
		"INT":                              true,
		"INTEGER":                          true,
		"INTEGRITY":                        true,
		"INTERSECT":                        true,
		"INTERSECTION":                     true,
		"INTERVAL":                         true,
		"INTO":                             true,
		"INVOKER":                          true,
		"IS":                               true,
		"ISNULL":                           true,
		"ISOLATION":                        true,
		"JOIN":                             true,
		"K":                                true,
		"KEY":                              true,
		"KEY_MEMBER":                       true,
		"KEY_TYPE":                         true,
		"LABEL":                            true,
		"LAG":                              true,
		"LANGUAGE":                         true,
		"LARGE":                            true,
		"LAST":                             true,
		"LAST_VALUE":                       true,
		"LATERAL":                          true,
		"LC_COLLATE":                       true,
		"LC_CTYPE":                         true,
		"LEAD":                             true,
		"LEADING":                          true,
		"LEAKPROOF":                        true,
		"LEAST":                            true,
		"LEFT":                             true,
		"LENGTH":                           true,
		"LEVEL":                            true,
		"LIBRARY":                          true,
		"LIKE":                             true,
		"LIKE_REGEX":                       true,
		"LIMIT":                            true,
		"LINK":                             true,
		"LISTEN":                           true,
		"LN":                               true,
		"LOAD":                             true,
		"LOCAL":                            true,
		"LOCALTIME":                        true,
		"LOCALTIMESTAMP":                   true,
		"LOCATION":                         true,
		"LOCATOR":                          true,
		"LOCK":                             true,
		"LOWER":                            true,
		"M":                                true,
		"MAP":                              true,
		"MAPPING":                          true,
		"MATCH":                            true,
		"MATCHED":                          true,
		"MATERIALIZED":                     true,
		"MAX":                              true,
		"MAXVALUE":                         true,
		"MAX_CARDINALITY":                  true,
		"MEMBER":                           true,
		"MERGE":                            true,
		"MESSAGE_LENGTH":                   true,
		"MESSAGE_OCTET_LENGTH":             true,
		"MESSAGE_TEXT":                     true,
		"METHOD":                           true,
		"MIN":                              true,
		"MINUTE":                           true,
		"MINVALUE":                         true,
		"MOD":                              true,
		"MODE":                             true,
		"MODIFIES":                         true,
		"MODULE":                           true,
		"MONTH":                            true,
		"MORE":                             true,
		"MOVE":                             true,
		"MULTISET":                         true,
		"MUMPS":                            true,
		"NAME":                             true,
		"NAMES":                            true,
		"NAMESPACE":                        true,
		"NATIONAL":                         true,
		"NATURAL":                          true,
		"NCHAR":                            true,
		"NCLOB":                            true,
		"NESTING":                          true,
		"NEW":                              true,
		"NEXT":                             true,
		"NFC":                              true,
		"NFD":                              true,
		"NFKC":                             true,
		"NFKD":                             true,
		"NIL":                              true,
		"NO":                               true,
		"NONE":                             true,
		"NORMALIZE":                        true,
		"NORMALIZED":                       true,
		"NOT":                              true,
		"NOTHING":                          true,
		"NOTIFY":                           true,
		"NOTNULL":                          true,
		"NOWAIT":                           true,
		"NTH_VALUE":                        true,
		"NTILE":                            true,
		"NULL":                             true,
		"NULLABLE":                         true,
		"NULLIF":                           true,
		"NULLS":                            true,
		"NUMBER":                           true,
		"NUMERIC":                          true,
		"OBJECT":                           true,
		"OCCURRENCES_REGEX":                true,
		"OCTETS":                           true,
		"OCTET_LENGTH":                     true,
		"OF":                               true,
		"OFF":                              true,
		"OFFSET":                           true,
		"OIDS":                             true,
		"OLD":                              true,
		"ON":                               true,
		"ONLY":                             true,
		"OPEN":                             true,
		"OPERATOR":                         true,
		"OPTION":                           true,
		"OPTIONS":                          true,
		"OR":                               true,
		"ORDER":                            true,
		"ORDERING":                         true,
		"ORDINALITY":                       true,
		"OTHERS":                           true,
		"OUT":                              true,
		"OUTER":                            true,
		"OUTPUT":                           true,
		"OVER":                             true,
		"OVERLAPS":                         true,
		"OVERLAY":                          true,
		"OVERRIDING":                       true,
		"OWNED":                            true,
		"OWNER":                            true,
		"P":                                true,
		"PAD":                              true,
		"PARAMETER":                        true,
		"PARAMETER_MODE":                   true,
		"PARAMETER_NAME":                   true,
		"PARAMETER_ORDINAL_POSITION":       true,
		"PARAMETER_SPECIFIC_CATALOG":       true,
		"PARAMETER_SPECIFIC_NAME":          true,
		"PARAMETER_SPECIFIC_SCHEMA":        true,
		"PARSER":                           true,
		"PARTIAL":                          true,
		"PARTITION":                        true,
		"PASCAL":                           true,
		"PASSING":                          true,
		"PASSTHROUGH":                      true,
		"PASSWORD":                         true,
		"PATH":                             true,
		"PERCENT":                          true,
		"PERCENTILE_CONT":                  true,
		"PERCENTILE_DISC":                  true,
		"PERCENT_RANK":                     true,
		"PERIOD":                           true,
		"PERMISSION":                       true,
		"PLACING":                          true,
		"PLANS":                            true,
		"PLI":                              true,
		"PORTION":                          true,
		"POSITION":                         true,
		"POSITION_REGEX":                   true,
		"POWER":                            true,
		"PRECEDES":                         true,
		"PRECEDING":                        true,
		"PRECISION":                        true,
		"PREPARE":                          true,
		"PREPARED":                         true,
		"PRESERVE":                         true,
		"PRIMARY":                          true,
		"PRIOR":                            true,
		"PRIVILEGES":                       true,
		"PROCEDURAL":                       true,
		"PROCEDURE":                        true,
		"PROGRAM":                          true,
		"PUBLIC":                           true,
		"QUOTE":                            true,
		"RANGE":                            true,
		"RANK":                             true,
		"READ":                             true,
		"READS":                            true,
		"REAL":                             true,
		"REASSIGN":                         true,
		"RECHECK":                          true,
		"RECOVERY":                         true,
		"RECURSIVE":                        true,
		"REF":                              true,
		"REFERENCES":                       true,
		"REFERENCING":                      true,
		"REFRESH":                          true,
		"REGR_AVGX":                        true,
		"REGR_AVGY":                        true,
		"REGR_COUNT":                       true,
		"REGR_INTERCEPT":                   true,
		"REGR_R2":                          true,
		"REGR_SLOPE":                       true,
		"REGR_SXX":                         true,
		"REGR_SXY":                         true,
		"REGR_SYY":                         true,
		"REINDEX":                          true,
		"RELATIVE":                         true,
		"RELEASE":                          true,
		"RENAME":                           true,
		"REPEATABLE":                       true,
		"REPLACE":                          true,
		"REPLICA":                          true,
		"REQUIRING":                        true,
		"RESET":                            true,
		"RESPECT":                          true,
		"RESTART":                          true,
		"RESTORE":                          true,
		"RESTRICT":                         true,
		"RESULT":                           true,
		"RETURN":                           true,
		"RETURNED_CARDINALITY":             true,
		"RETURNED_LENGTH":                  true,
		"RETURNED_OCTET_LENGTH":            true,
		"RETURNED_SQLSTATE":                true,
		"RETURNING":                        true,
		"RETURNS":                          true,
		"REVOKE":                           true,
		"RIGHT":                            true,
		"ROLE":                             true,
		"ROLLBACK":                         true,
		"ROLLUP":                           true,
		"ROUTINE":                          true,
		"ROUTINE_CATALOG":                  true,
		"ROUTINE_NAME":                     true,
		"ROUTINE_SCHEMA":                   true,
		"ROW":                              true,
		"ROWS":                             true,
		"ROW_COUNT":                        true,
		"ROW_NUMBER":                       true,
		"RULE":                             true,
		"SAVEPOINT":                        true,
		"SCALE":                            true,
		"SCHEMA":                           true,
		"SCHEMA_NAME":                      true,
		"SCOPE":                            true,
		"SCOPE_CATALOG":                    true,
		"SCOPE_NAME":                       true,
		"SCOPE_SCHEMA":                     true,
		"SCROLL":                           true,
		"SEARCH":                           true,
		"SECOND":                           true,
		"SECTION":                          true,
		"SECURITY":                         true,
		"SELECT":                           true,
		"SELECTIVE":                        true,
		"SELF":                             true,
		"SENSITIVE":                        true,
		"SEQUENCE":                         true,
		"SEQUENCES":                        true,
		"SERIALIZABLE":                     true,
		"SERVER":                           true,
		"SERVER_NAME":                      true,
		"SESSION":                          true,
		"SESSION_USER":                     true,
		"SET":                              true,
		"SETOF":                            true,
		"SETS":                             true,
		"SHARE":                            true,
		"SHOW":                             true,
		"SIMILAR":                          true,
		"SIMPLE":                           true,
		"SIZE":                             true,
		"SMALLINT":                         true,
		"SNAPSHOT":                         true,
		"SOME":                             true,
		"SOURCE":                           true,
		"SPACE":                            true,
		"SPECIFIC":                         true,
		"SPECIFICTYPE":                     true,
		"SPECIFIC_NAME":                    true,
		"SQL":                              true,
		"SQLCODE":                          true,
		"SQLERROR":                         true,
		"SQLEXCEPTION":                     true,
		"SQLSTATE":                         true,
		"SQLWARNING":                       true,
		"SQRT":                             true,
		"STABLE":                           true,
		"STANDALONE":                       true,
		"START":                            true,
		"STATE":                            true,
		"STATEMENT":                        true,
		"STATIC":                           true,
		"STATISTICS":                       true,
		"STDDEV_POP":                       true,
		"STDDEV_SAMP":                      true,
		"STDIN":                            true,
		"STDOUT":                           true,
		"STORAGE":                          true,
		"STRICT":                           true,
		"STRIP":                            true,
		"STRUCTURE":                        true,
		"STYLE":                            true,
		"SUBCLASS_ORIGIN":                  true,
		"SUBMULTISET":                      true,
		"SUBSTRING":                        true,
		"SUBSTRING_REGEX":                  true,
		"SUCCEEDS":                         true,
		"SUM":                              true,
		"SYMMETRIC":                        true,
		"SYSID":                            true,
		"SYSTEM":                           true,
		"SYSTEM_TIME":                      true,
		"SYSTEM_USER":                      true,
		"T":                                true,
		"TABLE":                            true,
		"TABLES":                           true,
		"TABLESAMPLE":                      true,
		"TABLESPACE":                       true,
		"TABLE_NAME":                       true,
		"TEMP":                             true,
		"TEMPLATE":                         true,
		"TEMPORARY":                        true,
		"TEXT":                             true,
		"THEN":                             true,
		"TIES":                             true,
		"TIME":                             true,
		"TIMESTAMP":                        true,
		"TIMEZONE_HOUR":                    true,
		"TIMEZONE_MINUTE":                  true,
		"TO":                               true,
		"TOKEN":                            true,
		"TOP_LEVEL_COUNT":                  true,
		"TRAILING":                         true,
		"TRANSACTION":                      true,
		"TRANSACTIONS_COMMITTED":           true,
		"TRANSACTIONS_ROLLED_BACK":         true,
		"TRANSACTION_ACTIVE":               true,
		"TRANSFORM":                        true,
		"TRANSFORMS":                       true,
		"TRANSLATE":                        true,
		"TRANSLATE_REGEX":                  true,
		"TRANSLATION":                      true,
		"TREAT":                            true,
		"TRIGGER":                          true,
		"TRIGGER_CATALOG":                  true,
		"TRIGGER_NAME":                     true,
		"TRIGGER_SCHEMA":                   true,
		"TRIM":                             true,
		"TRIM_ARRAY":                       true,
		"TRUE":                             true,
		"TRUNCATE":                         true,
		"TRUSTED":                          true,
		"TYPE":                             true,
		"TYPES":                            true,
		"UESCAPE":                          true,
		"UNBOUNDED":                        true,
		"UNCOMMITTED":                      true,
		"UNDER":                            true,
		"UNENCRYPTED":                      true,
		"UNION":                            true,
		"UNIQUE":                           true,
		"UNKNOWN":                          true,
		"UNLINK":                           true,
		"UNLISTEN":                         true,
		"UNLOGGED":                         true,
		"UNNAMED":                          true,
		"UNNEST":                           true,
		"UNTIL":                            true,
		"UNTYPED":                          true,
		"UPDATE":                           true,
		"UPPER":                            true,
		"URI":                              true,
		"USAGE":                            true,
		"USER":                             true,
		"USER_DEFINED_TYPE_CATALOG":        true,
		"USER_DEFINED_TYPE_CODE":           true,
		"USER_DEFINED_TYPE_NAME":           true,
		"USER_DEFINED_TYPE_SCHEMA":         true,
		"USING":                            true,
		"VACUUM":                           true,
		"VALID":                            true,
		"VALIDATE":                         true,
		"VALIDATOR":                        true,
		"VALUE":                            true,
		"VALUES":                           true,
		"VALUE_OF":                         true,
		"VARBINARY":                        true,
		"VARCHAR":                          true,
		"VARIADIC":                         true,
		"VARYING":                          true,
		"VAR_POP":                          true,
		"VAR_SAMP":                         true,
		"VERBOSE":                          true,
		"VERSION":                          true,
		"VERSIONING":                       true,
		"VIEW":                             true,
		"VOLATILE":                         true,
		"WHEN":                             true,
		"WHENEVER":                         true,
		"WHERE":                            true,
		"WHITESPACE":                       true,
		"WIDTH_BUCKET":                     true,
		"WINDOW":                           true,
		"WITH":                             true,
		"WITHIN":                           true,
		"WITHOUT":                          true,
		"WORK":                             true,
		"WRAPPER":                          true,
		"WRITE":                            true,
		"XML":                              true,
		"XMLAGG":                           true,
		"XMLATTRIBUTES":                    true,
		"XMLBINARY":                        true,
		"XMLCAST":                          true,
		"XMLCOMMENT":                       true,
		"XMLCONCAT":                        true,
		"XMLDECLARATION":                   true,
		"XMLDOCUMENT":                      true,
		"XMLELEMENT":                       true,
		"XMLEXISTS":                        true,
		"XMLFOREST":                        true,
		"XMLITERATE":                       true,
		"XMLNAMESPACES":                    true,
		"XMLPARSE":                         true,
		"XMLPI":                            true,
		"XMLQUERY":                         true,
		"XMLROOT":                          true,
		"XMLSCHEMA":                        true,
		"XMLSERIALIZE":                     true,
		"XMLTABLE":                         true,
		"XMLTEXT":                          true,
		"XMLVALIDATE":                      true,
		"YEAR":                             true,
		"YES":                              true,
		"ZONE":                             true,
	}
)

type postgres struct {
	TDialect
}

func init() {
	RegisterDialect("postgres", postgresDialect)
}

func postgresDialect() IDialect {
	return &postgres{}
}

func (db *postgres) Init(d *sql.DB, uri *TDataSource) error {
	return db.TDialect.Init(d, db, uri)
}

// 生成插入SQL句子
func (db *postgres) GenInsertSql(model string, fields []string, idField string) (sql string, isquery bool) {
	cnt := len(fields)
	field_places := strings.Repeat("?,", cnt-1) + "?"
	field_list := ""

	for i, name := range fields {
		if i < cnt-1 {
			field_list = field_list + db.Quote(name) + ","
		} else {
			field_list = field_list + db.Quote(name)
		}
	}

	sql = fmt.Sprintf("INSERT INTO %s (%v) VALUES (%v)",
		db.Quote(model),
		field_list,
		field_places,
	)

	if len(idField) > 0 {
		sql = sql + " RETURNING " + db.Quote(idField)
	}

	return sql, true
}

//
func (db *postgres) GenSqlType(field IField) string {
	var res string
	c := field.Base()
	switch t := c.SqlType.Name; t {
	case TinyInt:
		return SmallInt
	case MediumInt, Int, Integer:
		if c.isAutoIncrement {
			return Serial
		}
		return Integer
	case Serial, BigSerial:
		c.isAutoIncrement = true
		c._attr_required = true
		res = t
	case Binary, VarBinary:
		return Bytea
	case DateTime:
		res = TimeStamp
	case TimeStampz:
		return "timestamp with time zone"
	case Float:
		res = Real
	case TinyText, MediumText, LongText:
		res = Text
	case NVarchar:
		res = Varchar
	case Uuid:
		res = Uuid
	case Blob, TinyBlob, MediumBlob, LongBlob:
		return Bytea
	case Double:
		return "DOUBLE PRECISION"
	default:
		if c.isAutoIncrement {
			return Serial
		}
		res = t
	}

	var hasLen1 bool = (c._attr_size > 0)
	if hasLen1 {
		res += "(" + strconv.Itoa(c._attr_size) + ")"
	}

	return res
}

func (db *postgres) SupportInsertMany() bool {
	return true
}

func (db *postgres) IsReserved(name string) bool {
	_, ok := postgresReservedWords[name]
	return ok
}

func (db *postgres) Quote(name string) string {
	name = strings.Replace(name, ".", `"."`, -1)
	return "\"" + name + "\""
}

func (db *postgres) QuoteStr() string {
	return "\""
}

func (db *postgres) AutoIncrStr() string {
	return ""
}

func (db *postgres) SupportEngine() bool {
	return false
}

func (db *postgres) SupportCharset() bool {
	return false
}

func (db *postgres) IndexOnTable() bool {
	return false
}

func (db *postgres) IndexCheckSql(tableName, idxName string) (string, []interface{}) {
	args := []interface{}{tableName, idxName}
	return `SELECT indexname FROM pg_indexes ` +
		`WHERE tablename = ? AND indexname = ?`, args
}

func (db *postgres) TableCheckSql(tableName string) (string, []interface{}) {
	args := []interface{}{tableName}
	return `SELECT tablename FROM pg_tables WHERE tablename = $1`, args
}

/*func (db *postgres) ColumnCheckSql(tableName, colName string) (string, []interface{}) {
	args := []interface{}{tableName, colName}
	return "SELECT column_name FROM INFORMATION_SCHEMA.COLUMNS WHERE table_name = ?" +
		" AND column_name = ?", args
}*/

func (db *postgres) ModifyColumnSql(tableName string, field IField) string {
	return fmt.Sprintf("alter table %s ALTER COLUMN %s TYPE %s",
		tableName, field.Name(), db.GenSqlType(field))
}

func (db *postgres) IsDatabaseExist(name string) bool {
	s := fmt.Sprintf("SELECT datname FROM pg_database WHERE datname = $1")
	db.LogSQL(s, []interface{}{name})

	rows, err := db.DB().Query(s, name)
	if err != nil {
		return false
	}
	defer rows.Close()

	for rows.Next() {
		var database_name string
		err = rows.Scan(&database_name)
		if err != nil {
			logger.Panicf(err.Error())
			return false
		}

		return database_name == name
	}

	return false
}

func (self *postgres) CreateDatabase(name string) error {
	ds := self.TDataSource
	ds.DbName = "postgres"
	db_driver := ds.DbType
	db_src := ds.toString()

	db, err := sql.Open(db_driver, db_src)
	if err != nil {
		return err
	}

	query := fmt.Sprintf("CREATE DATABASE %v", name)
	result, err := db.Exec(query)
	if err != nil {
		return err
	}

	effect, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if effect == 1 {
		return nil
	}

	return fmt.Errorf("create database %s fail!", name)
}

func (self *postgres) DropDatabase(name string) error {
	ds := self.TDataSource
	ds.DbName = "postgres"
	db_driver := ds.DbType
	db_src := ds.toString()

	db, err := sql.Open(db_driver, db_src)
	if err != nil {
		return err
	}

	query := fmt.Sprintf("DROP DATABASE IF EXISTS %v", name)
	result, err := db.Exec(query)
	if err != nil {
		return err
	}

	effect, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if effect == 1 {
		return nil
	}

	return fmt.Errorf("drop database %s fail!", name)
}

func (db *postgres) DropIndexSql(tableName string, index *TIndex) string {
	//quote := db.Quote
	//var unique string
	/*var idxName string = index.Name
	if !strings.HasPrefix(idxName, "UQE_") &&
		!strings.HasPrefix(idxName, "IDX_") {
		if index.Type == UniqueType {
			idxName = fmt.Sprintf("UQE_%v_%v", tableName, index.Name)
		} else {
			idxName = fmt.Sprintf("IDX_%v_%v", tableName, index.Name)
		}
	}*/
	idxName := index.GetName(tableName)
	return fmt.Sprintf("DROP INDEX %v", db.Quote(idxName))
}

func (db *postgres) IsColumnExist(tableName, colName string) (bool, error) {
	args := []interface{}{tableName, colName}
	query := "SELECT column_name FROM INFORMATION_SCHEMA.COLUMNS WHERE table_name = $1" +
		" AND column_name = $2"
	db.LogSQL(query, args)

	rows, err := db.DB().Query(query, args...)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	return rows.Next(), nil
}

func (db *postgres) GetFields(tableName string) ([]string, map[string]IField, error) {
	// FIXME: the schema should be replaced by user custom's
	args := []interface{}{tableName, "public"}
	s := `SELECT column_name, column_default, is_nullable, data_type, character_maximum_length, numeric_precision, numeric_precision_radix ,
    CASE WHEN p.contype = 'p' THEN true ELSE false END AS primarykey,
    CASE WHEN p.contype = 'u' THEN true ELSE false END AS uniquekey
FROM pg_attribute f
    JOIN pg_class c ON c.oid = f.attrelid JOIN pg_type t ON t.oid = f.atttypid
    LEFT JOIN pg_attrdef d ON d.adrelid = c.oid AND d.adnum = f.attnum
    LEFT JOIN pg_namespace n ON n.oid = c.relnamespace
    LEFT JOIN pg_constraint p ON p.conrelid = c.oid AND f.attnum = ANY (p.conkey)
    LEFT JOIN pg_class AS g ON p.confrelid = g.oid
    LEFT JOIN INFORMATION_SCHEMA.COLUMNS s ON s.column_name=f.attname AND c.relname=s.table_name
WHERE c.relkind = 'r'::char AND c.relname = $1 AND s.table_schema = $2 AND f.attnum > 0 ORDER BY f.attnum;`
	db.LogSQL(s, args)

	rows, err := db.DB().Query(s, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	cols := make(map[string]IField)
	colSeq := make([]string, 0)

	for rows.Next() {
		var sql_type SQLType
		var colName, isNullable, dataType string
		var maxLenStr, colDefault, numPrecision, numRadix *string
		var isPK, isUnique bool
		err = rows.Scan(&colName, &colDefault, &isNullable, &dataType, &maxLenStr, &numPrecision, &numRadix, &isPK, &isUnique)
		if err != nil {
			return nil, nil, err
		}

		switch dataType {
		case "character varying", "character":
			sql_type = SQLType{Varchar, 0, 0}
		case "timestamp without time zone":
			sql_type = SQLType{DateTime, 0, 0}
		case "timestamp with time zone":
			sql_type = SQLType{TimeStampz, 0, 0}
		case "double precision":
			sql_type = SQLType{Double, 0, 0}
		case "boolean":
			sql_type = SQLType{Bool, 0, 0}
		case "time without time zone":
			sql_type = SQLType{Time, 0, 0}
		case "oid":
			sql_type = SQLType{BigInt, 0, 0}
		default:
			sql_type = SQLType{strings.ToUpper(dataType), 0, 0}
		}

		if _, ok := SqlTypes[sql_type.Name]; !ok {
			return nil, nil, errors.New(fmt.Sprintf("unknow colType: %v", dataType))
		}

		col := NewField(strings.Trim(colName, `" `), sql_type) // new(Column)
		//		col.Base().Indexes = make(map[string]int)

		//fmt.Println(args, colName, isNullable, dataType, maxLenStr, colDefault, numPrecision, numRadix, isPK, isUnique)
		var maxLen int
		if maxLenStr != nil {
			maxLen, err = strconv.Atoi(*maxLenStr)
			if err != nil {
				return nil, nil, err
			}
		}

		if colDefault != nil || isPK {
			if isPK {
				col.Base().isPrimaryKey = true
			} else {
				col.Base()._attr_size = utils.StrToInt(*colDefault)
			}
		}

		if colDefault != nil && strings.HasPrefix(*colDefault, "nextval(") {
			col.Base().isAutoIncrement = true
		}

		col.Base()._attr_required = (isNullable == "NO")

		col.Base()._attr_size = maxLen

		/*
			if col.SQLType().IsText() || col.SQLType().IsTime() {
				if col.Base()._attr_size != 0 {
					col.Base()._attr_size = "'" + col.Base()._attr_size + "'"
				} else {
					if col.Base().defaultIsEmpty {
						col.Base()._attr_size = 0 //"''"
					}
				}
			}
		*/
		cols[col.Name()] = col
		colSeq = append(colSeq, col.Name())
	}

	return colSeq, cols, nil
}

func (db *postgres) GetModels() ([]IModel, error) {
	// FIXME: replace public to user customrize schema
	args := []interface{}{"public"}
	s := fmt.Sprintf("SELECT tablename FROM pg_tables WHERE schemaname = $1")
	db.LogSQL(s, args)

	rows, err := db.DB().Query(s, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	models := make([]IModel, 0)
	for rows.Next() {
		var model_name string
		err = rows.Scan(&model_name)
		if err != nil {
			return nil, err
		}

		model_val := reflect.Indirect(reflect.ValueOf(new(TModel)))
		model_type := model_val.Type()

		// new a base model instance
		model := NewModel(model_name, model_val, model_type)
		models = append(models, model)
	}
	return models, nil
}

func (db *postgres) GetIndexes(tableName string) (map[string]*TIndex, error) {
	// FIXME: replace the public schema to user specify schema
	args := []interface{}{"public", tableName}
	s := fmt.Sprintf("SELECT indexname, indexdef FROM pg_indexes WHERE schemaname=$1 AND tablename=$2")
	db.LogSQL(s, args)

	rows, err := db.DB().Query(s, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	indexes := make(map[string]*TIndex, 0)
	for rows.Next() {
		var indexType int
		var indexName, indexdef string
		var colNames []string

		err = rows.Scan(&indexName, &indexdef)
		if err != nil {
			return nil, err
		}

		indexName = strings.Trim(indexName, `" `)
		if strings.HasSuffix(indexName, "_pkey") {
			continue
		}

		if strings.HasPrefix(indexdef, "CREATE UNIQUE INDEX") {
			indexType = UniqueType
		} else {
			indexType = IndexType
		}

		cs := strings.Split(indexdef, "(")
		colNames = strings.Split(cs[1][0:len(cs[1])-1], ",")

		if strings.HasPrefix(indexName, IndexPrefix+tableName) || strings.HasPrefix(indexName, UniquePrefix+tableName) {
			newIdxName := indexName[5+len(tableName):]
			if newIdxName != "" {
				indexName = newIdxName
			}
		}

		var indexs []string
		for _, colName := range colNames {
			indexs = append(indexs, strings.Trim(colName, `" `))
		}

		index := newIndex(indexName, indexType, indexs...)
		//index := &TIndex{Name: indexName, Type: indexType, Cols: make([]string, 0)}

		indexes[index.Name] = index
	}
	return indexes, nil
}

func (db *postgres) Fmter() []IFmter {
	return []IFmter{
		&IdFmter{},
		&QuoteFmter{},
		&HolderFmter{Prefix: "$", Start: 1}}
}
