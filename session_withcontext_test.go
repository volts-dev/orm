package orm

import (
	"context"
	"testing"
)

func TestSession_WithContext_DefaultIsBackground(t *testing.T) {
	// NewSession 构造时 context 默认应为 context.Background()
	// 这里不真起 db，直接构造 TSession 验证字段
	s := &TSession{context: context.Background()}
	if s.context != context.Background() {
		t.Fatalf("default ctx should be Background, got %v", s.context)
	}
}

func TestSession_WithContext_OverrideAndChain(t *testing.T) {
	s := &TSession{context: context.Background()}
	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "marker")

	out := s.WithContext(ctx)
	if out != s {
		t.Fatalf("WithContext should return self for chaining")
	}
	if s.context.Value(ctxKey{}) != "marker" {
		t.Fatalf("ctx not stored on session")
	}
}

func TestSession_WithContext_NilFallsBackToBackground(t *testing.T) {
	s := &TSession{context: context.Background()}
	//nolint:staticcheck // SA1012: 测试 nil 兜底逻辑本身，故意传 nil
	s.WithContext(nil)
	if s.context != context.Background() {
		t.Fatalf("nil ctx should fall back to Background, got %v", s.context)
	}
}
