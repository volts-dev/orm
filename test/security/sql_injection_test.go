package security

import (
	"fmt"
	"testing"

	"github.com/volts-dev/orm"
	"github.com/volts-dev/utils"
)

// runInjectionSuite is the dialect-agnostic test body.
// It verifies that malicious inputs are treated as literals (not executed as SQL)
// and that fixture rows survive all injection attempts.
func runInjectionSuite(t *testing.T, o *orm.TOrm) {
	t.Helper()
	seedUsers(t, o)

	for _, input := range MaliciousInputs {
		input := input
		t.Run(fmt.Sprintf("payload/%d", indexOf(MaliciousInputs, input)), func(t *testing.T) {
			// 1. WHERE parameterized: malicious input must NOT match any row
			ds, err := o.Model("sec.test.user").Where("name=?", input).Read()
			if err != nil {
				t.Fatalf("Where(name=?, %q): %v", input, err)
			}
			if ds.Count() != 0 {
				t.Errorf("Where(name=%q) matched %d rows — injection not neutralized", input, ds.Count())
			}

			// 2. Create: malicious input stored as literal value, round-trips intact
			id, err := o.Model("sec.test.user").Create(map[string]any{
				"name":  input,
				"email": "injection@test.invalid",
			})
			if err != nil {
				t.Fatalf("Create(name=%q): %v", input, err)
			}
			row, err := o.Model("sec.test.user").Ids(id).Read()
			if err != nil {
				t.Fatalf("Read back id=%v: %v", id, err)
			}
			if row.Count() != 1 {
				t.Fatalf("expected 1 row after Create, got %d", row.Count())
			}
			got := utils.ToString(row.Record().GetByField("name"))
			if got != input {
				t.Errorf("name roundtrip: got %q, want %q — data was modified by injection", got, input)
			}
		})
	}

	// Fixture integrity: alice and bob must still exist
	ds, err := o.Model("sec.test.user").Where("name=?", "alice").Read()
	if err != nil {
		t.Fatalf("fixture check alice: %v", err)
	}
	if ds.Count() != 1 {
		t.Errorf("fixture row 'alice' lost after injection attempts (count=%d)", ds.Count())
	}

	ds, err = o.Model("sec.test.user").Where("name=?", "bob").Read()
	if err != nil {
		t.Fatalf("fixture check bob: %v", err)
	}
	if ds.Count() != 1 {
		t.Errorf("fixture row 'bob' lost after injection attempts (count=%d)", ds.Count())
	}
}

func indexOf(slice []string, s string) int {
	for i, v := range slice {
		if v == s {
			return i
		}
	}
	return -1
}

func TestSQLInjection_SQLite(t *testing.T) {
	o := setupSQLite(t)
	defer o.Close()
	runInjectionSuite(t, o)
}

func TestSQLInjection_MySQL(t *testing.T) {
	o := setupMySQL(t)
	defer o.Close()
	runInjectionSuite(t, o)
}

func TestSQLInjection_Postgres(t *testing.T) {
	o := setupPostgres(t)
	defer o.Close()
	runInjectionSuite(t, o)
}
