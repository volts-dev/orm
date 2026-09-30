package orm

import (
	"database/sql"
	"fmt"

	"github.com/volts-dev/orm/core"
)

// 特权连接池：跨范围的活走另一个库身份。
//
// 开了行级安全之后，请求流量用的身份（vectors 里是 vs_app）只看得见自己那个租户的
// 行，也没有改表结构的权限。但有几类活天生跨租户或要改表：
//
//   - 结构同步（SyncModel）、运行期补字段（AddField），连同它们的一次性回填；
//   - 平台控制面、逐租户扫描的定时任务、全库迁移。
//
// 它们走 NewPrivilegedSession()。配了 PrivilegedDataSource 时，那是一个单独的池、
// 单独的身份（表的属主，带 BYPASSRLS）；没配时就是主池——今天的行为，一个字节不变。
// 两种情况下特权会话都既不发行范围、也不报行范围事件（见 row_scope.go）。
//
// 单独成池而不是在普通连接上 SET ROLE：普通身份一旦能切到特权身份，一条被注入的
// SQL 也能切过去，这道门就只拦得住疏忽、拦不住攻击。

// WithPrivilegedDataSource 配置特权连接池。maxOpen 是它的连接上限（0 = 与主池同一
// 套规则：不设就不限）；空闲回收、等连接上限沿用主池的设置。
func WithPrivilegedDataSource(ds *TDataSource, maxOpen int) Option {
	return func(cfg *Config) {
		cfg.PrivilegedDataSource = ds
		cfg.PrivilegedMaxOpenConns = maxOpen
	}
}

// openPrivileged 打开特权连接池。只建池、不连库：database/sql 在第一次用到时才拨号，
// 一个从不跨范围的进程就一条特权连接也不占。
func openPrivileged(cfg *Config) (*core.DB, error) {
	ds := cfg.PrivilegedDataSource
	if ds == nil {
		return nil, nil
	}
	if ds.DbType == "" {
		ds.DbType = cfg.DataSource.DbType
	}
	if ds.DbType != cfg.DataSource.DbType {
		return nil, fmt.Errorf("orm: privileged data source is %s but the main one is %s", ds.DbType, cfg.DataSource.DbType)
	}
	dsStr, err := ds.toString()
	if err != nil {
		return nil, fmt.Errorf("orm: privileged data source: %w", err)
	}
	db, err := core.Open(ds.DbType, dsStr)
	if err != nil {
		return nil, fmt.Errorf("orm: privileged data source: %w", err)
	}
	pool := *cfg
	pool.MaxOpenConns = cfg.PrivilegedMaxOpenConns
	// 空闲连接数跟着上限走：特权池平时是闲的，别让它照主池的数留一堆空闲连接。
	if pool.MaxOpenConns > 0 && (pool.MaxIdleConns == 0 || pool.MaxIdleConns > pool.MaxOpenConns) {
		pool.MaxIdleConns = pool.MaxOpenConns
	}
	applyPoolConfig(db, &pool)
	return db, nil
}

// HasPrivilegedPool 报告是否配了单独的特权连接池。
func (self *TOrm) HasPrivilegedPool() bool {
	return self.privDB != nil
}

// NewPrivilegedSession 返回一个特权会话，见文件头。
func (self *TOrm) NewPrivilegedSession() *TSession {
	session := NewSession(self)
	if self.privDB != nil {
		session.db = self.privDB
	}
	session.privileged = true
	return session
}

// PrivilegedPoolStats 返回特权连接池的计数；没配单独的池时第二个返回值是 false。
func (self *TOrm) PrivilegedPoolStats() (core.PoolStats, bool) {
	if self.privDB == nil {
		return core.PoolStats{}, false
	}
	return self.privDB.PoolStats(), true
}

// privilegedExec 在一个特权会话上执行一条语句（结构变更用）。
func (self *TOrm) privilegedExec(query string, args ...any) (sql.Result, error) {
	session := self.NewPrivilegedSession()
	defer session.Close()
	return session.Exec(query, args...)
}

// privilegedCopy 返回一个与本会话同 schema、同 context 的特权会话。
func (self *TSession) privilegedCopy() *TSession {
	session := self.orm.NewPrivilegedSession()
	session.Schema = self.Schema
	session.context = self.context
	return session
}
