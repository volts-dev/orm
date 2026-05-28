package orm

import (
	"errors"
	"testing"
	"time"

	ormerr "github.com/volts-dev/orm/errors"
	_ "modernc.org/sqlite"
)

// ---- Task 9: softDeleteMode type + IncludeDeleted / OnlyDeleted modifiers ----

func TestSoftDeleteMode_DefaultIsFilterActive(t *testing.T) {
	s := &TSession{}
	if s.softDeleteMode != softDeleteFilterActive {
		t.Fatalf("default mode should be filterActive (0), got %d", s.softDeleteMode)
	}
}

func TestIncludeDeleted_ReturnsSelfAndSetsMode(t *testing.T) {
	s := &TSession{}
	out := s.IncludeDeleted()
	if out != s {
		t.Fatal("IncludeDeleted should return self for chaining")
	}
	if s.softDeleteMode != softDeleteIncludeAll {
		t.Fatalf("expected softDeleteIncludeAll, got %d", s.softDeleteMode)
	}
}

func TestOnlyDeleted_ReturnsSelfAndSetsMode(t *testing.T) {
	s := &TSession{}
	out := s.OnlyDeleted()
	if out != s {
		t.Fatal("OnlyDeleted should return self for chaining")
	}
	if s.softDeleteMode != softDeleteOnlyDeleted {
		t.Fatalf("expected softDeleteOnlyDeleted, got %d", s.softDeleteMode)
	}
}

// ---- Shared test model ----

type SoftDeleteUser struct {
	TModel    `table:"name('sd_user')"`
	Id        int64     `field:"pk autoincr"`
	Name      string    `field:"varchar() size(64)"`
	DeletedAt time.Time `field:"datetime() deleted"`
}

var sdModelName string // populated by first setupSoftDeleteOrm call

func setupSoftDeleteOrm(t *testing.T) *TOrm {
	t.Helper()
	ds := &TDataSource{DbType: "sqlite", DbName: ":memory:"}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	names, err := o.SyncModel("", new(SoftDeleteUser))
	if err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("SyncModel returned no model names")
	}
	sdModelName = names[0]
	return o
}

// ---- Task 10: Read path auto-filter ----

func TestRead_AutoFiltersSoftDeleted(t *testing.T) {
	o := setupSoftDeleteOrm(t)
	defer o.Close()

	for _, n := range []string{"alice", "bob", "carol"} {
		if _, err := o.Model(sdModelName).Create(map[string]any{"name": n}); err != nil {
			t.Fatalf("Create %q: %v", n, err)
		}
	}

	// soft-delete bob via direct Write
	_, err := o.Model(sdModelName).Where("name=?", "bob").Write(map[string]any{
		"deleted_at": time.Now(),
	})
	if err != nil {
		t.Fatalf("soft-delete write: %v", err)
	}

	// default Read: should return 2 (alice + carol, not bob)
	ds, err := o.Model(sdModelName).Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if ds.Count() != 2 {
		t.Errorf("default Read count: got %d, want 2 (bob should be filtered)", ds.Count())
	}
}

func TestRead_IncludeDeleted_ShowsAll(t *testing.T) {
	o := setupSoftDeleteOrm(t)
	defer o.Close()

	for _, n := range []string{"alice", "bob"} {
		if _, err := o.Model(sdModelName).Create(map[string]any{"name": n}); err != nil {
			t.Fatalf("Create %q: %v", n, err)
		}
	}
	_, _ = o.Model(sdModelName).Where("name=?", "bob").Write(map[string]any{"deleted_at": time.Now()})

	ds, err := o.Model(sdModelName).IncludeDeleted().Read()
	if err != nil {
		t.Fatalf("IncludeDeleted Read: %v", err)
	}
	if ds.Count() != 2 {
		t.Errorf("IncludeDeleted count: got %d, want 2", ds.Count())
	}
}

func TestRead_OnlyDeleted_ShowsOnlySoftDeleted(t *testing.T) {
	o := setupSoftDeleteOrm(t)
	defer o.Close()

	for _, n := range []string{"alice", "bob", "carol"} {
		if _, err := o.Model(sdModelName).Create(map[string]any{"name": n}); err != nil {
			t.Fatalf("Create %q: %v", n, err)
		}
	}
	_, _ = o.Model(sdModelName).Where("name=?", "bob").Write(map[string]any{"deleted_at": time.Now()})

	ds, err := o.Model(sdModelName).OnlyDeleted().Read()
	if err != nil {
		t.Fatalf("OnlyDeleted Read: %v", err)
	}
	if ds.Count() != 1 {
		t.Errorf("OnlyDeleted count: got %d, want 1 (only bob)", ds.Count())
	}
}

