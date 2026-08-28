package orm

import "testing"

// 守 TSelectionField.OnRead 的第二件事：**非存储 selection 的值 getter 必须被调用**。
//
// 这个覆盖版从前只处理「刷新选项表」（getterMethod 那一支），完全没有落到
// TField.OnRead 的闭包 getter 上。后果是 `SelectionField(...).Store(false).Getter(fn)`
// 建出来的字段 fn 一次都不触发，那一列在响应里连键都没有 —— 不报错、不告警，
// 看起来就像后端算不出来（vectors 的 res.user.role 上撞到过：管理员用户表单的
// 「角色」那一格永远空白）。
//
// 两条断言缺一不可：值算了，**并且**选项表没被顺手清掉。
func TestSelectionField_OnRead_CallsClosureGetter(t *testing.T) {
	field := newSelectionField()
	base := field.Base()
	base.name = "role"
	base.store = false

	sel := [][]string{{"group_user", "User"}, {"group_system", "Administrator"}}
	base.selection = sel

	called := 0
	base.getterFunc = func(ctx *TFieldContext) error {
		called++
		ctx.SetValue("group_system")
		return nil
	}
	base.hasGetter = true

	ctx := &TFieldContext{Field: field}
	if err := field.OnRead(ctx); err != nil {
		t.Fatalf("OnRead 返回错误: %v", err)
	}
	if called != 1 {
		t.Fatalf("值 getter 应当被调用 1 次，实际 %d 次 —— 非存储 selection 的值算不出来", called)
	}
	if got := ctx.values; got != "group_system" {
		t.Fatalf("getter 写进去的值没留下: %#v", got)
	}
	// 选项表不能被值 getter 顺手改掉：改了的话下拉框/radio 会整个空掉。
	if len(field.Base().selection) != 2 {
		t.Fatalf("选项表被破坏了: %#v", field.Base().selection)
	}
}

// 没有 getter 的普通 selection 字段（绝大多数）行为不变：OnRead 什么都不做。
func TestSelectionField_OnRead_NoGetterIsNoop(t *testing.T) {
	field := newSelectionField()
	base := field.Base()
	base.name = "state"
	base.selection = [][]string{{"draft", "Draft"}}

	ctx := &TFieldContext{Field: field}
	if err := field.OnRead(ctx); err != nil {
		t.Fatalf("OnRead 返回错误: %v", err)
	}
	if ctx.values != nil {
		t.Fatalf("没有 getter 的字段不该写值: %#v", ctx.values)
	}
}
