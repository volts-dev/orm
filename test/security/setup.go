package security

import (
	"os"
	"testing"

	"github.com/volts-dev/orm"
	_ "modernc.org/sqlite"
)

// SecTestUser is the minimal model used across all dialect injection tests.
type SecTestUser struct {
	orm.TModel `table:"name('sec_test_user')"`
	Id         int64  `field:"pk autoincr"`
	Name       string `field:"varchar() size(255)"`
	Email      string `field:"varchar() size(255)"`
}

// MaliciousInputs is the canonical injection payload set.
var MaliciousInputs = []string{
	`'; DROP TABLE sec_test_user; --`,
	`1' OR '1'='1`,
	`1 UNION SELECT id, name FROM sec_test_user`,
	`1; UPDATE sec_test_user SET name='hacked'`,
	`1\'; DELETE FROM sec_test_user --`,
	`Robert'); DROP TABLE students;--`,
	`admin'--`,
	`' OR 1=1; --`,
}

// seedUsers inserts the standard test fixture: alice + bob.
func seedUsers(t *testing.T, o *orm.TOrm) {
	t.Helper()
	for _, name := range []string{"alice", "bob"} {
		_, err := o.Model("sec.test.user").Create(map[string]any{
			"name":  name,
			"email": name + "@example.com",
		})
		if err != nil {
			t.Fatalf("seed %q: %v", name, err)
		}
	}
}

func setupSQLite(t *testing.T) *orm.TOrm {
	t.Helper()
	ds := &orm.TDataSource{DbType: "sqlite", DbName: ":memory:"}
	o, err := orm.New(orm.WithDataSource(ds))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := o.SyncModel("", new(SecTestUser)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	return o
}

func setupMySQL(t *testing.T) *orm.TOrm {
	t.Helper()
	host := os.Getenv("MYSQL_TEST_HOST")
	if host == "" {
		t.Skip("MYSQL_TEST_HOST not set; skipping MySQL injection test")
	}
	ds := &orm.TDataSource{
		DbType:   "mysql",
		Host:     host,
		UserName: os.Getenv("MYSQL_TEST_USER"),
		Password: os.Getenv("MYSQL_TEST_PASS"),
		DbName:   "test_orm",
	}
	o, err := orm.New(orm.WithDataSource(ds))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_ = o.NewSession().DDL().AllowUnsafe().DropTable("sec_test_user")
	if _, err := o.SyncModel("", new(SecTestUser)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	return o
}

func setupPostgres(t *testing.T) *orm.TOrm {
	t.Helper()
	host := os.Getenv("POSTGRES_TEST_HOST")
	if host == "" {
		t.Skip("POSTGRES_TEST_HOST not set; skipping Postgres injection test")
	}
	ds := &orm.TDataSource{
		DbType:   "postgres",
		Host:     host,
		UserName: os.Getenv("POSTGRES_TEST_USER"),
		Password: os.Getenv("POSTGRES_TEST_PASS"),
		DbName:   "test_orm",
		SSLMode:  "disable",
	}
	o, err := orm.New(orm.WithDataSource(ds))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_ = o.NewSession().DDL().AllowUnsafe().DropTable("sec_test_user")
	if _, err := o.SyncModel("", new(SecTestUser)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	return o
}
