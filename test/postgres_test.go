package test

import (
	"runtime/debug"
	"testing"

	_ "github.com/lib/pq"
	"github.com/volts-dev/orm"
)

// defaultPostgresSource returns the hardcoded default Postgres connection.
func defaultPostgresSource() *orm.TDataSource {
	return &orm.TDataSource{
		DbType:   "postgres",
		Host:     "localhost",
		Port:     "5432",
		UserName: "postgres",
		Password: "postgres",
		DbName:   TEST_DB_NAME,
		SSLMode:  "disable",
	}
}

var pgSuffix int64

func uniqueSuffix() int64 {
	pgSuffix++
	return pgSuffix
}

// TestPostgresDeep runs comprehensive deep tests against PostgreSQL.
// Run with: go test -v -run TestPostgresDeep ./test/
func TestPostgresDeep(t *testing.T) {
	ds := defaultPostgresSource()
	DataSource = ds

	chain := NewTest(t)
	chain.ShowSql(true)

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("panic in TestPostgresDeep: %v\n%s", r, debug.Stack())
		}
	}()

	t.Run("1_Init", func(t *testing.T) {
		pgDeepInit(t, chain)
	})

	t.Run("2_Create", func(t *testing.T) {
		pgDeepCreate(t, chain)
	})

	t.Run("3_Query", func(t *testing.T) {
		pgDeepQuery(t, chain)
	})

	t.Run("4_Conditions", func(t *testing.T) {
		pgDeepConditions(t, chain)
	})

	t.Run("5_Write", func(t *testing.T) {
		pgDeepWrite(t, chain)
	})

	t.Run("6_Transaction", func(t *testing.T) {
		pgDeepTransaction(t, chain)
	})

	t.Run("7_Delete", func(t *testing.T) {
		pgDeepDelete(t, chain)
	})
}

// ── Step 1: Init ─────────────────────────────────────────────────────────────

func pgDeepInit(t *testing.T, chain *Testchain) {
	t.Run("connection", func(t *testing.T) {
		if !chain.Orm.IsExist(TEST_DB_NAME) {
			t.Fatalf("database %q does not exist or connection failed", TEST_DB_NAME)
		}
	})

	t.Run("sync_model_creates_tables", func(t *testing.T) {
		chain.Reset()

		for _, tableName := range []string{"partner_model", "company_model", "user_model"} {
			exists, err := chain.Orm.NewSession().IsExist(tableName)
			if err != nil {
				t.Fatalf("IsExist(%s) error: %v", tableName, err)
			}
			if !exists {
				t.Fatalf("table %q was not created by SyncModel", tableName)
			}
		}
	})

	t.Run("on_build_fields", func(t *testing.T) {
		model, err := chain.Orm.GetModel("user_model")
		if err != nil {
			t.Fatal(err)
		}
		if f := model.GetFieldByName("full_name"); f == nil {
			t.Fatal("OnBuildFields: computed field 'full_name' not registered on user_model")
		}
		if f := model.GetFieldByName("title"); f == nil {
			t.Fatal("OnBuildFields: field 'title' not found on user_model")
		}
		if f := model.GetFieldByName("help"); f == nil {
			t.Fatal("OnBuildFields: field 'help' not found on user_model")
		}
	})
}

// ── Stubs for remaining steps (implemented in subsequent tasks) ───────────────

func pgDeepCreate(t *testing.T, chain *Testchain)      {}
func pgDeepQuery(t *testing.T, chain *Testchain)       {}
func pgDeepConditions(t *testing.T, chain *Testchain)  {}
func pgDeepWrite(t *testing.T, chain *Testchain)       {}
func pgDeepTransaction(t *testing.T, chain *Testchain) {}
func pgDeepDelete(t *testing.T, chain *Testchain)      {}
