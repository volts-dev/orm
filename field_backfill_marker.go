package orm

import (
	"fmt"
	"sync"
)

// 没有事务性 DDL 的库上，AddField 的一次性回填靠一行「待回填」标记保证（MySQL 兼容 P5，2026-10-25）。
//
// WithBackfill 的承诺是「列（或关联表）第一次建出来时恰好回填一次」。PG / SQLite 上建列与回填在同一个事务里：
// 回填失败，列也跟着回滚，下次再补时重来。MySQL 的 ALTER TABLE / CREATE TABLE 当场隐式提交——回填失败时列已经
// 在了，下一次看到「列已存在」就不再回填，旧值悄悄丢了；两个进程并发时，后到的那个看见列已在，也不知道先到的
// 那个的回填到底成了没有。
//
// 所以在这种库上：
//
//  1. 建列之前先写一行标记（自动提交，已有就不动）；
//  2. 建列（自动提交，别人抢先建了也算成功）；
//  3. 一个事务里锁住这行标记 → 回填 → 删标记 → 提交。标记已经没了（别人做完了）就什么都不做。
//
// 回填失败时标记留着：下次补这个字段时列已在，但标记在，照样再回填一次（回填在自己的事务里，失败的那次已整个
// 回滚）。列已在而没有标记：回填早就做完了（或者这一列本来就不是这次建的），不再做。

// MySQLFieldBackfillTable 是「待回填」标记表（默认库里，每个待回填的列 / 关联表一行，做完即删）。
const MySQLFieldBackfillTable = "orm_field_backfill"

// fieldBackfillMarker：方言没有事务性 DDL 时，怎么记「这一列还欠一次回填」。
type fieldBackfillMarker interface {
	// backfillMarkerTable 是标记表的全名（总带主库名，理由同 lockTable）。
	backfillMarkerTable() string
	// backfillMarkSQL 插入一行标记、已有就不动（一个 ? 占位：键）。
	backfillMarkSQL() string
	// backfillMarkerDDL 建标记表。
	backfillMarkerDDL() string
}

func (db *mysql) backfillMarkerTable() string {
	return db.dialect.Quoter().QuoteTable(db.DbName, MySQLFieldBackfillTable)
}

func (db *mysql) backfillMarkSQL() string {
	k := db.dialect.Quoter().Quote("k")
	return "INSERT INTO " + db.backfillMarkerTable() + " (" + k + ") VALUES (?) ON DUPLICATE KEY UPDATE " + k + " = " + k
}

func (db *mysql) backfillMarkerDDL() string {
	return "CREATE TABLE IF NOT EXISTS " + db.backfillMarkerTable() + " (" + db.dialect.Quoter().Quote("k") +
		" VARCHAR(255) NOT NULL PRIMARY KEY) ENGINE=InnoDB"
}

var backfillMarkerReady sync.Map // *TOrm → *backfillMarkerOnce

type backfillMarkerOnce struct {
	sync.Once
	err error
}

// backfillMarker 返回方言的标记实现；方言有事务性 DDL（或没实现标记）时为 nil——走建列与回填同一个事务的老路。
func (self *TOrm) backfillMarker() (fieldBackfillMarker, error) {
	if self.dialect.Features().TransactionalDDL {
		return nil, nil
	}
	m, ok := self.dialect.(fieldBackfillMarker)
	if !ok {
		return nil, nil
	}
	v, _ := backfillMarkerReady.LoadOrStore(self, &backfillMarkerOnce{})
	on := v.(*backfillMarkerOnce)
	on.Do(func() {
		// 自动提交的另一条连接：在谁的事务里建表都等于替它提交。
		if _, err := self.privilegedExec(m.backfillMarkerDDL()); err != nil {
			on.err = fmt.Errorf("create %s: %w", MySQLFieldBackfillTable, err)
		}
	})
	if on.err != nil {
		return nil, on.err
	}
	return m, nil
}

// backfillKey 是一列（column 非空）或一张关联表（column 为空）的标记键。
func backfillKey(schema, table, column string) string {
	return schema + "|" + table + "|" + column
}

// markBackfillPending 记下 key 还欠一次回填（自动提交）。
func (self *TOrm) markBackfillPending(m fieldBackfillMarker, key string) error {
	_, err := self.privilegedExec(m.backfillMarkSQL(), key)
	return err
}

// hasBackfillPending 报告 key 是否还欠一次回填。
func (self *TOrm) hasBackfillPending(m fieldBackfillMarker, key string) (bool, error) {
	s := self.NewPrivilegedSession()
	defer s.Close()
	ds, err := s._query("SELECT "+self.dialect.Quoter().Quote("k")+" FROM "+m.backfillMarkerTable()+" WHERE "+
		self.dialect.Quoter().Quote("k")+" = ?", key)
	if err != nil {
		return false, err
	}
	return ds != nil && ds.Count() > 0, nil
}

// runPendingBackfill 在一个事务里锁住 key 的标记、回填、删标记。标记已经没了就什么都不做。
func (self *TOrm) runPendingBackfill(m fieldBackfillMarker, schema, key string, fn func(s *TSession) error) error {
	s := self.NewPrivilegedSession()
	defer s.Close()
	s.Schema = normalizeSchema(self.dialect, schema)
	if err := s.Begin(); err != nil {
		return err
	}
	k := self.dialect.Quoter().Quote("k")
	ds, err := s.Query("SELECT "+k+" FROM "+m.backfillMarkerTable()+" WHERE "+k+" = ? FOR UPDATE", key)
	if err != nil {
		s.Rollback(err)
		return err
	}
	if ds == nil || ds.Count() == 0 {
		return s.Commit() // 别人做完了
	}
	if err := fn(s); err != nil {
		s.Rollback(err)
		return fmt.Errorf("backfill: %w", err)
	}
	if _, err := s.Exec("DELETE FROM "+m.backfillMarkerTable()+" WHERE "+k+" = ?", key); err != nil {
		s.Rollback(err)
		return err
	}
	return s.Commit()
}
