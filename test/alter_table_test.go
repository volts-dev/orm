package test

import (
	"testing"

	"github.com/volts-dev/orm"
)

type (
	AlterTableV1 struct {
		orm.TModel `table:"name('alter_table_model')"`
		Id         int64  `field:"pk autoincr"`
		Code       string `field:"varchar() unique required"`
	}

	AlterTableV2 struct {
		orm.TModel `table:"name('alter_table_model')"`
		Id         int64  `field:"pk autoincr"`
		Code       string `field:"varchar()"`
	}
)

func TestAlterTable_UpdatesConstraints_SQLite(t *testing.T) {
	// This package's test harness usually injects DataSource via TestORMInterfaces.
	// Make the test runnable standalone as well.
	if DataSource == nil {
		DataSource = &orm.TDataSource{DbType: "sqlite", DbName: "test.db"}
	}
	tc := NewTest(t)

	// Start with a clean table for this test.
	_ = tc.Orm.DropTables("alter_table_model")

	// 1) Create table with NOT NULL + UNIQUE constraints.
	if _, err := tc.Orm.SyncModel("test", new(AlterTableV1)); err != nil {
		t.Fatalf("SyncModel(v1) failed: %v", err)
	}

	// NOT NULL should reject NULL inserts at DB level.
	if _, err := tc.Orm.Exec("INSERT INTO alter_table_model(code) VALUES (NULL)"); err == nil {
		t.Fatalf("expected NOT NULL constraint to reject NULL, but insert succeeded")
	}

	// UNIQUE should reject duplicates.
	if _, err := tc.Orm.Exec("INSERT INTO alter_table_model(code) VALUES ('A')"); err != nil {
		t.Fatalf("insert A failed: %v", err)
	}
	if _, err := tc.Orm.Exec("INSERT INTO alter_table_model(code) VALUES ('A')"); err == nil {
		t.Fatalf("expected UNIQUE constraint to reject duplicate 'A', but insert succeeded")
	}

	// 2) Alter model: Code becomes nullable and non-unique.
	if _, err := tc.Orm.SyncModel("test", new(AlterTableV2)); err != nil {
		t.Fatalf("SyncModel(v2) failed: %v", err)
	}

	// Now NULL should be accepted.
	if _, err := tc.Orm.Exec("INSERT INTO alter_table_model(code) VALUES (NULL)"); err != nil {
		t.Fatalf("expected NULL insert to succeed after migration, got: %v", err)
	}

	// And duplicates should be accepted.
	if _, err := tc.Orm.Exec("INSERT INTO alter_table_model(code) VALUES ('A')"); err != nil {
		t.Fatalf("expected duplicate 'A' to succeed after dropping unique, got: %v", err)
	}
}

