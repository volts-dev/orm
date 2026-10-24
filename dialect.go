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
		// LikeClause 生成一条 like 家族(like/ilike/not like/not ilike)的谓词，
		// column 已带表别名与引号，值以单个 '?' 占位。
		// 各方言的差异必须收在这里：postgres 需要 `::text` 转型且原生支持 ILIKE，
		// mysql/sqlite 两者都没有(`::text` 直接是语法错误)。
		LikeClause(column, operator string) string
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
		// ValidateIndex 在生成 DDL 之前判断本方言能否**如实**表达这条索引。
		// 表达不了就返回 ErrIndexUnsupported，而不是生成一条语义不同的 SQL。
		ValidateIndex(index *TIndex) error
		DropColumnNotNullSql(schema, tableName string, col IField) string
		DropColumnDefaultSql(schema, tableName string, col IField) string
		ModifyColumnSql(schema, tableName string, col IField) string
		// LockClause 生成行级锁子句(FOR UPDATE / FOR SHARE ...)，追加在
		// LIMIT/OFFSET **之后**。lock 为 nil 或 LockNone 时返回空串。
		// tableAlias 是本次查询主模型在 FROM 里的别名：postgres 用它生成
		// `FOR UPDATE OF <alias>`，把锁限定在主表上——否则一旦查询里有
		// LEFT JOIN(继承字段的父表连接就是)，PG 会以
		// "FOR UPDATE cannot be applied to the nullable side of an outer join"
		// 拒绝整条查询。传空串表示不限定。
		// 方言不支持行锁时返回 errors.ErrLockNotSupported，调用方据此降级为警告。
		LockClause(lock *TLock, tableAlias string) (string, error)
		// SetSearchPathSql 生成一条把当前**事务**的默认 schema 指向 schema 的语句；
		// 方言没有这个概念时返回空串。
		//
		// 用途：让**裸 SQL**（Exec/Query 里的字符串）也落在会话指定的 schema 上。
		// Statement 的 qualifiedTable 只管 ORM 自己拼的 SQL，表名已经写在字符串里的
		// 那些它够不着——于是同一张表 ORM 写 system、裸 SQL 读 public，查 0 行、不报错。
		//
		// 必须是**事务级**（postgres 的 SET LOCAL）：会话级 SET 会留在连接上，而连接
		// 是池化的，下一个借到它的请求会继承这个 schema —— 那是跨租户串数据，
		// 比原来的 bug 严重得多。
		SetSearchPathSql(schema string) string
		// StatementTimeoutSql 生成放开 / 还原当前**事务**的库侧超时的语句
		//（语句超时与等锁超时；事务结束自动还原）。不支持的方言返回空串。
		// 见 TSession.RelaxTimeouts。
		StatementTimeoutSql(relax bool) string
		// RowScopeSql 生成一条把当前**事务**的行范围设成 value 的语句（见 row_scope.go）；
		// 方言没有事务级自定义设置时返回空串，行范围在那里是空操作。
		RowScopeSql(setting string, value int64) string
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
		// Features 是方言的能力表。方言层之外的代码**只问能力、不问是哪个库**（MySQL 兼容
		// P1，2026-10-24；Django 的 DatabaseFeatures 同一个思路）。不依赖 Init——New 在连库之前就要用。
		Features() DialectFeatures
		// DropPrimaryKeySql 是删掉表主键约束的语句（只用在「请手动执行」的提示里）；表达不了的方言返回空串。
		DropPrimaryKeySql(quotedTable, table string) string

		// MapError 把 driver 原生错误翻译为 errors 包定义的 sentinel
		// 各 dialect 必须实现；session 层统一调用
		MapError(err error) error
	}

	// DialectFeatures：一个方言支持哪些超出「各家都有」的能力。零值＝都不支持（保守）。
	//
	// 不支持的能力有两种处理，都**不许静默**：配置期能看出来的在 New 里直接报错
	// （checkDialectFeatures，例如 MySQL 上开 RowScopeEnforce）；用到才知道的在生成 SQL 那一刻
	// 报错并点名能力（例如在不支持 DateTrunc 的方言上按月分组）。
	DialectFeatures struct {
		Returning        bool // INSERT … RETURNING
		TransactionalDDL bool // 建表 / 改表能随事务回滚（MySQL 的 DDL 隐式提交）
		Schemas          bool // 同一连接里按 schema 限定表（PG 的 schema；MySQL 上要等 P4 定成「库」）
		RowSecurity      bool // 库侧行级安全：RowScopeEnforce 发出的 SET LOCAL 有人认
		PartialIndex     bool // CREATE INDEX … WHERE
		FunctionalIndex  bool // 表达式索引
		JSONOps          bool // jsonb 运算（->> / @> / ?），公司相关字段与 properties 筛选靠它
		JSONIndex        bool // JSON 列上的 GIN 之类索引
		Vector           bool // 向量列与距离运算
		UnboundedVarchar bool // 不带长度的 VARCHAR
		Sequences        bool // 库里的序列对象
		XactAdvisoryLock bool // 事务级咨询锁（pg_advisory_xact_lock）
		DateTrunc        bool // 按日期粒度截断 / 取日期部分（read_group 的 :month、domain 里的 date part）
		TableComment     bool // COMMENT ON TABLE
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

func (db *TDialect) Features() DialectFeatures { return DialectFeatures{} }

func (db *TDialect) DropPrimaryKeySql(quotedTable, table string) string { return "" }

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

// LikeClause 的通用实现（sqlite；mysql 另有，见 dialect_mysql.go）：
//   - 不加 `::text`——那是 postgres 专有语法，别的库上直接
//     `unrecognized token: ":"` / `syntax error`。曾经 expr.go 对**所有**方言硬拼
//     `::text`，于是非 postgres 上任何 like/ilike 的 domain 查询都是直接报错。
//   - ILIKE 落成 LIKE：mysql/sqlite 都没有 ILIKE 关键字。两者默认排序规则下
//     LIKE 对 ASCII 本来就是大小写不敏感的（sqlite 内建、mysql 的 *_ci 排序规则），
//     非 ASCII 的大小写折叠不保证——这是方言能力所限，不是本层能补的。
func (db *TDialect) LikeClause(column, operator string) string {
	op := strings.ToUpper(strings.TrimSpace(operator))
	op = strings.ReplaceAll(op, "ILIKE", "LIKE")
	return fmt.Sprintf("(%s %s ?)", column, op)
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
	return fmt.Sprintf("CREATE%s INDEX %v ON %v (%v)%s", unique,
		quoter.Quote(idxName), quoter.QuoteTable(schema, tableName),
		indexKeyParts(quoter, index), indexWhereClause(index))
}

// ValidateIndex 默认实现：表达式与部分索引都是标准 SQL，PG/SQLite 原生支持。
// 表达不了的方言（MySQL）自行覆写。
func (db *TDialect) ValidateIndex(index *TIndex) error {
	return nil
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

// StatementTimeoutSql 默认无操作：只有 postgres 实现了事务级的超时开关。
func (db *TDialect) StatementTimeoutSql(relax bool) string {
	return ""
}

// RowScopeSql 默认无操作：只有 postgres 有事务级的自定义设置（也只有它有行级安全）。
func (db *TDialect) RowScopeSql(setting string, value int64) string {
	return ""
}

// SetSearchPathSql 默认无操作：只有 postgres 有 search_path。
// mysql 的 schema 等同于 database（连接时就定了），sqlite 没有 schema 概念。
func (db *TDialect) SetSearchPathSql(schema string) string {
	return ""
}

// LockClause 通用实现，覆盖 SQL 标准 / PostgreSQL 语法。
// mysql 与 sqlite 各自覆写，见 dialect_mysql.go / dialect_sqlite.go。
func (db *TDialect) LockClause(lock *TLock, tableAlias string) (string, error) {
	if !lock.IsLocking() {
		return "", nil
	}

	var b strings.Builder
	switch lock.Mode {
	case LockUpdate:
		b.WriteString("FOR UPDATE")
	case LockShare:
		b.WriteString("FOR SHARE")
	default:
		return "", fmt.Errorf("orm: unknown lock mode %d", lock.Mode)
	}

	// 只锁主表：查询里的 LEFT JOIN(继承字段的父表连接)不可锁，
	// 而 .ForUpdate() 的语义本来就是"锁住我正在读的这个模型的行"。
	if tableAlias != "" {
		b.WriteString(" OF ")
		b.WriteString(tableAlias)
	}

	switch lock.Wait {
	case LockWaitNoWait:
		b.WriteString(" NOWAIT")
	case LockWaitSkip:
		b.WriteString(" SKIP LOCKED")
	}

	return b.String(), nil
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
	// 只有**静态**默认值落成列默认值。默认值函数（DefaultFunc、default_company）在应用层
	// 新建时求值，库里没有它的对应物；拿 IsDefaultEmpty 判的话它们也会被当成「有默认值」，
	// 落成一个毫无意义的 DEFAULT 0 / DEFAULT ''（company_id bigint DEFAULT 0）。
	if dv := columnDefault(field); !field.IsAutoIncrement() && dv != nil {
		if _, err := bd.WriteString(" DEFAULT "); err != nil {
			return "", err
		}

		dvStr := utils.ToString(dv)
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

// checkDialectFeatures 在连库之前拒掉方言做不到的配置——做不到的东西不许静默失效。
//
// RowScopeEnforce 的意思是「把行范围发给库，由库上的行级安全拦」；方言没有 RowSecurity 时
// RowScopeSql 回空串，Enforce 就成了空转：配置上写着隔离、实际一行都拦不住（MySQL 正是如此）。
func checkDialectFeatures(d IDialect, cfg *Config) error {
	f := d.Features()
	if cfg.RowScopeMode == RowScopeEnforce && !f.RowSecurity {
		return fmt.Errorf("orm: RowScopeEnforce needs Features.RowSecurity (row-level security in the database); dialect %s has none — use RowScopeAudit, or rely on the query-level tenant filter", cfg.DataSource.DbType)
	}
	return nil
}

// sqlStringLiteral 把 s 写成标准 SQL 的字符串字面量（单引号翻倍）。用在拼不了占位符的 DDL 里
// （COMMENT ON COLUMN … IS '…'）：标题 / 帮助文字里有撇号时此前直接是语法错（PG 42601 / MySQL 1064）。
func sqlStringLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// mysqlStringLiteral 同上，另外把反斜杠翻倍：MySQL 默认（没有 NO_BACKSLASH_ESCAPES）把字符串里的 \ 当转义。
func mysqlStringLiteral(s string) string {
	return sqlStringLiteral(strings.ReplaceAll(s, `\`, `\\`))
}
