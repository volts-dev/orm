package test

import (
	"fmt"
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

// ── Step 2: Create ───────────────────────────────────────────────────────────

func pgDeepCreate(t *testing.T, chain *Testchain) {
	userModel, err := chain.Orm.GetModel("user_model")
	if err != nil {
		t.Fatal(err)
	}
	companyModel, err := chain.Orm.GetModel("company_model")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("basic/struct", func(t *testing.T) {
		data := &UserModel{Name: fmt.Sprintf("PgDeepUser1_%d", uniqueSuffix()), Title: "Admin"}
		id, err := userModel.Records().Create(data)
		if err != nil {
			t.Fatalf("Create(struct) failed: %v", err)
		}
		if id == nil {
			t.Fatal("Create(struct) returned nil ID")
		}
		t.Logf("Created user id=%v", id)
	})

	t.Run("basic/map", func(t *testing.T) {
		id, err := companyModel.Records().Create(map[string]any{
			"name": fmt.Sprintf("PgDeepCompany1_%d", uniqueSuffix()),
		})
		if err != nil {
			t.Fatalf("Create(map) failed: %v", err)
		}
		if id == nil {
			t.Fatal("Create(map) returned nil ID")
		}
	})

	t.Run("data_types", func(t *testing.T) {
		data := &UserModel{
			Name:      fmt.Sprintf("PgDeepDataTypes_%d", uniqueSuffix()),
			Title:     "TypeTest",
			Int:       42,
			Bool:      true,
			Float:     3.14,
			Text:      "long text value",
			Bin:       []byte{1, 2, 3},
			Selection: "person",
		}
		id, err := userModel.Records().Create(data)
		if err != nil {
			t.Fatalf("Create(data_types) failed: %v", err)
		}

		ds, err := userModel.Records().Ids(id).Read()
		if err != nil {
			t.Fatalf("Read after create failed: %v", err)
		}
		if ds.IsEmpty() {
			t.Fatal("Read returned empty dataset after create")
		}
		t.Logf("data_types read-back OK: id=%v count=%d", id, ds.Count())
	})

	t.Run("edge/duplicate_unique", func(t *testing.T) {
		name := fmt.Sprintf("PgDeepDupUnique_%d", uniqueSuffix())
		data1 := &UserModel{Name: name, Title: "first"}
		_, err := userModel.Records().Create(data1)
		if err != nil {
			t.Fatalf("first create failed: %v", err)
		}
		data2 := &UserModel{Name: name, Title: "second"}
		_, err = userModel.Records().Create(data2)
		if err == nil {
			t.Fatal("duplicate unique Name should have returned an error, got nil")
		}
		t.Logf("duplicate_unique correctly rejected: %v", err)
	})

	t.Run("edge/invalid_field", func(t *testing.T) {
		id, err := companyModel.Records().Create(map[string]any{
			"name":        fmt.Sprintf("PgDeepInvalidField_%d", uniqueSuffix()),
			"wrong_field": "should_be_ignored",
		})
		if err != nil {
			t.Fatalf("Create with invalid field should succeed, got: %v", err)
		}
		if id == nil {
			t.Fatal("Create with invalid field returned nil ID")
		}
	})

	t.Run("onconflict/do_nothing", func(t *testing.T) {
		name := fmt.Sprintf("PgDeepConflictDN_%d", uniqueSuffix())
		data := &CompanyModel{Name: name}
		id1, err := companyModel.Records().Create(data)
		if err != nil {
			t.Fatalf("initial create failed: %v", err)
		}

		id2, err := companyModel.Records().OnConflict(&orm.OnConflict{
			Fields:    []string{"name"},
			DoNothing: true,
		}).Create(data)
		if err != nil {
			t.Fatalf("OnConflict DoNothing returned error: %v", err)
		}
		t.Logf("do_nothing: id1=%v id2=%v", id1, id2)
	})

	t.Run("onconflict/do_update", func(t *testing.T) {
		name := fmt.Sprintf("PgDeepConflictDU_%d", uniqueSuffix())
		data := &CompanyModel{Name: name}
		_, err := companyModel.Records().Create(data)
		if err != nil {
			t.Fatalf("initial create failed: %v", err)
		}

		_, err = companyModel.Records().OnConflict(&orm.OnConflict{
			DoUpdates: []string{"name"},
		}).Create(data)
		if err != nil {
			t.Fatalf("OnConflict DoUpdate returned error: %v", err)
		}
	})

	t.Run("onconflict/update_all", func(t *testing.T) {
		name := fmt.Sprintf("PgDeepConflictUA_%d", uniqueSuffix())
		data := &CompanyModel{Name: name}
		_, err := companyModel.Records().Create(data)
		if err != nil {
			t.Fatalf("initial create failed: %v", err)
		}

		_, err = companyModel.Records().OnConflict(&orm.OnConflict{
			UpdateAll: true,
		}).Create(data)
		if err != nil {
			t.Fatalf("OnConflict UpdateAll returned error: %v", err)
		}
	})
}
func pgDeepQuery(t *testing.T, chain *Testchain)       {}
func pgDeepConditions(t *testing.T, chain *Testchain)  {}
func pgDeepWrite(t *testing.T, chain *Testchain)       {}
func pgDeepTransaction(t *testing.T, chain *Testchain) {}
func pgDeepDelete(t *testing.T, chain *Testchain)      {}
