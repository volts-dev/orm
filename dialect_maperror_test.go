package orm

import (
	"database/sql/driver"
	stdErrors "errors"
	"fmt"
	"os"
	"strings"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/lib/pq"
	ormerr "github.com/volts-dev/orm/errors"
	_ "modernc.org/sqlite"
)

// --- MySQL MapError unit tests (no live DB needed) ---

func TestMySQLMapError_Duplicate(t *testing.T) {
	d := &mysql{}
	src := &mysqldriver.MySQLError{Number: 1062, Message: "Duplicate entry 'x' for key 'PRIMARY'"}
	got := d.MapError(src)
	if !stdErrors.Is(got, ormerr.ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate, got %v", got)
	}
}

func TestMySQLMapError_Deadlock(t *testing.T) {
	d := &mysql{}
	src := &mysqldriver.MySQLError{Number: 1213, Message: "Deadlock found"}
	got := d.MapError(src)
	if !stdErrors.Is(got, ormerr.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", got)
	}
}

func TestMySQLMapError_LockTimeout(t *testing.T) {
	d := &mysql{}
	src := &mysqldriver.MySQLError{Number: 1205, Message: "Lock wait timeout exceeded"}
	got := d.MapError(src)
	if !stdErrors.Is(got, ormerr.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", got)
	}
}

func TestMySQLMapError_ConnectionLost(t *testing.T) {
	d := &mysql{}
	src := &mysqldriver.MySQLError{Number: 2006, Message: "MySQL server has gone away"}
	got := d.MapError(src)
	if !stdErrors.Is(got, ormerr.ErrConnection) {
		t.Fatalf("expected ErrConnection, got %v", got)
	}
}

func TestMySQLMapError_ForeignKey(t *testing.T) {
	d := &mysql{}
	src := &mysqldriver.MySQLError{Number: 1452, Message: "Cannot add or update a child row: a foreign key constraint fails"}
	got := d.MapError(src)
	if !stdErrors.Is(got, ormerr.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", got)
	}
}

func TestMySQLMapError_Passthrough(t *testing.T) {
	d := &mysql{}
	src := &mysqldriver.MySQLError{Number: 9999, Message: "unknown error"}
	got := d.MapError(src)
	// unknown error code should pass through unchanged
	if stdErrors.Is(got, ormerr.ErrDuplicate) || stdErrors.Is(got, ormerr.ErrConflict) {
		t.Fatalf("unexpected sentinel mapping for unknown error code, got %v", got)
	}
}

