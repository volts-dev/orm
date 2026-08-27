package orm

import (
	"fmt"

	"github.com/volts-dev/dataset"
)

// scopeIdsByDomain 用本次语句上累积的条件(Where()/Domain())把「点名的 ids」收窄成
// 条件同样成立的那一批，返回的必定是入参的子集；条件为空时原样返回。
//
// # 为什么必须有这一步
//
// UPDATE/DELETE 走「点名 id」时，SQL 是 `... WHERE id IN (...)`——**Statement 上
// 累积的条件整段不进 SQL**。于是凡是靠"往会话追加条件"实现的范围限制，在按 id 写
// 或删时全部失效：
//
//   - 多租户隔离：vectors 的 withSession 对 OpWrite/OpDelete 追加
//     `tenant_id = ?`，按 id 更新时这条不生效 —— 拿到别的租户的行 id 就能改/删它。
//   - 行级权限(记录规则)：同样是往会话追加 domain，按 id 写时一并失效。
//     2026-08-08 真栈实测：商家 A 对商家 B 的 stock.quant 发一次按 id 的更新，
//     **真实落库**，无错误、无日志——读侧隔离正常，写侧完全没有拦截。
//
// 读路径不受影响(SELECT 老老实实带上条件)，所以症状是"看不见但改得到"，
// 靠界面自测极难发现。
//
// # 语义
//
// 收窄而不是报错：条件表达的是「可见范围」，够不着的行等价于不存在，
// 与按条件更新时那些行不匹配是同一回事。需要把"够不着"变成明确报错的调用方，
// 应在业务层写前校验(vectors core/model.checkWriteAccess 即是)。
func (self *TSession) scopeIdsByDomain(ids []any) ([]any, error) {
	if len(ids) == 0 || self.Statement.domain.Count() == 0 {
		return ids, nil
	}

	query, err := self.Statement.where_calc(self.Statement.domain, false, nil)
	if err != nil {
		return nil, err
	}
	fromClause, whereClause, params := query.getSql()
	if whereClause == "" {
		return ids, nil
	}

	// 调用方加了 .ForUpdate() 时，这条"收窄可见范围"的 SELECT 同时把选中的行锁住，
	// 使随后的 UPDATE/DELETE 与本次可见性判定之间不再有窗口。不加锁时行为不变。
	lockClause, err := self.lockClause(false)
	if err != nil {
		return nil, err
	}

	idKey := self.Statement.IdKey

	// 主键必须限定到主表：条件里只要出现一个本表没有的列——委托继承(one2one/
	// _inherits)的父表字段，或写成点号的关联字段——where_calc 就把父表 JOIN 进
	// FROM，两张表都有 `id` 列，裸 `id` 在 PG 上直接是
	// `column reference "id" is ambiguous (42702)`：按 id 写/删整条路径 500，而
	// 报出来的只是一句 SQL 错误，不指向任何模型、任何字段。单表时限定同样合法，
	// 搜索路径(session_query.go 的 `SELECT "表"."id" FROM`)一直是这么拼的。
	quoter := self.orm.dialect.Quoter()
	qualifiedId := fmt.Sprintf("%s.%s", quoter.Quote(self.Statement.Model.Table()), quoter.Quote(idKey))
	sql := JoinClause(
		"SELECT", qualifiedId,
		"FROM", fromClause,
		"WHERE", whereClause,
		"AND", fmt.Sprintf("%s IN (%s)", qualifiedId, idsToSqlHolder(ids...)),
		lockClause,
	)
	params = append(params, ids...)

	// _query 的 defer 会复位 Statement。这条查询只是中间步骤，复位会把调用方
	// 接着要用的 Sets/Fields/IdParam 一起清空(现象同 _create 批量插入那处)。
	prevAutoReset := self.AutoResetStatement
	self.AutoResetStatement = false
	ds, err := self._query(sql, params...)
	self.AutoResetStatement = prevAutoReset
	if err != nil {
		return nil, err
	}
	if ds == nil || ds.Count() == 0 {
		return nil, nil
	}

	out := make([]any, 0, ds.Count())
	ds.Range(func(_ int, rec *dataset.TRecordSet) error {
		out = append(out, rec.GetByField(idKey))
		return nil
	})
	return out, nil
}
