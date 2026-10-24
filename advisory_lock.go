package orm

import (
	stdErrors "errors"
	"fmt"
	"hash/fnv"
	"sync"

	ormerr "github.com/volts-dev/orm/errors"
)

// 事务级咨询锁（MySQL 兼容 P4，2026-10-25）。
//
// 上层要串行化的常常是「一件事」而不是某一行：同一租户同一模块的安装 / 升级、同一个编号序列取号、
// 同一件器件的物化——那时可能一行都还没有。PG 有现成的 pg_advisory_xact_lock：锁随事务结束自动放。
// 此前 vectors 直接写 `SELECT pg_advisory_xact_lock(?)`，MySQL 上是语法错（探针里拿一律返回成功的替身
// 函数顶着，等于没锁）。
//
// MySQL 没有事务级的咨询锁。GET_LOCK 是**连接级**的：事务提交后连接回到池里，锁还挂在上面，下一个借到
// 这条连接的请求平白持着它——不能用。这里用 InnoDB 行锁模拟：一张只有主键的锁表，在调用方的事务里
// INSERT … ON DUPLICATE KEY UPDATE 那一行，拿到的行锁持有到事务提交或回滚，语义与 PG 一致；「试着拿」
// 用 SELECT … FOR UPDATE NOWAIT。锁表在默认库里、全库共用一张（PG 的咨询锁也是全库一份，不分 schema）。
//
// 锁键：PG 照发原来的语句，键不变（滚动部署时新旧进程仍然互斥）。

// advisoryLocker：方言实现事务级咨询锁。wait=false 时拿不到立即返回 false。
type advisoryLocker interface {
	advisoryXactLock(s *TSession, key int64, wait bool) (bool, error)
}

// AdvisoryXactLock 拿一把事务级咨询锁，拿不到就等；锁随事务提交 / 回滚释放。必须在事务里
// （Begin 之后）——自动提交的会话上，语句一结束锁就没了，等于没锁。
//
// 等锁这一步可能合法地久（排在前面的是一次几分钟的模块安装）：PG 上调用方照旧用 RelaxTimeouts 放开
// statement_timeout；MySQL 上等锁的上限（innodb_lock_wait_timeout）由这里自己放开、拿到后还原。
func (self *TSession) AdvisoryXactLock(key int64) error {
	_, err := self.advisoryLock(key, true)
	return err
}

// TryAdvisoryXactLock 试着拿一把事务级咨询锁，别人正持着时立即返回 false（不等）。
func (self *TSession) TryAdvisoryXactLock(key int64) (bool, error) {
	return self.advisoryLock(key, false)
}

// AdvisoryXactLockName 是按名字加锁的 AdvisoryXactLock：PG 上原样发 pg_advisory_xact_lock(hashtext(name))——
// 已有的按名字加锁的调用方（sys.sequence 取号）锁键因此不变，滚动部署时新旧进程仍然互斥；别的方言用
// FNV-1a 把名字折成键。
func (self *TSession) AdvisoryXactLockName(name string) error {
	if _, ok := self.orm.dialect.(*postgres); ok && !self.IsAutoCommit && self.tx != nil {
		_, err := self.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", name)
		return err
	}
	h := fnv.New64a()
	h.Write([]byte(name))
	return self.AdvisoryXactLock(int64(h.Sum64()))
}

func (self *TSession) advisoryLock(key int64, wait bool) (bool, error) {
	if self.IsAutoCommit || self.tx == nil {
		return false, ormerr.New(ormerr.ErrLockOutsideTransaction,
			fmt.Errorf("advisory lock %d: requires an explicit transaction; call Begin() first", key))
	}
	l, ok := self.orm.dialect.(advisoryLocker)
	if !ok {
		return false, ormerr.New(ormerr.ErrLockNotSupported, fmt.Errorf("advisory lock: %s", self.orm.dialect.DBType()))
	}
	return l.advisoryXactLock(self, key, wait)
}

// ---- PG ----

func (db *postgres) advisoryXactLock(s *TSession, key int64, wait bool) (bool, error) {
	if wait {
		_, err := s.Exec("SELECT pg_advisory_xact_lock(?)", key)
		return err == nil, err
	}
	ds, err := s.Query("SELECT pg_try_advisory_xact_lock(?) AS locked", key)
	if err != nil {
		return false, err
	}
	return ds != nil && ds.Count() > 0 && ds.FieldByName("locked").AsBoolean(), nil
}

// ---- SQLite ----

