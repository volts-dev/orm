package orm

// 字段级脱敏的「逃生舱」开关，配合 model.BeforeSession 钩子使用（如 vectors 的
// 多租户 withSession 会在认证用户的 Read 上 Omit("tenant_id")，避免机密字段外泄前端）。
//
// 默认 false = 脱敏（fail closed）：认证用户的通用读路径不得把 tenant_id 等范围字段
// 返回前端。无登录 session 的 system 读（备份、迁移等）本就不触发脱敏，无需调用此方法。
// 仅当「已登录、但确实需要在结果中拿到范围字段」时（如跨租户管理后台、数据导出）才显式开启。

// IncludeScopedFields 关闭本 Session 的字段级脱敏，使 BeforeSession 钩子不再 Omit
// tenant_id 等范围字段。sticky：一经设置在该 Session 生命周期内持续有效。返回 self 以链式调用。
func (self *TSession) IncludeScopedFields() *TSession {
	self.exposeScopedFields = true
	return self
}

// ScopedFieldsExposed 报告是否已通过 IncludeScopedFields() 关闭字段级脱敏。
// 供 model.BeforeSession 钩子判断是否跳过 Omit。
func (self *TSession) ScopedFieldsExposed() bool {
	return self.exposeScopedFields
}
