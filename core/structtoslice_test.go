package core

import "testing"

// TestStructToSlice_MissingFieldNoPanic 回归测试 new-correctness-4：
// 当 SQL 含 ?name 命名参数但结构体无对应字段时，StructToSlice 必须返回 error
// 而不是 panic（与 MapToSlice 的缺字段处理一致）。
func TestStructToSlice_MissingFieldNoPanic(t *testing.T) {
	type S struct {
		Id int64
	}
	_, _, err := StructToSlice("SELECT * FROM t WHERE x=?Missing", &S{Id: 1})
	if err == nil {
		t.Fatal("expected error for missing struct field, got nil")
	}
}
