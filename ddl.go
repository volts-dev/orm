package orm

import (
	"context"
	"fmt"

	"github.com/volts-dev/orm/errors"
)

// TDDLSession is the DDL-only namespace returned by session.DDL().
// Its method set is intentionally disjoint from TSession to prevent
// accidental DDL/DML mixing at compile time.
//
// Dangerous ops (DropTable, Truncate) require AllowUnsafe() first.
type TDDLSession struct {
	session *TSession
}

// DDL returns the DDL namespace for this session.
//
//	session.DDL().CreateTable("user")
//	session.DDL().AllowUnsafe().DropTable("user")
func (self *TSession) DDL() *TDDLSession {
	return &TDDLSession{session: self}
}

// AllowUnsafe sets the underlying session's allowUnsafe flag, permitting
// DropTable and Truncate to execute. Returns self for chaining.
func (d *TDDLSession) AllowUnsafe() *TDDLSession {
	d.session.allowUnsafe = true
	return d
}

// WithContext binds ctx to the underlying session. Returns self for chaining.
func (d *TDDLSession) WithContext(ctx context.Context) *TDDLSession {
	d.session.WithContext(ctx)
	return d
}

// CreateTable creates the table for the named model. Not guarded (non-destructive).
func (d *TDDLSession) CreateTable(model string) error {
	return d.session.createTableImpl(model)
}

// CreateIndexes creates indexes for the named model. Not guarded.
func (d *TDDLSession) CreateIndexes(model string) error {
	return d.session.createIndexesImpl(model)
}

// DropTable drops the named table. Requires AllowUnsafe() — returns ErrUnsafe otherwise.
func (d *TDDLSession) DropTable(name string) error {
	if !d.session.allowUnsafe {
		return errors.ErrUnsafe
	}
	return d.session.dropTableImpl(name)
}

// Truncate deletes all rows from the named table while keeping its structure.
// Requires AllowUnsafe() — returns ErrUnsafe otherwise.
//
// Note: SQLite does not support TRUNCATE TABLE; use DELETE FROM instead on SQLite.
func (d *TDDLSession) Truncate(table string) error {
	if !d.session.allowUnsafe {
		return errors.ErrUnsafe
	}
	return d.session.truncateImpl(table)
}

// truncateImpl executes TRUNCATE TABLE on the given table name.
func (self *TSession) truncateImpl(table string) error {
	quoter := self.orm.dialect.Quoter()
	quoted, err := quoter.QuoteIdent(table)
	if err != nil {
		return err
	}
	_, err = self._exec(fmt.Sprintf("TRUNCATE TABLE %s", quoted))
	return err
}
