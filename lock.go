package orm

// 行级锁（SELECT ... FOR UPDATE / FOR SHARE）。
//
// # 背景
//
// 本文件之前不存在：ForUpdate() 只把 Statement.IsForUpdate 置 true，而**全仓没有
// 任何一处读它**，dialect.ForUpdateSql() 也没有调用点——调用方以为拿到了行锁，
// 实际发出去的是一条普通 SELECT。这类"会骗人的 API"比缺失更危险：读-改-写之间
// 没有任何互斥，并发下静默丢更新，且代码看上去是对的。
//
// 使用者只好绕开：vectors 的 core/tenant/advisory_lock.go 改用 PG 咨询锁、
// registry/internal/manager/invite.go 用裸 Exec 自己拼 CAS。
//
// # 语义约定
//
//   - 锁只在事务里有意义。不在事务中（IsAutoCommit）调用一律**报错**而不是静默
//     发一条没用的 SQL——单语句事务会在语句结束的瞬间释放锁，等于没锁。
//   - 方言不支持行锁（sqlite）时返回 ErrLockNotSupported，由 session 降级为一条
//     警告并照常执行：SQLite 的写事务本身就是全库互斥，行锁无从谈起也无必要。
//   - 聚合查询（GROUP BY / Count）不能加锁，直接报错；PG 本来也会拒绝，提前报
//     能给出更有指向的信息。
type (
	// LockMode 行锁强度。
	LockMode uint8

	// LockWait 抢不到锁时的行为。
	LockWait uint8

	// TLock 一次查询要施加的行锁。零值（LockNone）表示不加锁。
	TLock struct {
		Mode LockMode
		Wait LockWait
	}

	// LockOption 以选项方式微调锁行为，见 NoWait / SkipLocked。
	LockOption func(*TLock)
)

const (
	// LockNone 不加锁
	LockNone LockMode = iota
	// LockUpdate 排他行锁：FOR UPDATE。其他事务既不能改也不能加锁读这些行。
	LockUpdate
	// LockShare 共享行锁：FOR SHARE（MySQL 5.7 为 LOCK IN SHARE MODE）。
	// 允许并发的共享读锁，阻止写。
	LockShare
)

const (
	// LockWaitBlock 阻塞等待，直到拿到锁（默认）
	LockWaitBlock LockWait = iota
	// LockWaitNoWait 拿不到锁立刻报错（NOWAIT）。错误经 MapError 映射为 errors.ErrConflict。
	LockWaitNoWait
	// LockWaitSkip 跳过已被别人锁住的行（SKIP LOCKED）。典型用于任务队列取件。
	LockWaitSkip
)

// NoWait 抢不到锁立刻返回错误，不阻塞。
func NoWait() LockOption {
	return func(l *TLock) { l.Wait = LockWaitNoWait }
}

// SkipLocked 跳过被其他事务锁住的行——结果集因此可能少于 WHERE 实际匹配的行数。
func SkipLocked() LockOption {
	return func(l *TLock) { l.Wait = LockWaitSkip }
}

// IsLocking 报告该锁是否真的要生成锁子句。
func (self *TLock) IsLocking() bool {
	return self != nil && self.Mode != LockNone
}

// String 用于日志与错误信息。
func (self *TLock) String() string {
	if !self.IsLocking() {
		return "none"
	}
	s := "for update"
	if self.Mode == LockShare {
		s = "for share"
	}
	switch self.Wait {
	case LockWaitNoWait:
		s += " nowait"
	case LockWaitSkip:
		s += " skip locked"
	}
	return s
}

func newLock(mode LockMode, opts ...LockOption) *TLock {
	l := &TLock{Mode: mode}
	for _, opt := range opts {
		if opt != nil {
			opt(l)
		}
	}
	return l
}
