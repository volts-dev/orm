package orm

import (
	"errors"
	"testing"

	ormerr "github.com/volts-dev/orm/errors"
	_ "modernc.org/sqlite"
)

func TestDDL_ReturnsTDDLSession(t *testing.T) {
	s := &TSession{}
	d := s.DDL()
	if d == nil {
		t.Fatal("DDL() returned nil")
	}
	if d.session != s {
		t.Fatal("DDL session should wrap original session")
	}
}

func TestDDL_AllowUnsafeChainsBack(t *testing.T) {
	s := &TSession{}
	out := s.DDL().AllowUnsafe()
	if out == nil {
		t.Fatal("DDL().AllowUnsafe() returned nil")
	}
	if !s.allowUnsafe {
		t.Fatal("AllowUnsafe through DDL should set underlying session's allowUnsafe")
	}
}

func TestDDLSession_DropTableRequiresAllowUnsafe(t *testing.T) {
	o := setupTestOrm(t)
	defer o.Close()

	err := o.NewSession().DDL().DropTable("bench_model")
	if !errors.Is(err, ormerr.ErrUnsafe) {
		t.Fatalf("DropTable without AllowUnsafe should return ErrUnsafe, got: %v", err)
	}
}

func TestDDLSession_TruncateRequiresAllowUnsafe(t *testing.T) {
	o := setupTestOrm(t)
	defer o.Close()

	err := o.NewSession().DDL().Truncate("bench_model")
	if !errors.Is(err, ormerr.ErrUnsafe) {
		t.Fatalf("Truncate without AllowUnsafe should return ErrUnsafe, got: %v", err)
	}
}

func TestDDLSession_CreateTableNotGuarded(t *testing.T) {
	o := setupTestOrm(t)
	defer o.Close()

	// CreateTable is not a dangerous op; should not return ErrUnsafe
	err := o.NewSession().DDL().CreateTable("bench.model")
	if errors.Is(err, ormerr.ErrUnsafe) {
		t.Fatal("CreateTable should not be guarded by AllowUnsafe")
	}
}

func TestDDLSession_DropTable_AllowUnsafePermits(t *testing.T) {
	o := setupTestOrm(t)
	defer o.Close()

	err := o.NewSession().DDL().AllowUnsafe().DropTable("bench_model")
	// Should not return ErrUnsafe (may return other errors like table not found, that's OK)
	if errors.Is(err, ormerr.ErrUnsafe) {
		t.Fatalf("DropTable with AllowUnsafe should not return ErrUnsafe, got: %v", err)
	}
}
