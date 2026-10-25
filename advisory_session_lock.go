package orm

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strconv"
)

// 会话级咨询锁：跨很多个事务都要持着的锁（P5，2026-10-25）。
//
// AdvisoryXactLock 随事务释放；分很多个事务跑几十秒的活（vectors 的媒体迁移一张表一把锁）要的是会话级的。
// 会话级的锁挂在**连接**上：从连接池里随手拿一条去加锁、再随手拿一条去解锁，两条多半不是同一条——解锁落空，
// 锁就留在那条回了池的连接上，直到连接被回收（此前媒体迁移就是这样：注释以为「回池时库会自己收」，不会）。
// 所以这里独占一条连接，加锁、解锁都在它上面，解锁后才还回去。
//
//   - PG：pg_try_advisory_lock(key) / pg_advisory_unlock(key)。
//   - MySQL：GET_LOCK('orm:<key>', 0) / RELEASE_LOCK（同样是连接级；名字最长 64 字符）。

// sessionLocker：方言的会话级锁语句。try 返回一列 ok（真 = 拿到）。
type sessionLocker interface {
	sessionLockSql(key int64) (try string, unlock string, args []any)
}

func (db *postgres) sessionLockSql(key int64) (string, string, []any) {
	return "SELECT pg_try_advisory_lock($1)", "SELECT pg_advisory_unlock($1)", []any{key}
}

func (db *mysql) sessionLockSql(key int64) (string, string, []any) {
	return "SELECT GET_LOCK(?, 0) = 1", "SELECT RELEASE_LOCK(?)", []any{"orm:" + strconv.FormatInt(key, 10)}
}

// TrySessionLock 试着拿一把会话级咨询锁，别人持着时立即返回 ok=false（不等）。拿到时本进程独占一条连接（特权池，
// 没配特权池就是主池），unlock 解锁并把连接还回去——unlock 一定要调。SQLite 没有这种锁：直接当拿到（单进程）。
func (self *TOrm) TrySessionLock(key int64) (unlock func(), ok bool, err error) {
	d, has := self.dialect.(sessionLocker)
	if !has {
		return func() {}, true, nil
	}
	pool := self.db
	if self.privDB != nil {
		pool = self.privDB
	}
	ctx := context.Background()
	conn, err := pool.DB.Conn(ctx)
	if err != nil {
		return func() {}, false, err
	}
	try, release, args := d.sessionLockSql(key)
	var got sql.NullBool
	if err := conn.QueryRowContext(ctx, try, args...).Scan(&got); err != nil {
		conn.Close()
		return func() {}, false, self.dialect.MapError(err)
	}
	if !got.Valid || !got.Bool {
		conn.Close()
		return func() {}, false, nil
	}
	return func() {
		if _, err := conn.ExecContext(ctx, release, args...); err != nil {
			// 解不掉就把这条连接扔掉：连接一断，锁随之释放，不留给下一个借用者。
			log.Warnf("orm: 会话锁 %d 没解掉，丢弃这条连接: %v", key, err)
			conn.Raw(func(any) error { return driver.ErrBadConn }) //nolint:errcheck
		}
		conn.Close()
	}, true, nil
}
