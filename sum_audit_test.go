package orm

import "testing"

// TestSum_ComputesRealAggregate 回归测试 new-correctness-7：
// 修复前 generate_sum 返回空 SQL，Sum 永远查空、返回 0；修复后应返回真实求和值。
func TestSum_ComputesRealAggregate(t *testing.T) {
	o := setupIntegrationOrm(t)
	for _, age := range []int{1, 2, 3, 4} {
		if _, err := o.Model("bench.model").Create(map[string]any{"name": "x", "age": age}); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	sum, err := o.Model("bench.model").Sum("age")
	if err != nil {
		t.Fatalf("Sum: %v", err)
	}
	if sum != 10 {
		t.Fatalf("expected SUM(age)=10, got %v", sum)
	}
}

// TestSum_UnknownFieldRejected 确认 Sum 对非模型字段拒绝（防注入），而非拼进 SQL。
func TestSum_UnknownFieldRejected(t *testing.T) {
	o := setupIntegrationOrm(t)
	if _, err := o.Model("bench.model").Sum("nosuchcol); DROP TABLE bench_model;--"); err == nil {
		t.Fatal("Sum with non-field column must be rejected")
	}
}
