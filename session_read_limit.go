package orm

import (
	"fmt"

	"github.com/volts-dev/dataset"
	ormerr "github.com/volts-dev/orm/errors"
)

// 读取的两道上限守卫。
//
// # 背景
//
// DefaultLimit=500 当初是为了"别让人无意中扫全表"。实际拦到的却不是这件事：
//
//   - Search() 压根没有默认上限（session_query.go 那里是 `if LimitClause > 0`），
//     枚举千万行的 id 一直是放行的。想扫表的人一直扫得到。
//   - 被拦的全是**已经被 WHERE 圈住**的查询。vectors + modules 里 270 处
//     `Limit(-1)` 无一例外是这种形状（"这张单的全部明细"、"这个向导的全部行"），
//     作者不是想要无界取数，是那个集合本来就有界，500 落在这里纯粹是破坏。
//   - 更糟的是它**静默**：截断在一次已经圈定范围的查询上，结果是"看着正常的错数据"。
//
// # 处置
//
// 原则是**永远不静默截断：要么全给，要么明确拒绝**。
//
//   - warnIfTruncated：隐式上限真的截到了数据，就说出来。
//   - guardUnscopedRead：把守卫挪到真正无界的地方——一条 Ids/Where/Domain 都没有的
//     读取。那才是"扫全表"，而拒绝不会产出错数据。

// warnIfTruncated 在隐式默认上限真的截断了结果时留下痕迹。
//
// 判据是"回来的行数正好等于上限"。它会有误报（恰好 500 行的结果集），但漏报的代价
// 是一条查不出来源的错数据，误报的代价是一行日志——这个不对称决定了宁可多报。
func (self *TSession) warnIfTruncated(ds *dataset.TDataSet, limit int64, implicit bool) {
	if ds == nil || !isImplicitlyTruncated(int64(ds.Count()), limit, implicit) {
		return
	}

	model := ""
	if self.Statement.Model != nil {
		model = self.Statement.Model.String()
	}
	log.Warnf("read %s returned exactly %d rows — the implicit LIMIT %d (orm.DefaultLimit) may have truncated the result. "+
		"Pass an explicit .Limit(n) to paginate, or .Limit(-1) to read all rows.",
		model, ds.Count(), limit)
}

// isImplicitlyTruncated 判断这次结果是否可能被**隐式**上限截断了。
// 抽成纯函数是为了可测——判据本身（"回来的行数正好顶到上限"）比日志格式重要。
func isImplicitlyTruncated(count, limit int64, implicit bool) bool {
	return implicit && limit > 0 && count >= limit
}

// guardUnscopedRead 拦下"一条定位条件都没有、又没给上限"的读取——那就是扫全表。
//
// 与 Delete/Write 那道 ErrUnsafe 守卫同源、同判据（hasCondition），只是挪到读侧：
// 一次写不出 WHERE 的 DELETE 会删光整张表，一次写不出 WHERE 的 SELECT 会拖垮整个库，
// 两者都该由调用方明确表态，而不是靠一个悄悄的 LIMIT 把后果盖住。
//
// 三种表态都放行：
//   - 显式 .Limit(n)：调用方自己划了范围（.Limit(-1) 也算——那是明确要全部）
//   - .AllowUnsafe()：与写侧同一个开关
//   - 任意 Ids/Where/Domain 条件
func (self *TSession) guardUnscopedRead() error {
	if self.allowUnsafe || self.Statement.LimitClause != 0 || self.hasCondition() {
		return nil
	}

	model := ""
	if self.Statement.Model != nil {
		model = self.Statement.Model.String()
	}
	return ormerr.New(ormerr.ErrUnsafe, fmt.Errorf(
		"unscoped read on %s: no Ids/Where/Domain and no Limit — this scans the whole table. "+
			"Add a condition, pass .Limit(n), or call .Limit(-1)/.AllowUnsafe() to read it all deliberately", model))
}
