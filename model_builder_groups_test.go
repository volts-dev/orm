package orm

import "testing"

// builder 造出来的字段是**全新**的，组必须能重新声明——否则挂了 Getter 的字段
// 就再也带不上 groups(...)，而这件事没有任何外在表现。
func TestFieldStatment_Groups(t *testing.T) {
	f, err := NewField("contract_wage", WithFieldType("double"))
	if err != nil {
		t.Fatalf("建字段失败: %v", err)
	}
	st := &fieldStatment{field: f}

	if got := f.Groups(); got != "" {
		t.Fatalf("新建字段的组应为空串（不限组），实得 %q", got)
	}
	if st.Groups("hr.group_hr_manager") != st {
		t.Error("Groups 应返回自身以便链式调用")
	}
	if got := f.Groups(); got != "hr.group_hr_manager" {
		t.Errorf("Groups() 后 = %q，期望 hr.group_hr_manager", got)
	}
	// 多组之间是 OR，与 tag 的写法保持一致（tag_groups 会归一成逗号分隔）。
	st.Groups("a.g1,b.g2")
	if got := f.Groups(); got != "a.g1,b.g2" {
		t.Errorf("多组 = %q，期望 a.g1,b.g2", got)
	}
}
