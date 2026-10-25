package orm

import (
	"database/sql"
	"fmt"
)

// Begin a transaction
//
//	Begin()
//		...
//	if err = Commit(); err != nil {
//			Rollback()
//	}
func (self *TSession) Begin() error {
	return self.BeginTx(nil)
}

// BeginTx 是带选项的 Begin（隔离级别、只读）。会话已在事务里时什么都不做——选项只在开事务那一刻有用。
//
// 要快照读就用它，别在事务里发 SET TRANSACTION ISOLATION LEVEL：PG 要求那是事务的第一条语句（Begin 之后
// orm 可能已经发过 SET LOCAL），MySQL 在事务里直接拒绝（1568）——驱动在 START TRANSACTION 之前设好才对。
func (self *TSession) BeginTx(opts *sql.TxOptions) error {
	self.syncBorrowedTx()
	// 当第一次调用时才修改Tx
	if self.IsAutoCommit {
		pool, err := self.pool()
		if err != nil {
			return err
		}
		tx, err := pool.BeginTx(self.context, opts)
		if err != nil {
			return err
		}

		self.IsAutoCommit = false
		self.IsCommitedOrRollbacked = false
		self.tx = tx
		self.txs = &txState{}
		self.borrowedTx = false

		// 会话带 schema 时，让**裸 SQL** 也落在这个 schema 上（SET LOCAL search_path，
		// 事务级、提交即还原）。SetSchema 从前只影响 ORM 自己拼的 SQL，Exec/Query 里
		// 的表名一直落 public——详见 session_schema.go。行范围已经定了的话一并设上，
		// 同一次往返（见 row_scope.go）；之后才定的由 ensureTxRowScope 在下一条语句前补。
		if err := self.applyTxSettings(); err != nil {
			tx.Rollback()
			self.IsAutoCommit = true
			self.tx = nil
			self.txs = nil
			return err
		}
	}

	return nil
}

func (self *TSession) Commit() error {
	// 借来的事务不归这里提交（见 Clone）：借用者的 Commit 是空操作，它的写随拥有者
	// 的提交一起生效。
	if self.borrowedTx {
		self.syncBorrowedTx()
		return nil
	}
	if !self.IsAutoCommit && !self.IsCommitedOrRollbacked {
		self.IsCommitedOrRollbacked = true
		self.markTxDone()

		if self.tx != nil {
			if err := self.tx.Commit(); err != nil {
				return err
			}
		}
	}

	/* 关闭事务 */
	self.IsAutoCommit = true
	self.tx = nil
	self.txs = nil
	return nil
}

// Rollback when using transaction, you can rollback if any error
// e: the error witch trigger this Rollback
//
// 借来的事务不归这里回滚（见 Clone）：只把错误原样带回去。失败的那条语句已经让
// postgres 把整个事务打成 aborted，拥有者提交时会报出来。
func (self *TSession) Rollback(e error) error {
	if self.borrowedTx {
		self.syncBorrowedTx()
		return newSessionError("", e)
	}
	if !self.IsAutoCommit && !self.IsCommitedOrRollbacked {
		self.IsCommitedOrRollbacked = true
		self.markTxDone()
		if self.tx != nil {
			err := self.tx.Rollback()
			if err != nil {
				return newSessionError("", e, err)
			}
		}
	}

	self.IsAutoCommit = true
	self.tx = nil
	self.txs = nil
	return newSessionError("", e)
}

// markTxDone 告诉借用者：事务结束了。
func (self *TSession) markTxDone() {
	if self.txs != nil {
		self.txs.done.Store(true)
	}
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
	self.syncBorrowedTx()
	if self.IsAutoCommit || self.IsCommitedOrRollbacked || self.tx == nil {
		fn()
		return
	}
	self.tx.AfterCommit(fn)
}

// RelaxTimeouts 在当前事务里放开库侧的语句超时与等锁超时，直到事务结束或
// RestoreTimeouts。
//
// 库（或角色）上配的 statement_timeout 是给请求流量兜底的：一条语句跑过了头、一把锁
// 等过了头，收掉它比让它占着连接好。但有两类事**本来就该**久：
//
//   - 等一把别人要持很久的锁（同一租户的模块安装排队，前一个事务一持就是几分钟）；
//   - 部署期的结构同步与数据迁移（大表上建索引、整表回填）。
//
// 它们在自己的事务里调这个方法声明"我知道我慢"。只对事务有效——自动提交的会话
// 上 SET LOCAL 在语句结束就没了，那时直接报错，而不是让调用方以为自己被放开了。
// 不支持的方言（没有这类超时）是空操作。
func (self *TSession) RelaxTimeouts() error {
	return self.setTimeouts(true)
}

// RestoreTimeouts 把 RelaxTimeouts 放开的超时还原成库上配的默认值。
// 用在"只有中间那一步该久"的地方：放开 → 等锁 → 还原，事务里其余的语句照常受约束。
func (self *TSession) RestoreTimeouts() error {
	return self.setTimeouts(false)
}

func (self *TSession) setTimeouts(relax bool) error {
	if self.orm == nil || self.orm.dialect == nil {
		return nil
	}
	stmt := self.orm.dialect.StatementTimeoutSql(relax)
	if stmt == "" {
		return nil
	}
	if self.IsAutoCommit || self.tx == nil {
		return fmt.Errorf("orm: RelaxTimeouts/RestoreTimeouts requires an explicit transaction; call Begin() first")
	}
	// 直接走事务而不是 Exec()：Exec 会顺带重置会话上正在拼的 Statement，而这个方法
	// 可能夹在链式调用中间。日志照打——"这个事务放开过超时"是排查时要看得见的事。
	_, err := self.orm._logExecSql(stmt, nil, func() (sql.Result, error) {
		return self.tx.ExecContext(self.context, stmt)
	})
	if err != nil {
		return self.orm.dialect.MapError(err)
	}
	return nil
}
