package orm

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
)

// 事务级临时表与事务里的 DDL（MySQL 兼容 P5，2026-10-25）。
//
// # 事务级临时表
//
// PG 的 CREATE TEMP TABLE … ON COMMIT DROP 在 MySQL / SQLite 上没有：那两家的临时表挂在**连接**上，事务结束后
// 还在，连接回池后下一个借到它的请求看得见里面的行（同名的 IF NOT EXISTS 直接复用上一位留下的数据）。
// TxTempTable 把三家收成一种语义：建一张空表，只在本事务里可见，提交 / 回滚之前在同一条连接上删掉。
//
// ★ MySQL 的临时表**在一条语句里只能引用一次**（Error 1137 Can't reopen table）：`a IN (SELECT id FROM t) OR
// b IN (SELECT id FROM t)`、自连接、UNION 两边都引用它，PG 上没事、MySQL 上报错。按列拆成几条语句写。
//
// # 事务里的 DDL
//
// MySQL 的 DDL 会**隐式提交**：事务里此前的写当场提交（之后的回滚撤不回）、事务里拿的行锁（包括 orm 的咨询锁）
// 当场释放、之后的语句按自动提交逐条生效。PG / SQLite 的 DDL 随事务回滚（Features().TransactionalDDL）。
// 方言没有事务性 DDL 时，事务里每一个发 DDL 的调用点记一条 Warn（同一调用点只记一次）；临时表的建 / 删不算
// （MySQL 上 CREATE / DROP TEMPORARY TABLE 不隐式提交）。

// txTempTabler：方言怎么建事务级临时表。ref 是语句里引用它的写法；create 是按顺序执行的语句；drop 非空时
// 登记在事务结束之前执行。
type txTempTabler interface {
	txTempTable(name, columns string) (ref string, create []string, drop string)
	// txTempAnalyze 是给灌好数据的临时表收集统计的语句，不需要时为空。
	txTempAnalyze(ref string) string
}

func (db *postgres) txTempAnalyze(ref string) string { return "ANALYZE " + ref }
func (db *mysql) txTempAnalyze(string) string        { return "" }
func (db *sqlite) txTempAnalyze(string) string       { return "" }

func (db *postgres) txTempTable(name, columns string) (string, []string, string) {
	q := db.quoter.Quote(name)
	return "pg_temp." + q, []string{"CREATE TEMP TABLE IF NOT EXISTS " + q + " " + columns + " ON COMMIT DROP"}, ""
}

func (db *mysql) txTempTable(name, columns string) (string, []string, string) {
	q := db.quoter.Quote(name)
	drop := "DROP TEMPORARY TABLE IF EXISTS " + q
	// 先 DROP：上一次在这条连接上没删成（提交前连接断了之类）留下的同名表。
	return q, []string{drop, "CREATE TEMPORARY TABLE " + q + " " + columns}, drop
}

func (db *sqlite) txTempTable(name, columns string) (string, []string, string) {
	q := db.quoter.Quote(name)
	drop := "DROP TABLE IF EXISTS temp." + q
	return "temp." + q, []string{drop, "CREATE TEMP TABLE " + q + " " + columns}, drop
}

// TxTempTable 在当前事务里建一张空的临时表 name（columns 是列定义，如 "(id bigint PRIMARY KEY)"），返回在
// 语句里引用它的写法（PG 是 pg_temp."name"）。同一事务里再调一次同名的：清空后复用。事务结束即删。
//
// 会话必须在事务里。MySQL 上一条语句只能引用它一次（见文件头）。
func (self *TSession) TxTempTable(name, columns string) (string, error) {
	self.syncBorrowedTx()
	if self.IsAutoCommit || self.tx == nil {
		return "", fmt.Errorf("orm: TxTempTable %q needs a transaction", name)
	}
	d, ok := self.orm.dialect.(txTempTabler)
	if !ok {
		return "", fmt.Errorf("orm: TxTempTable: dialect %s has no temporary tables", self.orm.dialect.DBType())
	}
	ref, create, drop := d.txTempTable(name, columns)
	if self.tx.MarkTempTable(name) {
		if _, err := self.Exec("DELETE FROM " + ref); err != nil {
			return "", err
		}
		return ref, nil
	}
	for _, q := range create {
		if _, err := self.Exec(q); err != nil {
			return "", err
		}
	}
	if drop != "" {
		self.tx.BeforeEnd(drop)
	}
	return ref, nil
}

// AnalyzeTxTempTable 让库为刚灌好数据的临时表 ref（TxTempTable 的返回值）收集统计：PG 的 autovacuum 不碰临时表，
// 不 ANALYZE 的话规划器按默认行数估，`x IN (SELECT id FROM 临时表)` 在大表上会挑成嵌套循环。MySQL / SQLite 不做
// （MySQL 的 ANALYZE TABLE 会隐式提交；它对临时表的子查询走物化，不靠统计）。
func (self *TSession) AnalyzeTxTempTable(ref string) error {
	d, ok := self.orm.dialect.(txTempTabler)
	if !ok {
		return nil
	}
	q := d.txTempAnalyze(ref)
	if q == "" {
		return nil
	}
	_, err := self.Exec(q)
	return err
}

// implicitCommitDDL：这条语句在没有事务性 DDL 的库（MySQL）上会不会隐式提交事务。临时表的建 / 删不会。
func implicitCommitDDL(sql string) bool {
	if !isDDL(sql) {
		return false
	}
	u := strings.ToUpper(strings.Join(strings.Fields(sql), " "))
	for _, p := range []string{"CREATE TEMPORARY ", "CREATE TEMP ", "DROP TEMPORARY ", "COMMENT "} {
		if strings.HasPrefix(u, p) {
			return false
		}
	}
	return true
}

var ddlInTxWarned sync.Map // 调用点 → struct{}

// warnDDLInTx 在方言没有事务性 DDL、会话却在事务里发 DDL 时记一条 Warn（每个调用点一次）。
func (self *TSession) warnDDLInTx(sql string) {
	if self.IsAutoCommit || self.orm.dialect.Features().TransactionalDDL || !implicitCommitDDL(sql) {
		return
	}
	site := callerOutsideOrm()
	if _, dup := ddlInTxWarned.LoadOrStore(site, struct{}{}); dup {
		return
	}
	stmt := strings.Join(strings.Fields(sql), " ")
	if len(stmt) > 160 {
		stmt = stmt[:160] + "…"
	}
	log.Warnf("orm: %s 上 DDL 会隐式提交事务（此前的写已提交、回滚撤不回，事务里拿的锁当场释放）——把它挪到事务外：%s（%s）",
		self.orm.dialect.DBType(), stmt, site)
}

// callerOutsideOrm 返回调用栈上第一个不在 orm 包里的帧（file:line）；orm 自己的 _test.go 算外面。
func callerOutsideOrm() string {
	pcs := make([]uintptr, 32)
	n := runtime.Callers(3, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	for {
		f, more := frames.Next()
		inOrm := strings.Contains(f.Function, "volts-dev/orm.") || strings.Contains(f.Function, "volts-dev/orm/")
		if !inOrm || strings.HasSuffix(f.File, "_test.go") {
			return fmt.Sprintf("%s:%d", f.File, f.Line)
		}
		if !more {
			return "?"
		}
	}
}
