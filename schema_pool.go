package orm

import (
	"fmt"
	"sync"

	"github.com/volts-dev/orm/core"
)

// 每个 schema 一个连接池（MySQL 兼容 P4，2026-10-25）。
//
// MySQL 的 schema 就是库（database）。ORM 自己拼的 SQL 都带库名限定，不受影响；**裸 SQL** 的未限定表名按
// 连接的当前库解析——PG 上同一个问题由 SET LOCAL search_path 解决（session_schema.go，事务级），MySQL 没有
// 事务级的等价物：USE 是**连接级**的，设了就留在连接上，连接回池后下一个借到它的请求（别的租户、别的
// schema）平白落进这个库，那是跨租户串数据。回池前再 USE 回去也不行：出错的那条路上漏一次就是事故。
//
// 所以 MySQL 上设了 schema 的会话走另一个池：池里每条连接**打开时**就在那个库（DSN 的 dbname），连接上
// 没有可泄漏的状态。实际只有两个库在用（public：全部普通租户；system：超级租户），每个池按需才建、
// 第一次用到才拨号，参数（上限、空闲回收、等连接上限）与它的基池相同，钩子共用。
//
// 探针 10-25：存储实例表 sys_blob_store 在 system 库，PlatformSystemSession 上的裸 SQL 查到的是 public 库
// （表不存在）→ 整个进程判成「没配 blob 存储」，所有图片都存不进去。

// schemaDatabases：方言的 schema 就是库，裸 SQL 要靠连接的当前库落对地方（MySQL）。
type schemaDatabases interface {
	schemaIsDatabase() bool
}

func (db *mysql) schemaIsDatabase() bool { return true }

type schemaPoolKey struct {
	base   *core.DB
	schema string
}

type schemaPoolEntry struct {
	once sync.Once
	db   *core.DB
	err  error
}

// pool 返回本会话这一次要用的连接池：没设 schema、或方言不需要时就是会话自己的池。
func (self *TSession) pool() (*core.DB, error) {
	if self.Schema == "" || self.orm == nil || self.db == nil {
		return self.db, nil
	}
	if d, ok := self.orm.dialect.(schemaDatabases); !ok || !d.schemaIsDatabase() {
		return self.db, nil
	}
	return self.orm.schemaPool(self.db, self.Schema)
}

// schemaPool 返回基池 base 在库 schema 上的那个池，没有就建。
func (self *TOrm) schemaPool(base *core.DB, schema string) (*core.DB, error) {
	v, _ := self.schemaPools.LoadOrStore(schemaPoolKey{base, schema}, &schemaPoolEntry{})
	e := v.(*schemaPoolEntry)
	e.once.Do(func() {
		ds, cfg := self.poolSource(base)
		if ds == nil {
			e.err = fmt.Errorf("orm: schema %q: unknown base pool", schema)
			return
		}
		cp := *ds
		cp.DbName = schema
		dsStr, err := cp.toString()
		if err != nil {
			e.err = fmt.Errorf("orm: schema %q pool: %w", schema, err)
			return
		}
		db, err := core.Open(cp.DbType, dsStr)
		if err != nil {
			e.err = fmt.Errorf("orm: schema %q pool: %w", schema, err)
			return
		}
		applyPoolConfig(db, cfg)
		self.hookMu.Lock()
		db.AddHook(self.hooks...)
		self.hookMu.Unlock()
		e.db = db
	})
	return e.db, e.err
}

// poolSource 回基池对应的数据源与池参数（主池 / 特权池）。
func (self *TOrm) poolSource(base *core.DB) (*TDataSource, *Config) {
	switch {
	case base == self.db:
		return self.config.DataSource, self.config
	case base != nil && base == self.privDB:
		pool := *self.config
		pool.MaxOpenConns = self.config.PrivilegedMaxOpenConns
		if pool.MaxOpenConns > 0 && (pool.MaxIdleConns == 0 || pool.MaxIdleConns > pool.MaxOpenConns) {
			pool.MaxIdleConns = pool.MaxOpenConns
		}
		ds := *self.config.PrivilegedDataSource
		if ds.DbType == "" {
			ds.DbType = self.config.DataSource.DbType
		}
		return &ds, &pool
	}
	return nil, nil
}

// eachSchemaPool 对已建好的每个 schema 池调一次 fn。
func (self *TOrm) eachSchemaPool(fn func(key schemaPoolKey, db *core.DB)) {
	self.schemaPools.Range(func(k, v any) bool {
		if e := v.(*schemaPoolEntry); e.db != nil {
			fn(k.(schemaPoolKey), e.db)
		}
		return true
	})
}

// SchemaPoolStats 返回各 schema 池的计数：键是 schema 名，特权池的那个加后缀 "(privileged)"。
// 没有任何 schema 池（PG / SQLite，或 MySQL 上还没用到）时为空。
func (self *TOrm) SchemaPoolStats() map[string]core.PoolStats {
	out := map[string]core.PoolStats{}
	self.eachSchemaPool(func(k schemaPoolKey, db *core.DB) {
		name := k.schema
		if k.base != self.db {
			name += "(privileged)"
		}
		out[name] = db.PoolStats()
	})
	return out
}
