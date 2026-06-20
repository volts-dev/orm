package orm

import "testing"

// TestWhere_NonFieldColumnRejected 锁定安全属性 new-security-1：
// 非模型字段的列名（SQL 注入 payload 同属此类）必须被拒绝，绝不作为列名拼进 SQL。
// 当前由解析期 expr.go 的字段校验直接返回错误中止查询（leaf_to_sql 闸门为防御纵深）。
// 此测试守住"未知列必被拒绝"这一保证：若未来移除解析期校验，此测试会失败。
func TestWhere_NonFieldColumnRejected(t *testing.T) {
	o := setupIntegrationOrm(t)
	if _, err := o.Model("bench.model").Create(map[string]any{"name": "a", "age": 1}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := o.Model("bench.model").Where("nosuchcol=?", 1).Read(); err == nil {
		t.Fatal("non-field column must be rejected, but Read succeeded (possible injection surface)")
	}

	// 表应仍存在、可读（没有任何注入被执行）
	if _, err := o.Model("bench.model").Read(); err != nil {
		t.Fatalf("table should still exist: %v", err)
	}
}

// TestOrderBy_InjectionDirectionIgnored 回归测试 sec-orderby：
// 用户可控的排序方向若非 ASC/DESC，必须被丢弃而不是原样拼进 SQL。
// 修复前 "name <payload>" 的 <payload> 会被拼成 `col <payload>` 触发 SQL 语法错误/注入；
// 修复后非法方向被忽略，查询正常返回。
func TestOrderBy_InjectionDirectionIgnored(t *testing.T) {
	o := setupIntegrationOrm(t)
	if _, err := o.Model("bench.model").Create(map[string]any{"name": "a", "age": 1}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// 空格分隔出方向 token，注入非法方向
	_, err := o.Model("bench.model").OrderBy("name EVIL)--").Read()
	if err != nil {
		t.Fatalf("malicious OrderBy direction should be ignored, got: %v", err)
	}
}

// TestGroupBy_InjectionFieldIgnored 回归测试 sec-groupby：
// 非模型字段的 GroupBy 项必须被丢弃而不是原样拼进 SQL。
func TestGroupBy_InjectionFieldIgnored(t *testing.T) {
	o := setupIntegrationOrm(t)
	if _, err := o.Model("bench.model").Create(map[string]any{"name": "a", "age": 1}); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := o.Model("bench.model").GroupBy("name", "x); DROP TABLE bench_model;--").Read()
	if err != nil {
		t.Fatalf("malicious GroupBy field should be ignored, got: %v", err)
	}

	// 表应仍然存在、可读
	if _, err := o.Model("bench.model").Read(); err != nil {
		t.Fatalf("table should still exist after malicious GroupBy: %v", err)
	}
}
