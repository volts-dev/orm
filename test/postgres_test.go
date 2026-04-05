package test

import (
	"fmt"
	"runtime/debug"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/volts-dev/orm"
	"github.com/volts-dev/orm/domain"
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

// pgSuffix is seeded from the current Unix timestamp (seconds) so each test
// run produces names that are unique across runs and won't collide with
// leftover rows from previous (possibly failed) runs.
var pgSuffix = atomic.Int64{}

func init() {
	pgSuffix.Store(time.Now().Unix() * 1000)
}

func uniqueSuffix() int64 {
	return pgSuffix.Add(1)
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
// ── Step 3: Query ───────────────────────────────────────────────────────────────

func pgDeepQuery(t *testing.T, chain *Testchain) {
	model, err := chain.Orm.GetModel("user_model")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("read/all", func(t *testing.T) {
		ds, err := model.Records().Read()
		if err != nil {
			t.Fatalf("Read() error: %v", err)
		}
		if ds.IsEmpty() {
			t.Fatal("Read() returned empty dataset — ensure Step 2 ran first")
		}
		t.Logf("Read() returned %d records", ds.Count())
	})

	t.Run("read/select_fields", func(t *testing.T) {
		ds, err := model.Records().Select("id", "name").Read()
		if err != nil {
			t.Fatalf("Read().Select() error: %v", err)
		}
		if ds.IsEmpty() {
			t.Fatal("Read().Select() returned empty dataset")
		}
	})

	t.Run("read/as_struct", func(t *testing.T) {
		ds, err := model.Records().Select("id", "name").Read()
		if err != nil {
			t.Fatal(err)
		}
		u := new(UserModel)
		if err := ds.Record().AsStruct(u); err != nil {
			t.Fatalf("AsStruct() failed: %v", err)
		}
		t.Logf("AsStruct OK: id=%v name=%q", u.Id, u.Name)
	})

	t.Run("search/all", func(t *testing.T) {
		ids, total, err := model.Records().Search()
		if err != nil {
			t.Fatalf("Search() error: %v", err)
		}
		if len(ids) == 0 {
			t.Fatal("Search() returned no IDs")
		}
		if int64(len(ids)) != total {
			t.Fatalf("Search() id count %d != total %d", len(ids), total)
		}
	})

	t.Run("search/where", func(t *testing.T) {
		allIds, _, err := model.Records().Search()
		if err != nil || len(allIds) == 0 {
			t.Fatal("need records for search/where")
		}
		ids, _, err := model.Records().Where("id=?", allIds[0]).Search()
		if err != nil {
			t.Fatalf("Search(where) error: %v", err)
		}
		if len(ids) != 1 {
			t.Fatalf("Search(where id=?) returned %d records, expected 1", len(ids))
		}
	})

	t.Run("search/limit", func(t *testing.T) {
		ids, _, err := model.Records().Limit(3).Search()
		if err != nil {
			t.Fatalf("Search(limit=3) error: %v", err)
		}
		if len(ids) > 3 {
			t.Fatalf("Limit(3) returned %d IDs", len(ids))
		}
	})

	t.Run("count/all", func(t *testing.T) {
		n, err := model.Records().Count()
		if err != nil {
			t.Fatalf("Count() error: %v", err)
		}
		if n < 0 {
			t.Fatalf("Count() returned negative: %d", n)
		}
		t.Logf("Count()=%d", n)
	})

	t.Run("count/where", func(t *testing.T) {
		total, err := model.Records().Count()
		if err != nil {
			t.Fatal(err)
		}
		filtered, err := model.Records().Where("id>?", 0).Count()
		if err != nil {
			t.Fatalf("Count(where id>0) error: %v", err)
		}
		if filtered != total {
			t.Fatalf("Count(where id>0)=%d != Count()=%d", filtered, total)
		}
	})

	t.Run("limit/basic", func(t *testing.T) {
		ds, err := model.Records().Limit(3).Read()
		if err != nil {
			t.Fatalf("Limit(3).Read() error: %v", err)
		}
		if ds.Count() > 3 {
			t.Fatalf("Limit(3) returned %d records", ds.Count())
		}
	})

	t.Run("limit/offset", func(t *testing.T) {
		total, _ := model.Records().Count()
		if total <= 3 {
			t.Skip("need more than 3 records for offset test")
		}
		ds, err := model.Records().Limit(3, 1).Read()
		if err != nil {
			t.Fatalf("Limit(3,1).Read() error: %v", err)
		}
		if ds.Count() > 3 {
			t.Fatalf("Limit(3,1) returned %d records", ds.Count())
		}
	})
}
// ── Step 4: Conditions ───────────────────────────────────────────────────────

func pgDeepConditions(t *testing.T, chain *Testchain) {
	model, err := chain.Orm.GetModel("user_model")
	if err != nil {
		t.Fatal(err)
	}

	allIds, _, err := model.Records().Search()
	if err != nil || len(allIds) == 0 {
		t.Fatal("Conditions: need existing records")
	}

	t.Run("where/exact", func(t *testing.T) {
		ds, err := model.Records().Where("id=?", allIds[0]).Read()
		if err != nil {
			t.Fatalf("Where(id=?) error: %v", err)
		}
		if ds.IsEmpty() {
			t.Fatal("Where(id=?) returned empty dataset")
		}
	})

	t.Run("where/chained", func(t *testing.T) {
		ds, err := model.Records().Where("id=?", allIds[0]).Where("id>?", 0).Read()
		if err != nil {
			t.Fatalf("chained Where error: %v", err)
		}
		if ds.IsEmpty() {
			t.Fatal("chained Where returned empty dataset")
		}
	})

	t.Run("and/domain_node", func(t *testing.T) {
		node := domain.NewDomainNode()
		for i := 0; i < 3; i++ {
			node.AND(domain.NewDomainNode("id", ">=", 0))
		}
		ds, err := model.Records().Domain(node).Read()
		if err != nil {
			t.Fatalf("Domain(AND node) error: %v", err)
		}
		if ds.IsEmpty() {
			t.Fatal("Domain(AND node) returned empty dataset")
		}
	})

	t.Run("or/two_ids", func(t *testing.T) {
		if len(allIds) < 2 {
			t.Skip("need at least 2 records for OR test")
		}
		ds, err := model.Records().Where("id=?", allIds[0]).Or("id=?", allIds[1]).Read()
		if err != nil {
			t.Fatalf("Or() error: %v", err)
		}
		if ds.Count() < 1 {
			t.Fatal("Or() returned no records")
		}
	})

	t.Run("in/values", func(t *testing.T) {
		name := fmt.Sprintf("PgDeepInTest_%d", uniqueSuffix())
		_, err := model.Records().Create(&UserModel{Name: name, Title: "InTitle"})
		if err != nil {
			t.Fatalf("Create for In test failed: %v", err)
		}
		ds, err := model.Records().In("title", "InTitle").Read()
		if err != nil {
			t.Fatalf("In() error: %v", err)
		}
		if ds.IsEmpty() {
			t.Fatal("In('title','InTitle') returned empty dataset")
		}
	})

	t.Run("notin/values", func(t *testing.T) {
		if len(allIds) < 2 {
			t.Skip("need at least 2 records for NotIn test")
		}
		excluded := allIds[:1]
		total, _ := model.Records().Count()
		ds, err := model.Records().NotIn("id", excluded...).Read()
		if err != nil {
			t.Fatalf("NotIn() error: %v", err)
		}
		if ds.Count() >= total {
			t.Fatalf("NotIn did not filter: got %d, total %d", ds.Count(), total)
		}
	})

	t.Run("domain/gt", func(t *testing.T) {
		ds, err := model.Records().Domain(`[('id', '>', 0)]`).Read()
		if err != nil {
			t.Fatalf("Domain([id>0]) error: %v", err)
		}
		if ds.IsEmpty() {
			t.Fatal("Domain([id>0]) returned empty dataset")
		}
	})

	t.Run("domain/in", func(t *testing.T) {
		if len(allIds) < 2 {
			t.Skip("need at least 2 records for domain/in")
		}
		idStr := fmt.Sprintf("%v,%v", allIds[0], allIds[1])
		ds, err := model.Records().Domain(fmt.Sprintf("[('id', 'in', [%s])]", idStr)).Read()
		if err != nil {
			t.Fatalf("Domain([id in [...]]) error: %v", err)
		}
		if ds.Count() > 2 {
			t.Fatalf("Domain in returned %d records, expected <= 2", ds.Count())
		}
	})
}

// ── Step 5: Write ────────────────────────────────────────────────────────────

func pgDeepWrite(t *testing.T, chain *Testchain) {
	model, err := chain.Orm.GetModel("user_model")
	if err != nil {
		t.Fatal(err)
	}

	ids := make([]any, 0, 5)
	for i := 0; i < 5; i++ {
		data := &UserModel{
			Name:  fmt.Sprintf("PgDeepWrite_%d_%d", i, uniqueSuffix()),
			Title: "Original",
		}
		id, err := model.Records().Create(data)
		if err != nil {
			t.Fatalf("pre-create for Write test failed: %v", err)
		}
		ids = append(ids, id)
	}

	t.Run("write/by_ids_map", func(t *testing.T) {
		effect, err := model.Records().Ids(ids...).Write(map[string]any{
			"title": "WrittenByMap",
		})
		if err != nil {
			t.Fatalf("Write(map) error: %v", err)
		}
		if effect != int64(len(ids)) {
			t.Fatalf("Write(map) effect=%d, expected %d", effect, len(ids))
		}
	})

	t.Run("write/by_id_struct", func(t *testing.T) {
		data := &UserModel{
			Name:  fmt.Sprintf("PgDeepWriteStruct_%d", uniqueSuffix()),
			Title: "WrittenByStruct",
		}
		effect, err := model.Records().Ids(ids[0]).Write(data)
		if err != nil {
			t.Fatalf("Write(struct) error: %v", err)
		}
		if effect != 1 {
			t.Fatalf("Write(struct) effect=%v, expected 1", effect)
		}
	})

	t.Run("write/verify_update", func(t *testing.T) {
		writeTitle := "PgVerifiedTitle"
		_, err := model.Records().Ids(ids[0]).Write(map[string]any{
			"title": writeTitle,
		})
		if err != nil {
			t.Fatalf("Write for verify failed: %v", err)
		}
		ds, err := model.Records().Ids(ids[0]).Read()
		if err != nil {
			t.Fatalf("Read after Write failed: %v", err)
		}
		got := ds.FieldByName("title").AsString()
		if got != writeTitle {
			t.Fatalf("Read after Write: got title=%q, want %q", got, writeTitle)
		}
	})
}

// ── Step 6: Transaction ──────────────────────────────────────────────────────

func pgDeepTransaction(t *testing.T, chain *Testchain) {
	userModel, err := chain.Orm.GetModel("user_model")
	if err != nil {
		t.Fatal(err)
	}
	companyModel, err := chain.Orm.GetModel("company_model")
	if err != nil {
		t.Fatal(err)
	}
	partnerModel, err := chain.Orm.GetModel("partner_model")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("rollback/single", func(t *testing.T) {
		name := fmt.Sprintf("TxRollbackSingle_%d", uniqueSuffix())
		ss := chain.Orm.NewSession()
		defer ss.Close()
		if err := ss.Begin(); err != nil {
			t.Fatalf("Begin failed: %v", err)
		}
		_, err := ss.Model("user_model").Create(&UserModel{Name: name, Title: "tx"})
		if err != nil {
			t.Fatalf("Create in tx failed: %v", err)
		}
		if err := ss.Rollback(nil); err != nil {
			t.Logf("Rollback returned: %v", err)
		}
		count, err := userModel.Records().Where("name=?", name).Count()
		if err != nil {
			t.Fatalf("Count after rollback failed: %v", err)
		}
		if count != 0 {
			t.Fatalf("rollback/single FAILED: record %q persists (%d found)", name, count)
		}
	})

	t.Run("commit/single", func(t *testing.T) {
		name := fmt.Sprintf("TxCommitSingle_%d", uniqueSuffix())
		ss := chain.Orm.NewSession()
		defer ss.Close()
		if err := ss.Begin(); err != nil {
			t.Fatalf("Begin failed: %v", err)
		}
		_, err := ss.Model("user_model").Create(&UserModel{Name: name, Title: "tx"})
		if err != nil {
			t.Fatalf("Create in tx failed: %v", err)
		}
		if err := ss.Commit(); err != nil {
			t.Fatalf("Commit failed: %v", err)
		}
		count, err := userModel.Records().Where("name=?", name).Count()
		if err != nil {
			t.Fatalf("Count after commit failed: %v", err)
		}
		if count != 1 {
			t.Fatalf("commit/single FAILED: expected 1 record, found %d", count)
		}
	})

	t.Run("rollback/multi_partial_failure", func(t *testing.T) {
		// names[3] duplicates names[1] to trigger unique constraint error
		names := make([]string, 4)
		for i := range names {
			names[i] = fmt.Sprintf("TxMultiPF_%d_%d", i, uniqueSuffix())
		}
		names[3] = names[1]

		ss := chain.Orm.NewSession()
		defer ss.Close()
		if err := ss.Begin(); err != nil {
			t.Fatalf("Begin failed: %v", err)
		}

		var txErr error
		for i, name := range names {
			_, err := userModel.Tx(ss).Create(&UserModel{Name: name, Title: "multi"})
			if err != nil {
				t.Logf("Create[%d] failed as expected (unique violation): %v", i, err)
				txErr = err
				break
			}
		}

		if txErr == nil {
			t.Log("WARNING: no unique constraint error triggered; rolling back anyway")
		}

		if err := ss.Rollback(txErr); err != nil {
			t.Logf("Rollback returned: %v", err)
		}

		for _, name := range names[:3] {
			count, err := userModel.Records().Where("name=?", name).Count()
			if err != nil {
				t.Fatalf("Count(%q) after rollback failed: %v", name, err)
			}
			if count != 0 {
				t.Fatalf("rollback/multi_partial_failure: record %q persists after rollback (%d found)", name, count)
			}
		}
		t.Log("rollback/multi_partial_failure: all records correctly removed")
	})

	t.Run("rollback/related_records", func(t *testing.T) {
		partnerName := fmt.Sprintf("TxPartnerRollback_%d", uniqueSuffix())
		companyName := fmt.Sprintf("TxCompanyRollback_%d", uniqueSuffix())

		ss := chain.Orm.NewSession()
		defer ss.Close()
		if err := ss.Begin(); err != nil {
			t.Fatalf("Begin failed: %v", err)
		}

		_, err := partnerModel.Tx(ss).Create(&PartnerModel{Name: partnerName})
		if err != nil {
			ss.Rollback(err)
			t.Fatalf("Create partner in tx failed: %v", err)
		}

		_, err = companyModel.Tx(ss).Create(&CompanyModel{Name: companyName})
		if err != nil {
			ss.Rollback(err)
			t.Fatalf("Create company in tx failed: %v", err)
		}

		if err := ss.Rollback(nil); err != nil {
			t.Logf("Rollback returned: %v", err)
		}

		pCount, err := partnerModel.Records().Where("name=?", partnerName).Count()
		if err != nil {
			t.Fatalf("Count(partner) after rollback failed: %v", err)
		}
		if pCount != 0 {
			t.Fatalf("rollback/related_records: partner %q persists (%d found)", partnerName, pCount)
		}

		cCount, err := companyModel.Records().Where("name=?", companyName).Count()
		if err != nil {
			t.Fatalf("Count(company) after rollback failed: %v", err)
		}
		if cCount != 0 {
			t.Fatalf("rollback/related_records: company %q persists (%d found)", companyName, cCount)
		}
	})

	t.Run("visibility/in_tx", func(t *testing.T) {
		name := fmt.Sprintf("TxVisibility_%d", uniqueSuffix())
		ss := chain.Orm.NewSession()
		defer ss.Close()
		if err := ss.Begin(); err != nil {
			t.Fatalf("Begin failed: %v", err)
		}
		_, err := ss.Model("user_model").Create(&UserModel{Name: name, Title: "vis"})
		if err != nil {
			ss.Rollback(err)
			t.Fatalf("Create in tx failed: %v", err)
		}
		count, err := ss.Model("user_model").Where("name=?", name).Count()
		if err != nil {
			ss.Rollback(err)
			t.Fatalf("Count within tx failed: %v", err)
		}
		if count != 1 {
			t.Logf("WARNING: in-tx visibility returned count=%d (expected 1)", count)
		}
		ss.Rollback(nil)
	})
}

// ── Step 7: Delete ───────────────────────────────────────────────────────────

func pgDeepDelete(t *testing.T, chain *Testchain) {
	model, err := chain.Orm.GetModel("user_model")
	if err != nil {
		t.Fatal(err)
	}

	ids := make([]any, 0, 5)
	for i := 0; i < 5; i++ {
		data := &UserModel{
			Name:  fmt.Sprintf("PgDeepDelete_%d_%d", i, uniqueSuffix()),
			Title: "ToDelete",
		}
		id, err := model.Records().Create(data)
		if err != nil {
			t.Fatalf("pre-create for Delete test failed: %v", err)
		}
		ids = append(ids, id)
	}

	t.Run("delete/single", func(t *testing.T) {
		before, _ := model.Records().Count()
		effect, err := model.Records().Ids(ids[0]).Delete()
		if err != nil {
			t.Fatalf("Delete(single) error: %v", err)
		}
		if effect != 1 {
			t.Fatalf("Delete(single) effect=%d, expected 1", effect)
		}
		after, _ := model.Records().Count()
		if after != before-1 {
			t.Fatalf("Delete(single): count before=%d after=%d (expected before-1)", before, after)
		}
	})

	t.Run("delete/batch", func(t *testing.T) {
		batchIds := ids[1:]
		before, _ := model.Records().Count()
		effect, err := model.Records().Ids(batchIds...).Delete()
		if err != nil {
			t.Fatalf("Delete(batch) error: %v", err)
		}
		if effect != int64(len(batchIds)) {
			t.Fatalf("Delete(batch) effect=%d, expected %d", effect, len(batchIds))
		}
		after, _ := model.Records().Count()
		if after != before-int(len(batchIds)) {
			t.Fatalf("Delete(batch): count before=%d after=%d (expected before-%d)", before, after, len(batchIds))
		}
	})

	t.Run("delete/nonexistent", func(t *testing.T) {
		effect, err := model.Records().Ids(int64(999999999)).Delete()
		if err != nil {
			t.Fatalf("Delete(nonexistent) should not error, got: %v", err)
		}
		if effect != 0 {
			t.Logf("Delete(nonexistent) effect=%d (expected 0)", effect)
		}
	})
}