func TestRead_NoFilterWhenModelHasNoDeletedField(t *testing.T) {
	o := setupTestOrm(t)
	defer o.Close()
	// BenchModel has no deleted field — Read must not inject any filter
	if _, err := o.Model("bench.model").Read(); err != nil {
		t.Fatalf("Read on model without deleted field: %v", err)
	}
}

// ---- Task 11: SoftDelete() + multi-deleted detection ----

func TestSoftDelete_HappyPath(t *testing.T) {
	o := setupSoftDeleteOrm(t)
	defer o.Close()

	id, _ := o.Model(sdModelName).Create(map[string]any{"name": "alice"})

	n, err := o.Model(sdModelName).Ids(id).SoftDelete()
	if err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	if n != 1 {
		t.Errorf("SoftDelete count: got %d, want 1", n)
	}

	// default Read should not see alice
	ds, _ := o.Model(sdModelName).Read()
	if ds.Count() != 0 {
		t.Errorf("after SoftDelete, default Read should return 0, got %d", ds.Count())
	}

	// IncludeDeleted should see alice with deleted_at set
	ds, _ = o.Model(sdModelName).Ids(id).IncludeDeleted().Read()
	if ds.Count() != 1 {
		t.Fatalf("IncludeDeleted after SoftDelete: expected 1, got %d", ds.Count())
	}
	if ds.Record().GetByField("deleted_at") == nil {
		t.Error("deleted_at should be set after SoftDelete")
	}
}

func TestSoftDelete_NoDeletedFieldReturnsError(t *testing.T) {
	o := setupTestOrm(t)
	defer o.Close()

	id, _ := o.Model("bench.model").Create(map[string]any{"name": "x", "age": 0})
	_, err := o.Model("bench.model").Ids(id).SoftDelete()
	if !errors.Is(err, ormerr.ErrNoSoftDelete) {
		t.Fatalf("expected ErrNoSoftDelete, got: %v", err)
	}
}

func TestSoftDelete_NoConditionReturnsErrUnsafe(t *testing.T) {
	o := setupSoftDeleteOrm(t)
	defer o.Close()

	_, err := o.Model(sdModelName).SoftDelete()
	if !errors.Is(err, ormerr.ErrUnsafe) {
		t.Fatalf("expected ErrUnsafe (no condition), got: %v", err)
	}
}

func TestSoftDelete_AllowUnsafeSoftDeletesAll(t *testing.T) {
	o := setupSoftDeleteOrm(t)
	defer o.Close()

	for _, n := range []string{"a", "b", "c"} {
		_, _ = o.Model(sdModelName).Create(map[string]any{"name": n})
	}

	n, err := o.Model(sdModelName).AllowUnsafe().SoftDelete()
	if err != nil {
		t.Fatalf("AllowUnsafe SoftDelete: %v", err)
	}
	if n != 3 {
		t.Errorf("expected 3 soft-deleted, got %d", n)
	}

	ds, _ := o.Model(sdModelName).Read()
	if ds.Count() != 0 {
		t.Errorf("after AllowUnsafe SoftDelete, default Read should return 0, got %d", ds.Count())
	}
}

func TestRestore_ViaWriteWithNilDeletedAt(t *testing.T) {
	o := setupSoftDeleteOrm(t)
	defer o.Close()

	id, _ := o.Model(sdModelName).Create(map[string]any{"name": "alice"})
	_, _ = o.Model(sdModelName).Ids(id).SoftDelete()

	// restore: Write deleted_at = nil via Nullable() to ensure SQL NULL is stored
	_, err := o.Model(sdModelName).Ids(id).Nullable("deleted_at").Write(map[string]any{"deleted_at": nil})
	if err != nil {
		t.Fatalf("restore Write: %v", err)
	}

	ds, _ := o.Model(sdModelName).Ids(id).Read()
	if ds.Count() != 1 {
		t.Errorf("after restore, default Read should return 1, got %d", ds.Count())
	}
}

func TestSyncModel_MultipleDeletedFieldsRejected(t *testing.T) {
	type BrokenModel struct {
		TModel    `table:"name('broken_sd')"`
		Id        int64     `field:"pk autoincr"`
		DeletedAt time.Time `field:"datetime() deleted"`
		ExpiredAt time.Time `field:"datetime() deleted"` // second deleted tag
	}

	ds := &TDataSource{DbType: "sqlite", DbName: ":memory:"}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer o.Close()

	_, err = o.SyncModel("", new(BrokenModel))
	if !errors.Is(err, ormerr.ErrSoftDeleteMisconfigured) {
		t.Fatalf("expected ErrSoftDeleteMisconfigured for two deleted fields, got: %v", err)
	}
}
