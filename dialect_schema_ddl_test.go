package orm

// P0a 加固护栏：多 schema 场景（如 per-tenant schema 隔离）下，DDL 与元数据查询
// 必须尊重会话 schema——同名表可同时存在于 public 与其他 schema，各查各的、各建各的。
//
// 单测部分不连库（纯 SQL 生成断言）；集成部分需本地 Postgres
// （postgres/postgres@localhost:5432），无库自动 Skip。集成库一次性创建、测后删除。

import (
	"database/sql"
	"strings"
	"testing"
)

func newPgDialectForTest(t *testing.T) IDialect {
	t.Helper()
	d := QueryDialect("postgres")
	if d == nil {
		t.Fatal("postgres dialect not registered")
	}
	if err := d.Init(nil, &TDataSource{DbType: "postgres", DbName: "testdb", Schema: "public"}); err != nil {
		t.Fatalf("init dialect: %v", err)
	}
	return d
}

// TableCheckSql 必须按 schemaname 过滤；空 schema 退回默认 public。
func TestPGTableCheckSqlSchemaScoped(t *testing.T) {
	d := newPgDialectForTest(t)

	sqlStr, args := d.TableCheckSql("system", "res_company")
	if !strings.Contains(sqlStr, "schemaname") {
		t.Errorf("TableCheckSql missing schemaname filter: %s", sqlStr)
	}
	if len(args) != 2 || args[0] != "res_company" || args[1] != "system" {
		t.Errorf("TableCheckSql args = %v, want [res_company system]", args)
	}

	_, args = d.TableCheckSql("", "res_company")
	if len(args) != 2 || args[1] != "public" {
		t.Errorf("TableCheckSql empty-schema args = %v, want fallback public", args)
	}
}

// IndexCheckSql 必须按 schemaname 过滤。
func TestPGIndexCheckSqlSchemaScoped(t *testing.T) {
	d := newPgDialectForTest(t)

	sqlStr, args := d.IndexCheckSql("system", "res_company", "IDX_res_company_code")
	if !strings.Contains(sqlStr, "schemaname") {
		t.Errorf("IndexCheckSql missing schemaname filter: %s", sqlStr)
	}
	if len(args) != 3 || args[0] != "system" {
		t.Errorf("IndexCheckSql args = %v, want schema first", args)
	}
}

// DROP TABLE / CREATE INDEX / DROP INDEX 必须按 schema 限定；索引名不掺 schema。
func TestPGIndexAndDropSqlSchemaQualified(t *testing.T) {
	d := newPgDialectForTest(t)

	if got := d.DropTableSql("system", "res_company"); !strings.Contains(got, `"system"."res_company"`) {
		t.Errorf("DropTableSql = %s, want qualified table", got)
	}

	idx := newIndex("", "res_company", IndexType, "code")
	// 注意：generate_index_name 会压缩表名（res_company→rcompany）
	create := d.CreateIndexUniqueSql("system", "res_company", idx)
	if !strings.Contains(create, `ON "system"."res_company"`) {
		t.Errorf("CreateIndexUniqueSql = %s, want ON qualified table", create)
	}
	if !strings.Contains(create, `"IDX_rcompany_code"`) {
		t.Errorf("CreateIndexUniqueSql = %s, want bare index name derived from bare table", create)
	}
	if strings.Contains(create, `"IDX_system`) {
		t.Errorf("CreateIndexUniqueSql = %s, index name polluted by schema", create)
	}

	drop := d.DropIndexUniqueSql("system", "res_company", idx)
	if !strings.Contains(drop, `"system"."IDX_rcompany_code"`) {
		t.Errorf("DropIndexUniqueSql = %s, want schema-qualified index", drop)
	}
}

// sqlite 无 schema 概念：加参后行为不变。
func TestSqliteIgnoresSchema(t *testing.T) {
	d := QueryDialect("sqlite")
	if d == nil {
		t.Fatal("sqlite dialect not registered")
	}
	if err := d.Init(nil, &TDataSource{DbType: "sqlite", DbName: ":memory:"}); err != nil {
		t.Fatalf("init sqlite dialect: %v", err)
	}
	sqlStr, args := d.TableCheckSql("system", "res_company")
	if strings.Contains(sqlStr, "system") || len(args) != 1 || args[0] != "res_company" {
		t.Errorf("sqlite TableCheckSql should ignore schema: sql=%s args=%v", sqlStr, args)
	}
}

// ---------------------------------------------------------------------------
// 集成：同一模型在 public 与 system 各建一份（修复前 TableCheckSql 无 schemaname
// 过滤，public 已存在会让 system 副本被误判「已存在」而根本不建；CreateTableSql
// 经 model.Transaction() 取 schema，DDL 会话的 SetSchema 不生效）。
// ---------------------------------------------------------------------------

