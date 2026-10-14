package orm

import (
	"context"

	"github.com/volts-dev/orm/domain"
)

// 「关联标签读取」标记：多对一字段为了显示「这条关联记录叫什么」而对关联模型发起的子读取
// （ManyToOne 的 classic / UseNameGet 两支）。
//
// orm 自己不做任何授权判断，只负责**如实告诉宿主**「这一次读是标签读」：宿主（vectors 的
// routeSchema）据此决定能否对调用方本无权直接读的模型放行**仅限标签列**的读取——对齐 Odoo
// `Many2one.convert_to_read` 里的 `value.sudo().display_name`：能读父记录，就能看见它关联的
// 那条记录叫什么。放行几列由宿主按 Statement.Fields 判定，orm 不替它决定。
//
// 标记挂在 context 上而不是会话字段上：NameGet 走 Clone()→Tx()→Records()，每一步都新建会话，
// 只有模型的 context 会被一路带过去（Records 把 options.Context 装进新会话）。
type labelReadKey struct{}

// WithLabelRead 返回带「关联标签读取」标记的 context。nil 视作 context.Background()。
func WithLabelRead(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, labelReadKey{}, true)
}

// IsLabelReadContext 报告 ctx 是否带「关联标签读取」标记。
func IsLabelReadContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(labelReadKey{}).(bool)
	return v
}

// IsLabelRead 报告这个会话是否为关联标签读取（见 WithLabelRead）。
func (self *TSession) IsLabelRead() bool {
	return IsLabelReadContext(self.context)
}

// SelectedFields 返回本次操作点名的列（Select(...)）；空 = 未点名（全部列）。
// 返回副本，调用方改它不影响语句。
func (self *TSession) SelectedFields() []string {
	return append([]string(nil), self.Statement.Fields...)
}

// DomainSnapshot 返回本次语句上**已累积**的条件（Where()/Domain()/In() 都在里面）的一份副本；
// 改它不影响语句。没有条件时返回空节点（Count()==0），不返回 nil。
//
// 给宿主在 BeforeSession 里**看**调用方按哪些列筛选用的，不是给它改条件的：比如只许
// 按公开列筛选的回落读取（vectors 的 hr.employee 对非 HR 用户回落到公开字段，按私有列
// 筛选等于把私有列当成查询预言机——条件不进结果，但"有没有这一行"就是答案）。
// 按 id 点名的 Ids() 不在这里（那是 Statement.IdParam，读时才并进条件）。
func (self *TSession) DomainSnapshot() *domain.TDomainNode {
	if self.Statement.domain == nil {
		return domain.NewDomainNode()
	}
	return self.Statement.domain.Clone()
}
