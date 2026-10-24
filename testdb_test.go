package orm

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// 测试数据源：同一套用例在 SQLite / PostgreSQL / MySQL 上各跑一遍（MySQL 兼容 P0，2026-10-24）。
//
//	ORM_TEST_DB    = sqlite（默认）| postgres | mysql
//	ORM_TEST_PG    = host:port，默认 127.0.0.1:54320（test/docker-compose.test.yml）
//	ORM_TEST_MYSQL = host:port，默认 127.0.0.1:33306
//
// 用户名口令是 test/docker-compose.test.env 那一套（postgres/test、root/test）。postgres / mysql
// 每调用一次建一个全新的库、测试结束删掉——用例之间互不可见，和 SQLite 的 :memory: / 临时文件同一语义。
// ★ 别指向本机真栈的库：整包测试会同时开几百条连接（见 Makefile 的 test-* 目标）。
//
// 用法：把 `&TDataSource{DbType: "sqlite", DbName: X}` 换成 `testDataSource(t, X)`。X 只在 SQLite
// 下用到（":memory:" 或 t.TempDir() 里的文件）。
func testDataSource(t testing.TB, sqliteName string) *TDataSource {
	t.Helper()
	switch testDBKind() {
	case "postgres":
		return freshDatabase(t, "postgres", envOr("ORM_TEST_PG", "127.0.0.1:54320"), "postgres", "test")
	case "mysql":
		return freshDatabase(t, "mysql", envOr("ORM_TEST_MYSQL", "127.0.0.1:33306"), "root", "test")
	}
	return &TDataSource{DbType: "sqlite", DbName: sqliteName}
}

// testDBKind 是本轮跑的方言名。只认得这三个；写错了直接让测试失败，而不是静默退回 SQLite。
func testDBKind() string {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("ORM_TEST_DB")))
	switch v {
	case "", "sqlite":
		return "sqlite"
	case "postgres", "mysql":
		return v
	}
	panic(fmt.Sprintf("ORM_TEST_DB=%q：只认 sqlite / postgres / mysql", v))
}

// skipUnlessDialect 给只在某几个方言上成立的用例用（例如断言 SQLite 专有的报错文字）。
func skipUnlessDialect(t testing.TB, dialects ...string) {
	t.Helper()
	d := testDBKind()
	for _, x := range dialects {
		if x == d {
			return
		}
	}
	t.Skipf("只在 %v 上跑（本轮是 %s）", dialects, d)
}

var testDBSeq atomic.Int64

func freshDatabase(t testing.TB, dbType, hostPort, user, pass string) *TDataSource {
	t.Helper()
	host, port := hostPort, ""
	if i := strings.LastIndex(hostPort, ":"); i > 0 {
		host, port = hostPort[:i], hostPort[i+1:]
	}
	name := fmt.Sprintf("ormt_%d_%d", os.Getpid(), testDBSeq.Add(1))
	var admin *sql.DB
	var err error
	if dbType == "postgres" {
		admin, err = sql.Open("postgres", fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=postgres sslmode=disable", host, port, user, pass))
	} else {
		admin, err = sql.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s)/", user, pass, hostPort))
	}
	if err != nil {
		t.Fatalf("%s 管理连接: %v", dbType, err)
	}
	create := "CREATE DATABASE " + name
	if dbType == "mysql" {
		// 与生产约定一致：utf8mb4 + 区分大小写（2026-10-24 拍板，见 docs/mysql/）。
		create += " CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_as_cs"
	}
	if _, err := admin.Exec(create); err != nil {
		admin.Close()
		t.Fatalf("%s 建测试库 %s（%s）: %v——容器起了吗？docker compose -f test/docker-compose.test.yml up -d", dbType, name, hostPort, err)
	}
	t.Cleanup(func() {
		defer admin.Close()
		drop := "DROP DATABASE IF EXISTS " + name
		if dbType == "postgres" {
			drop += " WITH (FORCE)" // 用例没 Close 的连接池还连着
		}
		if _, err := admin.Exec(drop); err != nil {
			t.Logf("删测试库 %s: %v", name, err)
		}
	})
	ds := &TDataSource{DbType: dbType, Host: host, Port: port, UserName: user, Password: pass, DbName: name, SSLMode: "disable"}
	if dbType == "mysql" {
		// ★ 过渡（P2，2026-10-24）：方言层外还有大量写死的双引号标识符（P3「标识符一律经引号器」在修），
		// 不开 ANSI_QUOTES 的话它们挡在前面，P2 的修复在矩阵里看不出来。P3 收口后删掉这一行。
		ds.Params = map[string]string{"sql_mode": "'" + MySQLSqlMode + ",ANSI_QUOTES'"}
	}
	return ds
}

