package orm

// Begin a transaction
//
//	Begin()
//		...
//	if err = Commit(); err != nil {
//			Rollback()
//	}
func (self *TSession) Begin() error {
	// 当第一次调用时才修改Tx
	if self.IsAutoCommit {
		tx, err := self.db.BeginTx(self.context, nil)
		if err != nil {
			return err
		}

		self.IsAutoCommit = false
		self.IsCommitedOrRollbacked = false
		self.tx = tx

		// 会话带 schema 时，让**裸 SQL** 也落在这个 schema 上（SET LOCAL search_path，
		// 事务级、提交即还原）。SetSchema 从前只影响 ORM 自己拼的 SQL，Exec/Query 里
		// 的表名一直落 public——详见 session_schema.go。
		if err := self.applySearchPath(); err != nil {
			tx.Rollback()
			self.IsAutoCommit = true
			self.tx = nil
			return err
		}
	}

	return nil
}

func (self *TSession) Commit() error {
	if !self.IsAutoCommit && !self.IsCommitedOrRollbacked {
		self.IsCommitedOrRollbacked = true

		if self.tx != nil {
			if err := self.tx.Commit(); err != nil {
				return err
			}
		}
	}

	/* 关闭事务 */
	self.IsAutoCommit = true
	self.tx = nil
	return nil
}

// Rollback when using transaction, you can rollback if any error
// e: the error witch trigger this Rollback
func (self *TSession) Rollback(e error) error {
	if !self.IsAutoCommit && !self.IsCommitedOrRollbacked {
		self.IsCommitedOrRollbacked = true
		if self.tx != nil {
			err := self.tx.Rollback()
			if err != nil {
				return newSessionError("", e, err)
			}
		}
	}

	self.IsAutoCommit = true
	self.tx = nil
	return newSessionError("", e)
}

// IsInTx if current session is in a transaction
func (self *TSession) IsTx() bool {
	return !self.IsAutoCommit
}
