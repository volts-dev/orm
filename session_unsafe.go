package orm

// AllowUnsafe disables this session's Phase 2 dangerous-operation guards,
// permitting no-WHERE Delete/Write and the DDL counterparts DropTable/Truncate.
// Effect is sticky for the session's lifetime.
func (self *TSession) AllowUnsafe() *TSession {
	self.allowUnsafe = true
	return self
}

// hasCondition reports whether the current Statement carries a row-locating
// condition: explicit Ids, a non-empty Where clause, or a non-nil Domain.
// Used by Delete/Write to decide whether to enforce the AllowUnsafe guard.
func (self *TSession) hasCondition() bool {
	if len(self.Statement.IdParam) > 0 {
		return true
	}
	// ★ 判据必须与 where_calc 真正生成 WHERE 的门槛**一致**：那里的门槛是
	//   `node.Count() > 0`。不能用 IsEmpty()——IsEmpty() 说的是"没有值也没有孩子"，
	//   一个 Count()==0 但 Value 非空的值节点在它眼里"非空"，可 where_calc 压根
	//   不会为它生成任何条件，于是守卫放行、DELETE 落到"按域全选"的回退上，
	//   把整张表删掉。这里宁可严格：算不出 WHERE 的域一律不算条件。
	if self.Statement.domain.Count() > 0 {
		return true
	}
	if len(self.Statement.Params) > 0 {
		return true
	}
	return false
}
