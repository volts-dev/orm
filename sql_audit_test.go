package orm

import (
	"testing"

	_ "modernc.org/sqlite"
)

// TestDelete_QuotesTableAndFieldNames verifies that the DELETE SQL uses
// dialect-quoted identifiers for the table and primary-key column.
//
// Without quoting, a table named "order" (SQLite reserved word) would produce
// "DELETE FROM order WHERE id in (?)" — a syntax error.
// With quoting it becomes "DELETE FROM `order` WHERE `id` in (?)" — valid SQL.
type OrderAuditModel struct {
	TModel `table:"name('order')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar() size(64)"`
}

func TestDelete_QuotesTableAndFieldNames(t *testing.T) {
	ds := &TDataSource{DbType: "sqlite", DbName: ":memory:"}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer o.Close()

	names, err := o.SyncModel("", new(OrderAuditModel))
	if err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("SyncModel returned no model names")
	}
	modelName := names[0]

	id, err := o.Model(modelName).Create(map[string]any{"name": "alice"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	n, err := o.Model(modelName).Delete(id)
	if err != nil {
		t.Fatalf("Delete with reserved-word table name failed: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 row deleted, got %d", n)
	}
}

// TestDelete_RoundtripAfterReservedWordTable verifies Create+Delete works
// when the table name is also a SQL reserved word ("order").
// This exercises the quoting path end-to-end on an in-memory SQLite DB.
func TestDelete_RoundtripAfterReservedWordTable(t *testing.T) {
	ds := &TDataSource{DbType: "sqlite", DbName: ":memory:"}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer o.Close()

	names, err := o.SyncModel("", new(OrderAuditModel))
	if err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	modelName := names[0]

	// seed 3 rows
	var ids []any
	for _, name := range []string{"a", "b", "c"} {
		id, err := o.Model(modelName).Create(map[string]any{"name": name})
		if err != nil {
			t.Fatalf("Create %q: %v", name, err)
		}
		ids = append(ids, id)
	}

	// delete one by id — must not fail with SQL syntax error
	n, err := o.Model(modelName).Delete(ids[0])
	if err != nil {
		t.Fatalf("Delete(id) on reserved-word table: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
}
