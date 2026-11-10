package core

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"time"
)

// ScopedConn 是为一条语句临时开的事务，开事务和设事务级参数合成**一次往返**。
//
// 自动提交的语句要带事务级参数（search_path、行级安全的 app.tenant_id）时，只能包一个
// 隐式事务：SET LOCAL 在事务外无效。走 BeginTx 是四次往返——BEGIN、SET、语句、COMMIT。
// 这里钉住一条连接，用简单查询协议一次发出 "BEGIN; SET LOCAL …"（lib/pq 对无参语句
// 走简单查询，一条消息里可以有多句），于是只剩三次。行级安全下每条自动提交的读都要走
// 这条路，省下的那一次往返是实打实的。
//
// database/sql 不知道这条连接上开着事务，所以结束时必须由这里 COMMIT / ROLLBACK；
// ROLLBACK 都失败了（连接已坏）就把连接标成坏的扔掉，绝不把一个还开着事务的连接还回池里。
type ScopedConn struct {
	db   *DB
	conn *sql.Conn
	slot poolSlot
	done bool
}

// BeginScoped 占一个池槽位、钉住一条连接，发 "BEGIN; <setup>"。setup 必须不带参数。
func (db *DB) BeginScoped(ctx context.Context, setup string) (*ScopedConn, error) {
	hookCtx := NewContextHook(ctx, "BEGIN; "+setup, nil)
	ctx, err := db.beforeProcess(hookCtx)
	if err != nil {
		return nil, err
	}
	var conn *sql.Conn
	gate, err := db.acquire(ctx)
	if err == nil {
		conn, err = db.DB.Conn(ctx)
	}
	if err == nil {
		_, err = conn.ExecContext(ctx, "BEGIN; "+setup)
	}
	hookCtx.End(ctx, nil, err)
	c := &ScopedConn{db: db, conn: conn, slot: poolSlot{gate: gate}}
	if err := db.afterProcess(hookCtx); err != nil {
		if conn != nil {
			c.abort()
		} else if gate != nil {
			gate.put()
		}
		return nil, err
	}
	return c, nil
}

// QueryContext 在这个事务里查询。结果要在 Commit 之前读完。
func (c *ScopedConn) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	hookCtx := NewContextHook(ctx, query, args)
	ctx, err := c.db.beforeProcess(hookCtx)
	if err != nil {
		return nil, err
	}
	rows, err := c.conn.QueryContext(ctx, query, args...)
	hookCtx.End(ctx, nil, err)
	if err := c.db.afterProcess(hookCtx); err != nil {
		if rows != nil {
			rows.Close()
		}
		return nil, err
	}
	return rows, nil
}

// ExecContext 在这个事务里执行。
func (c *ScopedConn) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	hookCtx := NewContextHook(ctx, query, args)
	ctx, err := c.db.beforeProcess(hookCtx)
	if err != nil {
		return nil, err
	}
	res, err := c.conn.ExecContext(ctx, query, args...)
	hookCtx.End(ctx, res, err)
	if err := c.db.afterProcess(hookCtx); err != nil {
		return nil, err
	}
	return res, nil
}

// Commit 提交并归还连接。提交失败时这条连接也不会带着事务回到池里。
func (c *ScopedConn) Commit() error {
	if c.done {
		return nil
	}
	c.done = true
	cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := c.conn.ExecContext(cleanup, "COMMIT")
	if err != nil {
		c.discard()
		return err
	}
	c.conn.Close()
	c.slot.release()
	return nil
}

// Rollback 回滚并归还连接；重复调用是空操作。
func (c *ScopedConn) Rollback() {
	if c.done {
		return
	}
	c.done = true
	c.abort()
}

// abort：ROLLBACK，失败就扔掉连接。
func (c *ScopedConn) abort() {
	// 业务 context 可能已取消；清理不能跳过，也不能无上限占着池槽。
	cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := c.conn.ExecContext(cleanup, "ROLLBACK"); err != nil {
		c.discard()
		return
	}
	c.conn.Close()
	c.slot.release()
}

// discard 把连接标成坏的：database/sql 收到 ErrBadConn 就关掉它、不再复用。
func (c *ScopedConn) discard() {
	c.conn.Raw(func(any) error { return driver.ErrBadConn })
	c.conn.Close()
	c.slot.release()
}
