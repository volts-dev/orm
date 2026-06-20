package core

import (
	"context"
	"errors"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// toggleAfterHook 是一个测试用 Hook：当 fail=true 时让 AfterProcess 返回错误，
// 用于模拟“查询本身成功、但 afterProcess 钩子失败”的场景。
type toggleAfterHook struct{ fail bool }

func (h *toggleAfterHook) BeforeProcess(c *ContextHook) (context.Context, error) {
	return c.Ctx, nil
}
func (h *toggleAfterHook) AfterProcess(c *ContextHook) error {
	if h.fail {
		return errors.New("forced afterProcess failure")
	}
	return nil
}

// TestStmt_QueryContext_AfterProcessErrorClosesRows 回归测试 #1：
// 当底层查询成功、但 afterProcess 钩子返回错误时，Stmt.QueryContext 必须关闭
// 已打开的 rows，否则会泄露数据库连接。这里把连接池上限设为 1，若 rows 未被
// 关闭则唯一连接被占用，后续探测查询会因连接池耗尽而超时——以此坐实泄露。
func TestStmt_QueryContext_AfterProcessErrorClosesRows(t *testing.T) {
	db, err := Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE t (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO t (id) VALUES (1)`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	hook := &toggleAfterHook{}
	db.AddHook(hook)

	stmt, err := db.PrepareContext(ctx, `SELECT id FROM t WHERE id = ?`)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	defer stmt.Close()

	// 查询会成功，但 afterProcess 钩子返回错误。
	hook.fail = true
	rows, err := stmt.QueryContext(ctx, 1)
	if rows != nil {
		rows.Close()
	}
	if err == nil {
		t.Fatal("expected afterProcess error from QueryContext, got nil")
	}
	hook.fail = false // 关闭钩子失败，避免影响探测查询

	// 探测：若上面成功打开的 rows 未被关闭，唯一连接将被占用，本次查询会超时。
	probeCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	pr, err := db.QueryContext(probeCtx, `SELECT id FROM t`)
	if err != nil {
		t.Fatalf("probe query failed — rows leaked, connection not returned to pool: %v", err)
	}
	pr.Close()
}
