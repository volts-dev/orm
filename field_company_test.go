package orm

import (
	"context"
	"testing"
)

// 显式的当前公司优先；没有时才问惰性来源，且只在真正取公司时调用它。
func TestCompanyIdFrom_ExplicitThenLookup(t *testing.T) {
	calls := 0
	lookup := func() int64 { calls++; return 7 }

	ctx := WithCompanyLookup(context.Background(), lookup)
	if calls != 0 {
		t.Fatal("放进 context 时就调了来源：应当用到时才调")
	}
	if got := CompanyIdFrom(ctx); got != 7 || calls != 1 {
		t.Fatalf("没有显式公司时应问来源: got %d, calls %d", got, calls)
	}
	if got := CompanyIdFrom(WithCompanyId(ctx, 3)); got != 3 || calls != 1 {
		t.Fatalf("有显式公司时不该再问来源: got %d, calls %d", got, calls)
	}
	if got := CompanyIdFrom(WithCompanyLookup(context.Background(), func() int64 { return 0 })); got != 0 {
		t.Fatalf("来源也给不出时应为 0，得 %d", got)
	}
	if got := CompanyIdFrom(context.Background()); got != 0 {
		t.Fatalf("什么都没有应为 0，得 %d", got)
	}
}
