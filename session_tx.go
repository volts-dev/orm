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

// AfterCommit 让 fn 在"刚才写下的东西对别的连接可见之后"执行。
//
//   - 会话在事务里：登记到底层事务上，提交成功后执行；回滚/提交失败则丢弃。
//     登记在 *core.Tx 上，所以从派生会话（Clone、_getModel）登记的回调，发起
//     Commit 的那个会话一样会执行到。
//   - 会话不在事务里（自动提交）：**立即执行**——每条语句已经各自提交了。
//
// 调用方因此不必知道自己被谁、以哪种方式调用：同一段业务代码既会被前台控制器
// 直接调（自动提交），也会被通用 CRUD 入口包在 `tx.Begin()…tx.Commit()` 里调。
// 典型用途是跨进程通知：对端要按 id 回头读这条记录，事务没提交它就读不到。
func (self *TSession) AfterCommit(fn func()) {
	if fn == nil {
		return
	}
	if self.IsAutoCommit || self.IsCommitedOrRollbacked || self.tx == nil {
		fn()
		return
	}
	self.tx.AfterCommit(fn)
}
