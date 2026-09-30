package core

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	ormerr "github.com/volts-dev/orm/errors"
	_ "modernc.org/sqlite"
)

// 等外部事件必须带超时：池闸的测试一旦写错，失败方式就是永远挂着。
func within(t *testing.T, d time.Duration, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s: 超过 %s 没有返回（挂住了）", what, d)
	}
}

func openGated(t *testing.T, maxOpen int, acquire time.Duration) *DB {
	t.Helper()
	db, err := Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(maxOpen)
	db.SetAcquireTimeout(acquire)
	return db
}

func inUse(db *DB) int {
	if g := db.gate.Load(); g != nil {
		return len(g.slots)
	}
	return -1
}

// 池满时等连接必须在 AcquireTimeout 内返回一个认得出的错误，而不是一直等。
func TestPoolGate_FullPoolFailsWithinAcquireTimeout(t *testing.T) {
	db := openGated(t, 1, 80*time.Millisecond)
	ctx := context.Background()

	tx, err := db.BeginTx(ctx, nil) // 占住唯一的连接
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	var execErr error
	start := time.Now()
	within(t, 5*time.Second, "池满时的 Exec", func() {
		_, execErr = db.ExecContext(ctx, "SELECT 1")
	})
	if !errors.Is(execErr, ErrPoolTimeout) || !errors.Is(execErr, ormerr.ErrPoolTimeout) {
		t.Fatalf("want ErrPoolTimeout, got %v", execErr)
	}
	var pe *PoolTimeoutError
	if !errors.As(execErr, &pe) || pe.MaxOpen != 1 || pe.Waited < 80*time.Millisecond {
		t.Fatalf("PoolTimeoutError 没带上现场: %+v", pe)
	}
	if waited := time.Since(start); waited < 80*time.Millisecond {
		t.Fatalf("没等够就返回了: %s", waited)
	}

	// Query 与 BeginTx 走的是同一道闸。
	within(t, 5*time.Second, "池满时的 Query", func() {
		_, err = db.QueryContext(ctx, "SELECT 1")
	})
	if !errors.Is(err, ErrPoolTimeout) {
		t.Fatalf("Query: want ErrPoolTimeout, got %v", err)
	}
	within(t, 5*time.Second, "池满时的 BeginTx", func() {
		_, err = db.BeginTx(ctx, nil)
	})
	if !errors.Is(err, ErrPoolTimeout) {
		t.Fatalf("BeginTx: want ErrPoolTimeout, got %v", err)
	}

	st := db.PoolStats()
	if st.GateTimeoutCount != 3 || st.GateWaitCount != 3 || st.GateWaitDuration < 240*time.Millisecond {
		t.Fatalf("计数不对: %+v", st)
	}
	if st.GatePeakInUse != 1 || st.AcquireTimeout != 80*time.Millisecond {
		t.Fatalf("峰值/上限不对: %+v", st)
	}

	// 事务结束，槽位回来，同一条语句立刻能跑。
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if _, err := db.ExecContext(ctx, "SELECT 1"); err != nil {
		t.Fatalf("事务结束后仍拿不到连接: %v", err)
	}
	if n := inUse(db); n != 0 {
		t.Fatalf("槽位没还干净: in use = %d", n)
	}
}

// 排队的请求在别人还回连接时被唤醒，不必等到超时。
func TestPoolGate_WaiterWakesWhenSlotFrees(t *testing.T) {
	db := openGated(t, 1, 5*time.Second)
	ctx := context.Background()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	go func() {
		time.Sleep(60 * time.Millisecond)
		tx.Commit()
	}()

	start := time.Now()
	within(t, 3*time.Second, "排队的 Exec", func() {
		_, err = db.ExecContext(ctx, "SELECT 1")
	})
	if err != nil {
		t.Fatalf("排队后应当成功: %v", err)
	}
	if waited := time.Since(start); waited > 2*time.Second {
		t.Fatalf("像是等到了超时才返回: %s", waited)
	}
	if st := db.PoolStats(); st.GateWaitCount != 1 || st.GateTimeoutCount != 0 {
		t.Fatalf("计数不对: %+v", st)
	}
}