// SQLite 的写事务本来就是全库串行的，没有比它更细的锁可拿：恒成功。
func (db *sqlite) advisoryXactLock(_ *TSession, _ int64, _ bool) (bool, error) { return true, nil }

// ---- MySQL ----

// MySQLAdvisoryLockTable 是 MySQL 上模拟咨询锁的锁表（默认库里，每个锁键一行，只增不删）。
const MySQLAdvisoryLockTable = "orm_advisory_lock"

// mysqlLockTableReady：每个 TOrm 只建一次锁表。
var mysqlLockTableReady sync.Map // *TOrm → *sync.Once 的结果（error）

func (db *mysql) ensureAdvisoryLockTable(o *TOrm) error {
	type once struct {
		sync.Once
		err error
	}
	v, _ := mysqlLockTableReady.LoadOrStore(o, &once{})
	on := v.(*once)
	on.Do(func() {
		// 另一条连接、事务之外：MySQL 的 DDL 会隐式提交当前事务，在调用方的事务里建表等于替它提交。
		_, on.err = o.privilegedExec("CREATE TABLE IF NOT EXISTS " + db.tableRef("", MySQLAdvisoryLockTable) +
			" (" + db.dialect.Quoter().Quote("k") + " BIGINT NOT NULL PRIMARY KEY) ENGINE=InnoDB")
	})
	return on.err
}

func (db *mysql) advisoryXactLock(s *TSession, key int64, wait bool) (bool, error) {
	if err := db.ensureAdvisoryLockTable(s.orm); err != nil {
		return false, fmt.Errorf("advisory lock: create %s: %w", MySQLAdvisoryLockTable, err)
	}
	if err := db.ensureAdvisoryKeyRow(s.orm, key); err != nil {
		return false, fmt.Errorf("advisory lock %d: %w", key, err)
	}
	q := db.dialect.Quoter()
	k := q.Quote("k")
	sel := "SELECT " + k + " FROM " + db.tableRef("", MySQLAdvisoryLockTable) + " WHERE " + k + " = ? FOR UPDATE"
	if !wait {
		ds, err := s.Query(sel+" NOWAIT", key)
		if err != nil {
			if stdErrors.Is(err, ormerr.ErrConflict) { // 3572：别人持着
				return false, nil
			}
			return false, err
		}
		return ds != nil && ds.Count() > 0, nil
	}
	// 等锁上限按会话设、用完还原（MySQL 没有只在一条语句上生效的写法）。还原失败的话这条连接上的等锁上限
	// 留在放开后的值——只会让以后的锁等得更久，不会错。
	if _, err := s.Exec("SET SESSION innodb_lock_wait_timeout = 1073741824"); err != nil { // 上限值：一直等
		return false, err
	}
	_, err := s.Query(sel, key)
	if _, rerr := s.Exec("SET SESSION innodb_lock_wait_timeout = DEFAULT"); err == nil {
		err = rerr
	}
	return err == nil, err
}

// mysqlAdvisoryKeys：本进程确认过锁表里已经有的键。
var mysqlAdvisoryKeys sync.Map // [2]any{*TOrm, int64} → struct{}

// ensureAdvisoryKeyRow 保证锁表里有 key 这一行——**在调用方的事务之外**（另一条自动提交的连接）。
//
// 行不在调用方的事务里建：REPEATABLE READ（RDS 的默认）下，事务里对一个不存在的行 SELECT … FOR UPDATE
// 会加间隙锁，挡住别的键第一次插入（「试着拿」另一个键也拿不到）；在事务里 INSERT 则要等别人持着的锁。
// 行事先就在，事务里按主键等值锁一条存在的行，只有记录锁。
func (db *mysql) ensureAdvisoryKeyRow(o *TOrm, key int64) error {
	ck := [2]any{o, key}
	if _, ok := mysqlAdvisoryKeys.Load(ck); ok {
		return nil
	}
	q := db.dialect.Quoter()
	k := q.Quote("k")
	table := db.tableRef("", MySQLAdvisoryLockTable)
	s := o.NewPrivilegedSession()
	defer s.Close()
	// 先不加锁地看一眼：行在就不插（INSERT IGNORE 碰上已有的行要拿共享锁，别人持着时会等）。
	ds, err := s.Query("SELECT "+k+" FROM "+table+" WHERE "+k+" = ?", key)
	if err != nil {
		return err
	}
	if ds == nil || ds.Count() == 0 {
		if _, err := s.Exec("INSERT IGNORE INTO "+table+" ("+k+") VALUES (?)", key); err != nil {
			return err
		}
	}
	mysqlAdvisoryKeys.Store(ck, struct{}{})
	return nil
}