func TestMySQLMapError_Nil(t *testing.T) {
	d := &mysql{}
	if got := d.MapError(nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

// --- PostgreSQL MapError unit tests (no live DB needed) ---

func TestPostgresMapError_Duplicate(t *testing.T) {
	d := &postgres{}
	src := &pq.Error{Code: "23505", Message: "duplicate key value violates unique constraint"}
	got := d.MapError(src)
	if !stdErrors.Is(got, ormerr.ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate, got %v", got)
	}
}

func TestPostgresMapError_ForeignKey(t *testing.T) {
	d := &postgres{}
	src := &pq.Error{Code: "23503", Message: "insert or update on table violates foreign key constraint"}
	got := d.MapError(src)
	if !stdErrors.Is(got, ormerr.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", got)
	}
}

func TestPostgresMapError_NotNull(t *testing.T) {
	d := &postgres{}
	src := &pq.Error{Code: "23502", Message: "null value in column violates not-null constraint"}
	got := d.MapError(src)
	if !stdErrors.Is(got, ormerr.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", got)
	}
}

func TestPostgresMapError_Deadlock(t *testing.T) {
	d := &postgres{}
	src := &pq.Error{Code: "40P01", Message: "deadlock detected"}
	got := d.MapError(src)
	if !stdErrors.Is(got, ormerr.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", got)
	}
}

func TestPostgresMapError_Serialization(t *testing.T) {
	d := &postgres{}
	src := &pq.Error{Code: "40001", Message: "could not serialize access due to concurrent update"}
	got := d.MapError(src)
	if !stdErrors.Is(got, ormerr.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", got)
	}
}

func TestPostgresMapError_Connection(t *testing.T) {
	d := &postgres{}
	src := &pq.Error{Code: "08006", Message: "connection failure"}
	got := d.MapError(src)
	if !stdErrors.Is(got, ormerr.ErrConnection) {
		t.Fatalf("expected ErrConnection, got %v", got)
	}
}

func TestPostgresMapError_Nil(t *testing.T) {
	d := &postgres{}
	if got := d.MapError(nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

// --- SQLite MapError unit tests (string-based matching) ---

func TestSqliteMapError_Duplicate(t *testing.T) {
	d := &sqlite{}
	src := stdErrors.New("UNIQUE constraint failed: bench_model.id")
	got := d.MapError(src)
	if !stdErrors.Is(got, ormerr.ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate, got %v", got)
	}
}

func TestSqliteMapError_Locked(t *testing.T) {
	d := &sqlite{}
	src := stdErrors.New("database is locked")
	got := d.MapError(src)
	if !stdErrors.Is(got, ormerr.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", got)
	}
}

func TestSqliteMapError_NoSuchTable(t *testing.T) {
	d := &sqlite{}
	src := stdErrors.New("no such table: missing_table")
	got := d.MapError(src)
	if !stdErrors.Is(got, ormerr.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", got)
	}
}

func TestSqliteMapError_ForeignKey(t *testing.T) {
	d := &sqlite{}
	src := stdErrors.New("FOREIGN KEY constraint failed")
	got := d.MapError(src)
	if !stdErrors.Is(got, ormerr.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", got)
	}
}

func TestSqliteMapError_NotNull(t *testing.T) {
	d := &sqlite{}
	src := stdErrors.New("NOT NULL constraint failed: bench_model.name")
	got := d.MapError(src)
	if !stdErrors.Is(got, ormerr.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", got)
	}
}

func TestSqliteMapError_Nil(t *testing.T) {
	d := &sqlite{}
	if got := d.MapError(nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

// --- Integration test: SQLite Duplicate via live ORM raw Exec ---

// TestMapError_Duplicate_Integration 用 SQLite 内存模式验证 end-to-end 路径：
// session.Exec → _exec → dialect.MapError → ormerr.ErrDuplicate
// 使用 raw SQL 直接插入重复主键，绕过 ORM 的 ID 自动生成逻辑
func TestMapError_Duplicate_Integration(t *testing.T) {
	ds := testDataSource(t, ":memory:")
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("New ORM: %v", err)
	}
	defer o.Close()

	// Create a simple table with a unique PK using raw SQL
	ss := o.NewSession()
	defer ss.Close()

	if _, err := ss.Exec(`CREATE TABLE IF NOT EXISTS maperror_test (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}

	// Insert first row
	if _, err := ss.Exec(`INSERT INTO maperror_test (id, name) VALUES (?, ?)`, 1, "alice"); err != nil {
		t.Fatalf("first INSERT: %v", err)
	}

	// Duplicate insert — same PK; should trigger UNIQUE constraint
	_, dupErr := ss.Exec(`INSERT INTO maperror_test (id, name) VALUES (?, ?)`, 1, "bob")
	if dupErr == nil {
		t.Fatal("expected duplicate error, got nil")
	}
	if !stdErrors.Is(dupErr, ormerr.ErrDuplicate) {
		t.Errorf("expected errors.Is(err, ErrDuplicate)==true, got err=%v (type: %T)", dupErr, dupErr)
	}
}

// TestMapError_MySQL_Live 仅在 MySQL 环境跑（需要 MYSQL_TEST_HOST 环境变量）
func TestMapError_MySQL_Live(t *testing.T) {
	if os.Getenv("MYSQL_TEST_HOST") == "" {
		t.Skip("MYSQL_TEST_HOST not set; skip")
	}
	t.Log("MySQL live MapError 测试: PR-2 实施时按真实 driver 行为补齐")
}

// TestMapError_Postgres_Live 仅在 PG 环境跑
func TestMapError_Postgres_Live(t *testing.T) {
	if os.Getenv("POSTGRES_TEST_HOST") == "" {
		t.Skip("POSTGRES_TEST_HOST not set; skip")
	}
	t.Log("PostgreSQL live MapError 测试: PR-2 实施时按真实 driver 行为补齐")
}

// --- MySQL 兼容 P2（2026-10-24）：缺默认值 / 值放不进列 / 语句超时 / 连接断开 ---

func TestMySQLMapError_P2Codes(t *testing.T) {
	d := &mysql{}
	cases := []struct {
		num        uint16
		msg        string
		kind       error
		field      string
		isRequired bool
	}{
		{1364, "Field 'partner_id' doesn't have a default value", ormerr.ErrRequired, "partner_id", true},
		{1406, "Data too long for column 'code' at row 1", ormerr.ErrValidation, "code", false},
		{1264, "Out of range value for column 'qty' at row 1", ormerr.ErrValidation, "qty", false},
		{1265, "Data truncated for column 'state' at row 1", ormerr.ErrValidation, "state", false},
		{1366, "Incorrect integer value: 'abc' for column 'qty' at row 1", ormerr.ErrValidation, "qty", false},
		{1292, "Incorrect datetime value: 'x' for column 'seen' at row 1", ormerr.ErrValidation, "seen", false},
		{3024, "Query execution was interrupted, maximum statement execution time exceeded", ormerr.ErrTimeout, "", false},
		{1317, "Query execution was interrupted", ormerr.ErrTimeout, "", false},
		{4031, "The client was disconnected by the server because of inactivity.", ormerr.ErrConnection, "", false},
	}
	for _, c := range cases {
		got := d.MapError(&mysqldriver.MySQLError{Number: c.num, Message: c.msg})
		if !stdErrors.Is(got, c.kind) {
			t.Errorf("%d: 想要 %v，得 %v", c.num, c.kind, got)
			continue
		}
		var oe *ormerr.ORMError
		stdErrors.As(got, &oe)
		if c.isRequired {
			if f := ormerr.RequiredFields(got); len(f) != 1 || f[0] != c.field {
				t.Errorf("%d: 必填字段 %v，想要 %s", c.num, f, c.field)
			}
		} else if oe.Field != c.field {
			t.Errorf("%d: 列 %q，想要 %q", c.num, oe.Field, c.field)
		}
	}
	for _, e := range []error{mysqldriver.ErrInvalidConn, driver.ErrBadConn, fmt.Errorf("query: %w", mysqldriver.ErrInvalidConn)} {
		if got := d.MapError(e); !stdErrors.Is(got, ormerr.ErrConnection) {
			t.Errorf("%v: 想要 ErrConnection，得 %v", e, got)
		}
	}
}

func TestPostgresMapError_ValueDoesNotFit(t *testing.T) {
	d := &postgres{}
	for _, code := range []pq.ErrorCode{"22001", "22003", "22P02", "22007", "22008"} {
		if got := d.MapError(&pq.Error{Code: code}); !stdErrors.Is(got, ormerr.ErrValidation) {
			t.Errorf("%s: 想要 ErrValidation，得 %v", code, got)
		}
	}
	if got := d.MapError(driver.ErrBadConn); !stdErrors.Is(got, ormerr.ErrConnection) {
		t.Errorf("ErrBadConn: 想要 ErrConnection，得 %v", got)
	}
}

// 真库：超长字符串写进 size(16) 的列。MySQL 严格模式与 PG 都拒，且都归 ErrValidation；
// SQLite 不检查 VARCHAR 长度（Features().UnboundedVarchar），跳过。
func TestMapError_ValueTooLong_Integration(t *testing.T) {
	skipUnlessDialect(t, "postgres", "mysql")
	o, err := New(WithDataSource(testDataSource(t, "")))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer o.Close()
	if _, err := o.SyncModel("", new(ResyncTag), new(ResyncPartner)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	_, err = o.Model("resync.partner").Create(map[string]any{"name": "x", "code": strings.Repeat("y", 17)})
	if !stdErrors.Is(err, ormerr.ErrValidation) {
		t.Fatalf("超长值应报 ErrValidation，得 %v", err)
	}
}