// 调用方自己的 ctx 先到期时，返回的是 ctx 的错误（不冒充成池超时）。
func TestPoolGate_CallerContextWins(t *testing.T) {
	db := openGated(t, 1, 5*time.Second)

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	within(t, 3*time.Second, "ctx 到期的 Exec", func() {
		_, err = db.ExecContext(ctx, "SELECT 1")
	})
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrPoolTimeout) {
		t.Fatalf("want context.DeadlineExceeded, got %v", err)
	}
}

// 没设 AcquireTimeout：行为与标准库一致——等到调用方的 ctx 为止。
func TestPoolGate_NoAcquireTimeoutWaitsForContext(t *testing.T) {
	db := openGated(t, 1, 0)

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	start := time.Now()
	within(t, 3*time.Second, "无池超时的 Exec", func() {
		_, err = db.ExecContext(ctx, "SELECT 1")
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want context.DeadlineExceeded, got %v", err)
	}
	if waited := time.Since(start); waited < 100*time.Millisecond {
		t.Fatalf("没设池超时却提前返回了: %s", waited)
	}
}

// Rows 占着槽位直到关闭；读到头而没调 Close 也要还（标准库此刻已把连接还回池）。
func TestPoolGate_RowsHoldSlotUntilClosedOrDrained(t *testing.T) {
	db := openGated(t, 1, 60*time.Millisecond)
	ctx := context.Background()

	rows, err := db.QueryContext(ctx, "SELECT 1 UNION ALL SELECT 2")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if n := inUse(db); n != 1 {
		t.Fatalf("结果集开着时应占 1 个槽位，got %d", n)
	}
	within(t, 5*time.Second, "结果集开着时的 Exec", func() {
		_, err = db.ExecContext(ctx, "SELECT 1")
	})
	if !errors.Is(err, ErrPoolTimeout) {
		t.Fatalf("结果集开着时第二条语句应当排队超时，got %v", err)
	}

	for rows.Next() {
	}
	if n := inUse(db); n != 0 {
		t.Fatalf("读到头后槽位应已归还，got %d", n)
	}
	// 之后再 Close 是空操作，不能多还一次（多还会把别人的槽位让出去）。
	if err := rows.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if n := inUse(db); n != 0 {
		t.Fatalf("重复归还: in use = %d", n)
	}

	// 没读完就 Close。
	rows, err = db.QueryContext(ctx, "SELECT 1 UNION ALL SELECT 2")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	rows.Next()
	rows.Close()
	if n := inUse(db); n != 0 {
		t.Fatalf("Close 后槽位应已归还，got %d", n)
	}

	// QueryRow 这条路也要还。
	var v int
	if err := db.QueryRowContext(ctx, "SELECT 7").Scan(&v); err != nil || v != 7 {
		t.Fatalf("QueryRow: v=%d err=%v", v, err)
	}
	if n := inUse(db); n != 0 {
		t.Fatalf("QueryRow 后槽位应已归还，got %d", n)
	}
}

// 事务的槽位只还一次：Commit 之后再 Rollback（会话 Close 时的常规动作）不能多还。
func TestPoolGate_TxReleasesExactlyOnce(t *testing.T) {
	db := openGated(t, 2, 60*time.Millisecond)
	ctx := context.Background()

	tx1, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin 1: %v", err)
	}
	tx2, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin 2: %v", err)
	}
	if n := inUse(db); n != 2 {
		t.Fatalf("want 2 in use, got %d", n)
	}

	if err := tx1.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	tx1.Rollback() // sql.ErrTxDone，槽位不该再动
	tx1.Rollback()
	if n := inUse(db); n != 1 {
		t.Fatalf("tx1 结束后应剩 1 个占用（tx2 的），got %d", n)
	}

	tx2.Rollback()
	if n := inUse(db); n != 0 {
		t.Fatalf("want 0 in use, got %d", n)
	}
	if st := db.PoolStats(); st.GatePeakInUse != 2 {
		t.Fatalf("峰值应为 2: %+v", st)
	}
}

