package orm

import (
	"database/sql"
	"fmt"

	"github.com/volts-dev/dataset"
)

// 让**裸 SQL** 也落在会话指定的 schema 上。
//
// # 病灶
//
// SetSchema 一直只影响 ORM **自己拼**的 SQL（statement.qualifiedTable 与各 builder）。
// Exec/Query 收到的是字符串，表名已经写在里面了，那一层够不着——裸表名由 postgres
// 的 search_path 解析，永远落 public。于是
//
//	s.SetSchema("system"); s.Exec("DELETE FROM foo WHERE …")
//
// 删的是 public.foo，**不报错**。同一张表 ORM 写 system、裸 SQL 读 public，查 0 行。
//
// 真实代价（vectors core/db/raw_sql_guard_test.go 记的战绩）：税额算成 0、组税展不开、
// property 的 m2m 读恒空；最贵的一次在 product —— 读关联表拿"当前挂着哪些值"读成空，
// 调用方据此拼 `(6,0,全集)` 覆盖式写回，**把原有取值全抹掉**。也就是说读错 schema
// 会从"读不到"升级成"丢数据"，而全程没有任何错误。
//
// # 为什么不能在运行期拦
//
// 正确写法是 `db.QualifiedTable(tid, "mail_mail")`，它对普通租户**正确地返回不带
// 前缀**的表名。运行期拿到的字符串里，"正确地没前缀"和"忘了加前缀"一模一样，分不开。
// vectors 因此把检查做成了静态 AST 守卫——那只能覆盖它自己那两个仓的字面量 SQL。
//
// # 处置
//
// 把 schema 变成**连接属性**而不是字符串前缀：进事务时发一条
// `SET LOCAL search_path TO <schema>, public`，裸 SQL 于是天然落对地方，
// 而已经显式限定的 SQL（system.foo）不受任何影响。
//
// 必须是 SET **LOCAL**（事务级）。会话级 SET 会留在连接上，而 database/sql 的连接
// 是池化的——下一个借到它的请求会继承这个 schema，那是跨租户串数据，比原来的 bug
// 严重得多。自动提交路径因此不能直接 SET，而是把语句包进一个隐式事务，见下。

// searchPathSql 返回本会话需要的 search_path 语句；不需要时返回空串。
func (self *TSession) searchPathSql() string {
	if self.Schema == "" || self.orm == nil || self.orm.dialect == nil {
		return ""
	}
	return self.orm.dialect.SetSearchPathSql(self.Schema)
}

// applySearchPath 在**已开启的事务**上设置 search_path。Begin 之后调用。
func (self *TSession) applySearchPath() error {
	stmt := self.searchPathSql()
	if stmt == "" || self.tx == nil {
		return nil
	}
	if _, err := self.tx.ExecContext(self.context, stmt); err != nil {
		return self.orm.dialect.MapError(err)
	}
	return nil
}

// execInSchemaScope 在自动提交会话上执行一条语句，并保证它与 SET LOCAL 落在
// **同一条连接**上——办法是包一个隐式事务。
//
// 代价是两次额外往返，只发生在设了非默认 schema 的会话上。正确性优先：一条
// 打错 schema 的 DELETE 不报错，比多两次往返贵得多。
func (self *TSession) execInSchemaScope(stmt string, query string, args ...any) (sql.Result, error) {
	tx, err := self.db.BeginTx(self.context, nil)
	if err != nil {
		return nil, self.orm.dialect.MapError(err)
	}
	if _, err := tx.ExecContext(self.context, stmt); err != nil {
		tx.Rollback()
		return nil, self.orm.dialect.MapError(err)
	}
	res, err := tx.ExecContext(self.context, query, args...)
	if err != nil {
		tx.Rollback()
		return nil, self.orm.dialect.MapError(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, self.orm.dialect.MapError(err)
	}
	return res, nil
}

// queryInSchemaScope 同上，查询侧。
//
// 结果必须在 Commit **之前**读完：_scanRows 会把整个结果集物化成 dataset，
// 所以提交后再返回是安全的；换成流式游标就不成立了。
func (self *TSession) queryInSchemaScope(stmt string, query string, args ...any) (*dataset.TDataSet, error) {
	tx, err := self.db.BeginTx(self.context, nil)
	if err != nil {
		return nil, self.orm.dialect.MapError(err)
	}
	if _, err := tx.ExecContext(self.context, stmt); err != nil {
		tx.Rollback()
		return nil, self.orm.dialect.MapError(err)
	}
	rows, err := tx.QueryContext(self.context, query, args...)
	if err != nil {
		tx.Rollback()
		return nil, self.orm.dialect.MapError(err)
	}
	ds, err := self._scanRows(rows)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, self.orm.dialect.MapError(err)
	}
	return ds, nil
}

// ensureOpen 拦下"在已关闭的会话上继续用"。
//
// Close() 把 db 与 tx 都置 nil，之后任何一次执行都是**空指针崩溃**——整个进程带走，
// 而调用栈指向 core/db.go 的驱动层，完全看不出根因是"这个会话早就关了"。
//
// 这不是假想：公开的 Exec/Query 在 IsAutoClose 的会话上自己 `defer Close()`，
// 于是 `s := orm.Model(x); s.Exec(...); s.Exec(...)` 第二次必崩。m2m 的
// link/unlink_all 正是这个形状(见 field_relational.go)。
func (self *TSession) ensureOpen() error {
	if self.db == nil && self.tx == nil {
		return fmt.Errorf("orm: session is closed (Close() was called, or an IsAutoClose session was reused after Exec/Query) — start a new session")
	}
	return nil
}
