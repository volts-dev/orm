package orm

import (
	"errors"
	"testing"

	ormerr "github.com/volts-dev/orm/errors"
	_ "modernc.org/sqlite"
)

func setupTestOrm(t *testing.T) *TOrm {
	t.Helper()
	ds := &TDataSource{DbType: "sqlite", DbName: ":memory:"}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := o.SyncModel("", new(BenchModel)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	return o
}

func TestAllowUnsafe_ReturnsSelfForChain(t *testing.T) {
	s := &TSession{}
	out := s.AllowUnsafe()
	if out != s {
		t.Fatal("AllowUnsafe should return self for chaining")
	}
	if !s.allowUnsafe {
		t.Fatal("AllowUnsafe should set allowUnsafe=true")
	}
}

func TestAllowUnsafe_Sticky(t *testing.T) {
	s := &TSession{}
	s.AllowUnsafe()
	if !s.allowUnsafe {
		t.Fatal("AllowUnsafe should be sticky on the session")
	}
}

func TestHasCondition_NoCondition(t *testing.T) {
	s := newTestSessionForCondition()
	if s.hasCondition() {
		t.Fatal("empty Statement should have no condition")
	}
}

func TestHasCondition_WithIds(t *testing.T) {
	s := newTestSessionForCondition()
	s.Statement.IdParam = []any{int64(1)}
	if !s.hasCondition() {
		t.Fatal("Statement with IdParam should have condition")
	}
}

func newTestSessionForCondition() *TSession {
	return &TSession{
		Statement: TStatement{
			IdParam: []any{},
			Params:  []any{},
		},
	}
}

func TestDelete_NoConditionReturnsErrUnsafe(t *testing.T) {
	o := setupTestOrm(t)
	defer o.Close()

	_, err := o.Model("bench.model").Delete()
	if !errors.Is(err, ormerr.ErrUnsafe) {
		t.Fatalf("expected ErrUnsafe, got: %v", err)
	}
}

func TestDelete_AllowUnsafeBypasses(t *testing.T) {
	o := setupTestOrm(t)
	defer o.Close()

	for i := 0; i < 3; i++ {
		_, _ = o.Model("bench.model").Create(map[string]any{"name": "x", "age": i})
	}

	n, err := o.Model("bench.model").AllowUnsafe().Delete()
	if err != nil {
		t.Fatalf("AllowUnsafe Delete should succeed, got: %v", err)
	}
	if n < 1 {
		t.Errorf("expected to delete rows, got n=%d", n)
	}
}

func TestWrite_NoConditionReturnsErrUnsafe(t *testing.T) {
	o := setupTestOrm(t)
	defer o.Close()

	_, err := o.Model("bench.model").Write(map[string]any{"name": "y"})
	if !errors.Is(err, ormerr.ErrUnsafe) {
		t.Fatalf("expected ErrUnsafe, got: %v", err)
	}
}

// TestWrite_DataWithIdIsAllowed verifies that Write(map_with_id) is not blocked
// by the ErrUnsafe guard. The ID in the data acts as an implicit condition.
func TestWrite_DataWithIdIsAllowed(t *testing.T) {
	o := setupTestOrm(t)
	defer o.Close()

	id, err := o.Model("bench.model").Create(map[string]any{"name": "before", "age": 0})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = o.Model("bench.model").Write(map[string]any{"id": id, "name": "after"})
	if err != nil {
		t.Fatalf("Write(data_with_id) should succeed, got: %v", err)
	}
}

func TestDelete_WithIdsAllowed(t *testing.T) {
	o := setupTestOrm(t)
	defer o.Close()

	id, err := o.Model("bench.model").Create(map[string]any{"name": "z", "age": 0})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	n, err := o.Model("bench.model").Delete(id)
	if err != nil {
		t.Fatalf("Delete(id) should succeed, got: %v", err)
	}
	if n != 1 {
		t.Errorf("expected n=1, got %d", n)
	}
}