// 语句失败也要还槽位。
func TestPoolGate_FailedStatementsReleaseSlot(t *testing.T) {
	db := openGated(t, 1, 60*time.Millisecond)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, "THIS IS NOT SQL"); err == nil {
		t.Fatal("坏 SQL 应当报错")
	}
	if _, err := db.QueryContext(ctx, "SELECT * FROM no_such_table"); err == nil {
		t.Fatal("查不存在的表应当报错")
	}
	if n := inUse(db); n != 0 {
		t.Fatalf("失败的语句没还槽位: in use = %d", n)
	}
}

type failAfterBegin struct{}

func (failAfterBegin) BeforeProcess(c *ContextHook) (context.Context, error) { return c.Ctx, nil }
func (failAfterBegin) AfterProcess(c *ContextHook) error {
	if c.SQL == "BEGIN TRANSACTION" {
		return errors.New("forced afterProcess failure")
	}
	return nil
}

// 事务开成了、钩子却报错：调用方拿不到 Tx，连接与槽位都不能留在外面。
func TestPoolGate_BeginHookFailureDoesNotLeak(t *testing.T) {
	db := openGated(t, 1, 60*time.Millisecond)
	db.AddHook(failAfterBegin{})
	ctx := context.Background()

	if _, err := db.BeginTx(ctx, nil); err == nil {
		t.Fatal("钩子报错时 BeginTx 应当失败")
	}
	if n := inUse(db); n != 0 {
		t.Fatalf("槽位泄漏: in use = %d", n)
	}
	var err error
	within(t, 3*time.Second, "钩子失败后的 Exec", func() {
		_, err = db.ExecContext(ctx, "SELECT 1")
	})
	if err != nil {
		t.Fatalf("连接泄漏（唯一的连接被没人持有的事务占着）: %v", err)
	}
}

// 没设上限就没有闸：什么都不排队，PoolStats 的闸计数恒为零。
func TestPoolGate_DisabledWithoutMaxOpenConns(t *testing.T) {
	db, err := Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	db.SetAcquireTimeout(time.Millisecond)

	if db.gate.Load() != nil {
		t.Fatal("没设 MaxOpenConns 不该有闸")
	}
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()
	if _, err := db.ExecContext(ctx, "SELECT 1"); err != nil {
		t.Fatalf("无上限时不该排队: %v", err)
	}

	// 设了再撤：闸跟着撤。
	db.SetMaxOpenConns(3)
	if db.gate.Load() == nil || db.PoolStats().MaxOpenConnections != 3 {
		t.Fatal("SetMaxOpenConns(3) 应当同时设上标准库的上限和闸")
	}
	db.SetMaxOpenConns(0)
	if db.gate.Load() != nil {
		t.Fatal("SetMaxOpenConns(0) 应当撤掉闸")
	}
}

// 并发压过上限：同时在跑的语句数永远不超过上限，全部完成后槽位归零。
func TestPoolGate_ConcurrencyNeverExceedsLimit(t *testing.T) {
	const limit = 3
	db := openGated(t, limit, 5*time.Second)
	ctx := context.Background()

	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				errs <- err
				return
			}
			if n := inUse(db); n > limit {
				errs <- errors.New("同时占用超过了上限")
			}
			time.Sleep(2 * time.Millisecond)
			errs <- tx.Commit()
		}()
	}
	within(t, 20*time.Second, "并发事务", wg.Wait)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("并发事务出错: %v", err)
		}
	}
	st := db.PoolStats()
	if st.GatePeakInUse != limit {
		t.Fatalf("峰值应当正好顶到上限 %d: %+v", limit, st)
	}
	if n := inUse(db); n != 0 {
		t.Fatalf("全部结束后槽位应归零，got %d", n)
	}
}
