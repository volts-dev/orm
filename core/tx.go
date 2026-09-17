package core

import (
	"context"
	"database/sql"
	"runtime/debug"
	"sync"
)

var (
	_ QueryExecuter = &Tx{}
)

// Tx represents a transaction
type Tx struct {
	*sql.Tx
	db  *DB
	ctx context.Context

	// afterCommit 是"这个事务真的提交成功之后"才该做的事，见 AfterCommit。
	// 挂在 Tx 上而不是 TSession 上：派生会话（_getModel、Clone）各是一个新的
	// TSession，但共享同一个 *Tx 指针——挂在会话上的话，从派生会话登记的回调
	// 发起 Commit 的那个会话根本看不见。
	hookMu      sync.Mutex
	afterCommit []func()
	// state 是事务的终态。派生会话复制的是 *Tx 指针与当时的标志位，事务结束后
	// 它们手里还是这个 Tx——此时再登记的回调要按终态处理（见 AfterCommit），
	// 否则会挂在一个再也不会提交的事务上，永远不执行、也不报错。
	state txState
}

type txState int

const (
	txActive txState = iota
	txCommitted
	txRolledBack
)

// BeginTx begin a transaction with option
func (db *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*Tx, error) {
	hookCtx := NewContextHook(ctx, "BEGIN TRANSACTION", nil)
	ctx, err := db.beforeProcess(hookCtx)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.BeginTx(ctx, opts)
	hookCtx.End(ctx, nil, err)
	if err := db.afterProcess(hookCtx); err != nil {
		return nil, err
	}
	return &Tx{Tx: tx, db: db, ctx: ctx}, nil
}

// Begin begins a transaction
func (db *DB) Begin() (*Tx, error) {
	return db.BeginTx(context.Background(), nil)
}

// Commit submit the transaction
func (tx *Tx) Commit() error {
	hookCtx := NewContextHook(tx.ctx, "COMMIT", nil)
	ctx, err := tx.db.beforeProcess(hookCtx)
	if err != nil {
		return err
	}
	err = tx.Tx.Commit()
	hookCtx.End(ctx, nil, err)
	// afterProcess 先交回提交本身的错误（Hooks.AfterProcess 以 c.Err 打底），
	// 再是钩子的。任何一种都算"没确认提交成功"：宁可不做，也不在一个可能没提交的
	// 事务上做"提交之后"的事。
	if err := tx.db.afterProcess(hookCtx); err != nil {
		tx.finish(txRolledBack)
		return err
	}
	tx.runAfterCommit(tx.finish(txCommitted))
	return nil
}

// AfterCommit 登记一个在本事务**提交成功之后**才执行的回调。
//
// 存在的理由：有些副作用必须等数据对**别的连接**可见之后才能做——典型是跨进程
// 通知（mail 服务回头按 id 读这条记录渲染邮件）、推送、缓存失效。在事务里直接做，
// 对端读到的是一条还不存在的记录；而事务回滚的话，那封信已经发出去了。
//
// 回滚、提交失败时回调**丢弃，不执行**。回调按登记顺序执行，单个回调 panic 只记
// 日志，不影响其余回调，也不改变 Commit 的返回值——事务已经提交了，没有什么能
// 再让它"失败"。
//
// 事务已经结束时再登记：已提交的立即执行，已回滚的丢弃。
func (tx *Tx) AfterCommit(fn func()) {
	if fn == nil {
		return
	}
	tx.hookMu.Lock()
	switch tx.state {
	case txCommitted:
		tx.hookMu.Unlock()
		tx.runAfterCommit([]func(){fn})
		return
	case txRolledBack:
		tx.hookMu.Unlock()
		return
	}
	tx.afterCommit = append(tx.afterCommit, fn)
	tx.hookMu.Unlock()
}

// finish 记下终态并取走已登记的回调。
func (tx *Tx) finish(state txState) []func() {
	tx.hookMu.Lock()
	tx.state = state
	fns := tx.afterCommit
	tx.afterCommit = nil
	tx.hookMu.Unlock()
	return fns
}

func (tx *Tx) runAfterCommit(fns []func()) {
	for _, fn := range fns {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Errf("after-commit hook panicked: %v\n%s", r, debug.Stack())
				}
			}()
			fn()
		}()
	}
}

// Rollback rollback the transaction
func (tx *Tx) Rollback() error {
	tx.finish(txRolledBack) // 回滚了就不该发生"提交之后"的事
	hookCtx := NewContextHook(tx.ctx, "ROLLBACK", nil)
	ctx, err := tx.db.beforeProcess(hookCtx)
	if err != nil {
		return err
	}
	err = tx.Tx.Rollback()
	hookCtx.End(ctx, nil, err)
	return tx.db.afterProcess(hookCtx)
}

// PrepareContext prepare the query
func (tx *Tx) PrepareContext(ctx context.Context, query string) (*Stmt, error) {
	names := make(map[string]int)
	var i int
	query = re.ReplaceAllStringFunc(query, func(src string) string {
		names[src[1:]] = i
		i++
		return "?"
	})
	hookCtx := NewContextHook(ctx, "PREPARE", nil)
	ctx, err := tx.db.beforeProcess(hookCtx)
	if err != nil {
		return nil, err
	}
	stmt, err := tx.Tx.PrepareContext(ctx, query)
	hookCtx.End(ctx, nil, err)
	if err := tx.db.afterProcess(hookCtx); err != nil {
		return nil, err
	}
	return &Stmt{stmt, tx.db, names, query}, nil
}

