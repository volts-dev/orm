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
		tx, err := self.db.Begin()
		if err != nil {
			return err
		}

		self.IsAutoCommit = false
		self.IsCommitedOrRollbacked = false
		self.tx = tx
	}

	return nil
}

func (self *TSession) Commit() error {
	if !self.IsAutoCommit && !self.IsCommitedOrRollbacked {
		self.IsCommitedOrRollbacked = true

		if err := self.tx.Commit(); err != nil {
			return err
		}
	}

	/* 关闭事务 */
	self.Statement.model.GetBase().transaction = nil
	// TODO 是否重置Session
	return nil
}

// Rollback when using transaction, you can rollback if any error
// e: the error witch trigger this Rollback
func (self *TSession) Rollback(e error) error {
	if !self.IsAutoCommit && !self.IsCommitedOrRollbacked {
		//session.saveLastSQL(session.Engine.dialect.RollBackStr())
		self.IsCommitedOrRollbacked = true
		err := self.tx.Rollback()
		if err != nil {
			return newSessionError("", e, err)
		}
	}
	return newSessionError("", e)
}

// IsInTx if current session is in a transaction
func (self *TSession) IsTx() bool {
	return !self.IsAutoCommit
}
