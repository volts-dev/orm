package core

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"
)

type cleanupConnector struct{ seen chan context.Context }

func (c cleanupConnector) Connect(context.Context) (driver.Conn, error) {
	return &cleanupConnection{seen: c.seen}, nil
}
func (c cleanupConnector) Driver() driver.Driver { return cleanupDriver{} }

type cleanupDriver struct{}

func (cleanupDriver) Open(string) (driver.Conn, error) { return nil, errors.New("not used") }

type cleanupConnection struct{ seen chan context.Context }

func (c *cleanupConnection) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not used") }
func (c *cleanupConnection) Close() error                        { return nil }
func (c *cleanupConnection) Begin() (driver.Tx, error)           { return nil, errors.New("not used") }
func (c *cleanupConnection) ExecContext(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Result, error) {
	c.seen <- ctx
	return driver.RowsAffected(0), nil
}

func TestScopedCleanupHasIndependentBoundedContext(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "commit"}[commit], func(t *testing.T) {
			seen := make(chan context.Context, 1)
			db := sql.OpenDB(cleanupConnector{seen: seen})
			defer db.Close()
			conn, err := db.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			scope := &ScopedConn{conn: conn}
			if commit {
				if err := scope.Commit(); err != nil {
					t.Fatal(err)
				}
			} else {
				scope.Rollback()
			}
			select {
			case ctx := <-seen:
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > 3*time.Second || time.Until(deadline) < 2*time.Second {
					t.Fatal("cleanup has no independent three-second budget")
				}
			case <-time.After(time.Second):
				t.Fatal("cleanup was skipped")
			}
			if st := db.Stats(); st.InUse != 0 {
				t.Fatal("cleanup retained a connection")
			}
		})
	}
}
