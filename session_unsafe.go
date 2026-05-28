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
	if self.Statement.domain != nil && !self.Statement.domain.IsEmpty() {
		return true
	}
	if len(self.Statement.Params) > 0 {
		return true
	}
	return false
}