// Prepare prepare the query
func (tx *Tx) Prepare(query string) (*Stmt, error) {
	return tx.PrepareContext(tx.ctx, query)
}

// StmtContext creates Stmt with context
func (tx *Tx) StmtContext(ctx context.Context, stmt *Stmt) *Stmt {
	stmt.Stmt = tx.Tx.StmtContext(ctx, stmt.Stmt)
	return stmt
}

// Stmt creates Stmt
func (tx *Tx) Stmt(stmt *Stmt) *Stmt {
	return tx.StmtContext(tx.ctx, stmt)
}

// ExecMapContext executes query with args in a map
func (tx *Tx) ExecMapContext(ctx context.Context, query string, mp any) (sql.Result, error) {
	query, args, err := MapToSlice(query, mp)
	if err != nil {
		return nil, err
	}
	return tx.ExecContext(ctx, query, args...)
}

// ExecMap executes query with args in a map
func (tx *Tx) ExecMap(query string, mp any) (sql.Result, error) {
	return tx.ExecMapContext(tx.ctx, query, mp)
}

// ExecStructContext executes query with args in a struct
func (tx *Tx) ExecStructContext(ctx context.Context, query string, st any) (sql.Result, error) {
	query, args, err := StructToSlice(query, st)
	if err != nil {
		return nil, err
	}
	return tx.ExecContext(ctx, query, args...)
}

// ExecContext executes a query with args
func (tx *Tx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	hookCtx := NewContextHook(ctx, query, args)
	ctx, err := tx.db.beforeProcess(hookCtx)
	if err != nil {
		return nil, err
	}
	res, err := tx.Tx.ExecContext(ctx, query, args...)
	hookCtx.End(ctx, res, err)
	if err := tx.db.afterProcess(hookCtx); err != nil {
		return nil, err
	}
	return res, err
}

// ExecStruct executes query with args in a struct
func (tx *Tx) ExecStruct(query string, st any) (sql.Result, error) {
	return tx.ExecStructContext(tx.ctx, query, st)
}

// QueryContext query with args
func (tx *Tx) QueryContext(ctx context.Context, query string, args ...any) (*Rows, error) {
	hookCtx := NewContextHook(ctx, query, args)
	ctx, err := tx.db.beforeProcess(hookCtx)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Tx.QueryContext(ctx, query, args...)
	hookCtx.End(ctx, nil, err)
	if err := tx.db.afterProcess(hookCtx); err != nil {
		if rows != nil {
			rows.Close()
		}
		return nil, err
	}
	return &Rows{rows, tx.db}, nil
}

// Query query with args
func (tx *Tx) Query(query string, args ...any) (*Rows, error) {
	return tx.QueryContext(tx.ctx, query, args...)
}

// QueryMapContext query with args in a map
func (tx *Tx) QueryMapContext(ctx context.Context, query string, mp any) (*Rows, error) {
	query, args, err := MapToSlice(query, mp)
	if err != nil {
		return nil, err
	}
	return tx.QueryContext(ctx, query, args...)
}

// QueryMap query with args in a map
func (tx *Tx) QueryMap(query string, mp any) (*Rows, error) {
	return tx.QueryMapContext(tx.ctx, query, mp)
}

// QueryStructContext query with args in struct
func (tx *Tx) QueryStructContext(ctx context.Context, query string, st any) (*Rows, error) {
	query, args, err := StructToSlice(query, st)
	if err != nil {
		return nil, err
	}
	return tx.QueryContext(ctx, query, args...)
}

// QueryStruct query with args in struct
func (tx *Tx) QueryStruct(query string, st any) (*Rows, error) {
	return tx.QueryStructContext(tx.ctx, query, st)
}

// QueryRowContext query one row with args
func (tx *Tx) QueryRowContext(ctx context.Context, query string, args ...any) *Row {
	rows, err := tx.QueryContext(ctx, query, args...)
	return &Row{rows, err}
}

// QueryRow query one row with args
func (tx *Tx) QueryRow(query string, args ...any) *Row {
	return tx.QueryRowContext(tx.ctx, query, args...)
}

// QueryRowMapContext query one row with args in a map
func (tx *Tx) QueryRowMapContext(ctx context.Context, query string, mp any) *Row {
	query, args, err := MapToSlice(query, mp)
	if err != nil {
		return &Row{nil, err}
	}
	return tx.QueryRowContext(ctx, query, args...)
}

// QueryRowMap query one row with args in a map
func (tx *Tx) QueryRowMap(query string, mp any) *Row {
	return tx.QueryRowMapContext(tx.ctx, query, mp)
}

// QueryRowStructContext query one row with args in struct
func (tx *Tx) QueryRowStructContext(ctx context.Context, query string, st any) *Row {
	query, args, err := StructToSlice(query, st)
	if err != nil {
		return &Row{nil, err}
	}
	return tx.QueryRowContext(ctx, query, args...)
}

// QueryRowStruct query one row with args in struct
func (tx *Tx) QueryRowStruct(query string, st any) *Row {
	return tx.QueryRowStructContext(tx.ctx, query, st)
}