func envOr(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

// pgDataSource 给只在 PG 上成立的用例用（行级安全、schema 隔离、jsonb 上的公司相关字段……）。
//
//	ORM_TEST_DB 没设：照旧连本机 localhost:5432 的共享测试库 legacyName——go test ./... 的老行为不变；
//	ORM_TEST_DB=postgres：连 docker 里的测试 PG（ORM_TEST_PG），库名同 legacyName，进程内第一次用到时建；
//	sqlite / mysql：跳过。
//
// 同一个 legacyName 多次调用拿到的是**同一个库**——这些用例常开两个 TOrm（一个清场、一个被测），
// 清场靠用例自己的 DROP TABLE，和在本机共享库上时一样。
func pgDataSource(t testing.TB, legacyName string) *TDataSource {
	t.Helper()
	host, port, user, pass := pgTarget(t)
	if os.Getenv("ORM_TEST_DB") != "" {
		ensurePgDatabase(t, legacyName)
	}
	return &TDataSource{DbType: "postgres", Host: host, Port: port, UserName: user, Password: pass, DbName: legacyName, SSLMode: "disable"}
}

// pgDSN 是同一个目标的 lib/pq 关键字连接串（给直接 sql.Open 的用例）。
func pgDSN(t testing.TB, dbName string) string {
	t.Helper()
	host, port, user, pass := pgTarget(t)
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable", host, port, user, pass, dbName)
}

func pgTarget(t testing.TB) (host, port, user, pass string) {
	t.Helper()
	switch testDBKind() {
	case "postgres":
		if os.Getenv("ORM_TEST_DB") != "" {
			hp := envOr("ORM_TEST_PG", "127.0.0.1:54320")
			host, port = hp, ""
			if i := strings.LastIndex(hp, ":"); i > 0 {
				host, port = hp[:i], hp[i+1:]
			}
			return host, port, "postgres", "test"
		}
	case "sqlite":
		if os.Getenv("ORM_TEST_DB") == "" {
			return "localhost", "5432", "postgres", "postgres" // 老行为：本机 PG
		}
	}
	t.Skipf("PG 专属用例（本轮 ORM_TEST_DB=%s）", testDBKind())
	return
}

var pgEnsured sync.Map // 库名 → struct{}

func ensurePgDatabase(t testing.TB, name string) {
	t.Helper()
	if _, ok := pgEnsured.Load(name); ok {
		return
	}
	admin, err := sql.Open("postgres", pgDSN(t, "postgres"))
	if err != nil {
		t.Fatalf("postgres 管理连接: %v", err)
	}
	defer admin.Close()
	var n int
	if err := admin.QueryRow(`SELECT count(*) FROM pg_database WHERE datname = $1`, name).Scan(&n); err != nil {
		t.Fatalf("查测试库 %s: %v——容器起了吗？make test-db-up", name, err)
	}
	if n == 0 {
		if _, err := admin.Exec("CREATE DATABASE " + name); err != nil && !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("建测试库 %s: %v", name, err)
		}
	}
	pgEnsured.Store(name, struct{}{})
}
