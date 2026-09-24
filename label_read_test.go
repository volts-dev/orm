package orm

import (
	"context"
	"testing"
)

func TestLabelReadContext(t *testing.T) {
	if IsLabelReadContext(nil) || IsLabelReadContext(context.Background()) {
		t.Fatal("未打标记的 context 不该被认成标签读取")
	}
	ctx := WithLabelRead(context.WithValue(context.Background(), "AuthSession", "keep"))
	if !IsLabelReadContext(ctx) {
		t.Fatal("WithLabelRead 之后应认得出")
	}
	if ctx.Value("AuthSession") != "keep" {
		t.Fatal("打标记不能丢掉原 context 里的值（宿主靠它取调用方会话）")
	}
	s := (&TSession{}).WithContext(ctx)
	if !s.IsLabelRead() {
		t.Fatal("会话应从 context 读出标记")
	}
	s.Statement.Fields = []string{"id", "name"}
	got := s.SelectedFields()
	got[0] = "x"
	if s.Statement.Fields[0] != "id" {
		t.Fatal("SelectedFields 必须返回副本")
	}
	if WithLabelRead(nil) == nil || !IsLabelReadContext(WithLabelRead(nil)) {
		t.Fatal("nil context 应规整为 Background 再打标记")
	}
}
