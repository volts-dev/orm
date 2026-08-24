package orm

import (
	stdErrors "errors"
	"fmt"

	ormerr "github.com/volts-dev/orm/errors"
)

// lockClause 把 Statement 上的行锁解析成可直接拼进 SQL 的子句（追加在
// LIMIT/OFFSET 之后）。没有请求锁时返回 ("", nil)。
//
// isAggregate 表示这条查询带 GROUP BY 或聚合（Count/Sum）。
//
// 这里的三道校验都是"宁可报错也不静默"：ForUpdate() 以前的问题不是锁得不好，
// 而是根本没锁却看不出来。任何一种"发出去也没用"的情形都必须让调用方知道。
func (self *TSession) lockClause(isAggregate bool) (string, error) {
	lock := self.Statement.Lock
	if !lock.IsLocking() {
		return "", nil
	}

	model := ""
	if self.Statement.Model != nil {
		model = self.Statement.Model.String()
	}

	// 1) 不在事务里加锁没有任何意义：语句一结束隐式事务就提交，锁随即释放，
	//    读-改-写之间照样有窗口。这正是调用方最容易误以为已经安全的情形。
	if self.IsAutoCommit {
		return "", fmt.Errorf("%w (model=%s, lock=%s)", ormerr.ErrLockOutsideTransaction, model, lock)
	}

	// 2) 聚合结果不对应具体的行，没有可锁的对象。PG 本来也会拒绝
	//    （"FOR UPDATE is not allowed with GROUP BY clause"），提前报能指明是哪个模型。
	if isAggregate {
		return "", fmt.Errorf("%w: aggregate query (model=%s, lock=%s)", ormerr.ErrLockNotApplicable, model, lock)
	}

	// 3) 只锁主模型的表。读取路径会为继承字段(_inherits)接 LEFT JOIN 父表，
	//    PG 拒绝锁 outer join 的可空侧；而 .ForUpdate() 想锁的本来就是主表的行。
	alias := ""
	if self.Statement.Model != nil {
		alias = self.orm.dialect.Quoter().Quote(self.Statement.Model.Table())
	}

	clause, err := self.orm.dialect.LockClause(lock, alias)
	if err != nil {
		// 方言没有行锁(sqlite)：降级为警告而非失败，但**必须**留下痕迹，
		// 否则又回到"以为加了锁其实没有"的老路。
		if stdErrors.Is(err, ormerr.ErrLockNotSupported) {
			log.Warnf("%s does not support row-level locking; %q on model %s is ignored, relying on transaction-level isolation instead",
				self.orm.dialect.DBType(), lock.String(), model)
			return "", nil
		}
		return "", err
	}

	return clause, nil
}