type SchemaDdlPoc struct {
	TModel `table:"name('schema_ddl_poc')"`
	Id     int64  `field:"pk autoincr"`
	Code   string `field:"varchar() index"`
}

const itSchemaDDLDB = "orm_schema_ddl_it"

func TestSchemaDDLIsolationPG(t *testing.T) {
	maint, err := sql.Open("postgres", "host=localhost port=5432 user=postgres password=postgres dbname=postgres sslmode=disable")
	if err != nil {
		t.Skipf("cannot open postgres: %v", err)
	}
	defer maint.Close()
	if err := maint.Ping(); err != nil {
		t.Skipf("no local postgres: %v", err)
	}
	_, _ = maint.Exec("DROP DATABASE IF EXISTS " + itSchemaDDLDB)
	if _, err := maint.Exec("CREATE DATABASE " + itSchemaDDLDB); err != nil {
		t.Fatalf("create db: %v", err)
	}
	defer func() { _, _ = maint.Exec("DROP DATABASE IF EXISTS " + itSchemaDDLDB) }()

	o, err := New(WithDataSource(&TDataSource{
		DbType: "postgres", DbName: itSchemaDDLDB, Host: "localhost",
		UserName: "postgres", Password: "postgres", SSLMode: "disable",
	}))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}

	// 1) 默认会话（public）建表
	if _, err := o.SyncModel("test", new(SchemaDdlPoc)); err != nil {
		t.Fatalf("sync public: %v", err)
	}

	// 2) system 会话建同名表
	sess := o.NewSession()
	defer sess.Close()
	sess.SetSchema("system")
	if _, err := sess.Exec("CREATE SCHEMA IF NOT EXISTS system"); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if _, err := sess.SyncModel("test", new(SchemaDdlPoc)); err != nil {
		t.Fatalf("sync system: %v", err)
	}

	// 3) 两 schema 各有一份表；索引也各在其位
	verify, err := sql.Open("postgres", "host=localhost port=5432 user=postgres password=postgres dbname="+itSchemaDDLDB+" sslmode=disable")
	if err != nil {
		t.Fatalf("open verify conn: %v", err)
	}
	defer verify.Close()
	for _, sch := range []string{"public", "system"} {
		var n int
		if err := verify.QueryRow(`SELECT count(*) FROM pg_tables WHERE schemaname=$1 AND tablename='schema_ddl_poc'`, sch).Scan(&n); err != nil {
			t.Fatalf("query pg_tables: %v", err)
		}
		if n != 1 {
			t.Errorf("table schema_ddl_poc in %s: got %d, want 1", sch, n)
		}
		var idxN int
		if err := verify.QueryRow(`SELECT count(*) FROM pg_indexes WHERE schemaname=$1 AND tablename='schema_ddl_poc'`, sch).Scan(&idxN); err != nil {
			t.Fatalf("query pg_indexes: %v", err)
		}
		if idxN == 0 {
			t.Errorf("no index on schema_ddl_poc in %s", sch)
		}
	}

	// 4) DML 也按会话 schema 落位：system 会话 INSERT/READ/DELETE 只作用于 system 副本
	sysSess := o.NewSession()
	defer sysSess.Close()
	sysSess.SetSchema("system")
	if _, err := sysSess.Model("schema.ddl.poc").Create(map[string]any{"code": "sys-row"}); err != nil {
		t.Fatalf("create in system: %v", err)
	}
	pubSess := o.NewSession()
	defer pubSess.Close()
	if _, err := pubSess.Model("schema.ddl.poc").Create(map[string]any{"code": "pub-row"}); err != nil {
		t.Fatalf("create in public: %v", err)
	}
	countIn := func(sch string) int {
		var n int
		if err := verify.QueryRow(`SELECT count(*) FROM ` + sch + `.schema_ddl_poc`).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", sch, err)
		}
		return n
	}
	if got := countIn("system"); got != 1 {
		t.Errorf("system rows = %d, want 1 (INSERT must respect session schema)", got)
	}
	if got := countIn("public"); got != 1 {
		t.Errorf("public rows = %d, want 1", got)
	}
	// READ 按 schema 分仓
	ds, err := sysSess.Model("schema.ddl.poc").Where("code=?", "pub-row").Read()
	if err != nil {
		t.Fatalf("read in system: %v", err)
	}
	if ds != nil && ds.Count() != 0 {
		t.Errorf("system session sees public row — SELECT not schema-scoped")
	}
	ds, err = sysSess.Model("schema.ddl.poc").Where("code=?", "sys-row").Read()
	if err != nil {
		t.Fatalf("read own in system: %v", err)
	}
	if ds == nil || ds.Count() != 1 {
		t.Errorf("system session cannot see its own row")
	}
}
