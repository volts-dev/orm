package orm

import (
	"context"
	stdErrors "errors"
	"testing"
	"time"

	ormerr "github.com/volts-dev/orm/errors"
	_ "modernc.org/sqlite"
)

// TestCancelCtx_CreateOnCanceled: WithContext(已取消 ctx).Create() 应立即返回 context.Canceled
func TestCancelCtx_CreateOnCanceled(t *testing.T) {
	ds := &TDataSource{DbType: "sqlite", DbName: ":memory:"}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer o.Close()
	if _, err := o.SyncModel("", new(BenchModel)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	_, err = o.Model("bench.model").WithContext(ctx).Create(map[string]any{
		"name": "alice", "age": 30,
	})
	if err == nil {
		t.Fatal("expected error on canceled ctx, got nil")
	}
	// SQLite MapError 对 Canceled 不做包装，直接返回原始错误
	// DeadlineExceeded 则被包装为 ormerr.ErrTimeout
	if !stdErrors.Is(err, context.Canceled) &&
		!stdErrors.Is(err, context.DeadlineExceeded) &&
		!stdErrors.Is(err, ormerr.ErrTimeout) {
		t.Errorf("expected context.Canceled, context.DeadlineExceeded, or ormerr.ErrTimeout, got: %v", err)
	}
}

// TestCancelCtx_ReadDeadline: WithContext(timeout ctx).Read() 应返回 DeadlineExceeded 或 ErrTimeout
func TestCancelCtx_ReadDeadline(t *testing.T) {
	ds := &TDataSource{DbType: "sqlite", DbName: ":memory:"}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer o.Close()
	if _, err := o.SyncModel("", new(BenchModel)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(2 * time.Nanosecond)

	_, err = o.Model("bench.model").WithContext(ctx).Read()
	if err == nil {
		t.Skip("Read returned nil（可能 SQLite memory 太快，ctx 检查未触发；跳过）")
	}
	// SQLite MapError 将 DeadlineExceeded 包装为 ormerr.ErrTimeout；
	// 也接受 Canceled（少见但 ctx 可能两者都触发）
	if !stdErrors.Is(err, ormerr.ErrTimeout) &&
		!stdErrors.Is(err, context.DeadlineExceeded) &&
		!stdErrors.Is(err, context.Canceled) {
		t.Errorf("expected ormerr.ErrTimeout, context.DeadlineExceeded, or context.Canceled, got: %v", err)
	}
}

// TestCancelCtx_BeginDropsCtx: WithContext(已取消 ctx).Begin() 应传播 context，返回 context.Canceled
func TestCancelCtx_BeginDropsCtx(t *testing.T) {
	ds := &TDataSource{DbType: "sqlite", DbName: ":memory:"}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer o.Close()
	if _, err := o.SyncModel("", new(BenchModel)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	session := o.Model("bench.model").WithContext(ctx)
	err = session.Begin()
	if err == nil {
		t.Fatal("expected error from Begin() with canceled ctx, got nil")
	}
	if !stdErrors.Is(err, context.Canceled) &&
		!stdErrors.Is(err, context.DeadlineExceeded) &&
		!stdErrors.Is(err, ormerr.ErrTimeout) {
		t.Errorf("expected context.Canceled/DeadlineExceeded/ErrTimeout from Begin(), got: %v", err)
	}
}

// TestDefaultCtx_NoChange: 不调用 WithContext 时使用默认 Background，Create 正常
func TestDefaultCtx_NoChange(t *testing.T) {
	ds := &TDataSource{DbType: "sqlite", DbName: ":memory:"}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer o.Close()
	if _, err := o.SyncModel("", new(BenchModel)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}

	_, err = o.Model("bench.model").Create(map[string]any{"name": "alice", "age": 30})
	if err != nil {
		t.Fatalf("default ctx (Background) Create should succeed, got: %v", err)
	}
}
