package orm

// 字段级权限组的 builder 出口。
//
// # 为什么必须有这个 setter
//
// `ModelBuilder.Field(name, type)` **不是**"取出已有字段来改"，而是
// `NewField(...)` 造一个全新的，再 `SetField` 覆盖同名的那个（`osv.go` 里就是
// `fields.Store(name, field)`，纯覆盖）。所以一个字段只要在 `OnBuildFields` 里
// 被重新声明过，它在结构体 tag 上写的一切就都没了 —— 包括 `groups(...)`。
//
// 这条路的后果**不报错、不拒绝，只是那个字段对谁都可见**。真栈里踩到的是
// `hr.version.contract_wage`（员工工资）：结构体上写着
// `groups('hr.group_hr_manager')`，而 `OnBuildFields` 为了挂 Getter 又
// `Builder().Field("contract_wage", "double")` 了一遍，组就此丢掉。
// 表单上一切正常，只是本该看不到工资的人看得到。
//
// 其余属性（Title / Store / Readonly …）之所以没出这个问题，是因为它们本来就得在
// builder 里重写一遍，写漏了肉眼能看见；组丢了则完全没有外在表现。
func (self *fieldStatment) Groups(v string) *fieldStatment {
	self.field.Base().permissionGroups = v
	return self
}
