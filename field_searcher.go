package orm

import (
	"github.com/volts-dev/orm/domain"
	"github.com/volts-dev/utils"
)

/*
非存储字段的 search 钩子（对标 Odoo `fields.Char(compute=..., search='_search_xxx')`）。

在此之前本仓**没有**这个能力：`TField.searchOnSelf` 声明了却全仓从未被赋过值，恒 false，
于是 expr.go 里每一个非存储字段的叶子都走"生成空节点、什么都不 push"那条路——条件消失、
整表返回。计算列因此只能看、不能筛，而且筛了不报错。

有了钩子之后，字段自己负责把 `(自己, 操作符, 右值)` 翻译成一条**由存储列构成**的 domain：

	b.VarcharField("name").Store(false).
		Getter(self._computeName).
		Searcher(func(ctx *orm.TFieldSearchContext) (*domain.TDomainNode, error) {
			// name 是 attribute_value_id.name 的投影，于是照原样转给那条路径
			return domain.New("attribute_value_id."+ctx.Field.Name(), ctx.Operator, ctx.Value), nil
		})

三条约定：

  - **返回 nil 表示"一条都不匹配"**，不是"不筛"。expr 会把它落成恒假条件。
    想表达"不筛"必须显式返回 TRUE_LEAF——让这件事必须写出来，是因为反过来的默认值
    正是这个 bug 的成因。
  - 返回值可以是单个叶子，也可以是完整 domain（含 `|`/`!`），expr 会跑一遍
    normalize_domain 补上隐式的 `&`。少了它，两个叶子会在 toSql 的栈上各留一个，
    生成结构性错误的 SQL。
  - 翻译结果里**不能再出现本字段自己**，否则无限递归。
*/

type (
	// TFieldSearchContext 是 search 钩子的入参。
	TFieldSearchContext struct {
		Model IModel
		Field IField
		// Session 是发起本次查询的会话。要在钩子里再查一次库时必须用它派生
		// （Clone），否则丢掉 schema 与事务——schema 隔离租户下会查到别的库去。
		Session *TSession
		// Operator 已经过 normalize_leaf 归一（小写、`<>`→`!=`）。
		Operator string
		// Value 是右值的原始形态。两个坑：
		//   - 它可能是 encoding/json.Number 之类的**具名字符串类型**，别直接
		//     `.(string)` 断言，用 utils.ToString。
		//   - 右值是**列表**时（`x in [a,b]`）它是 nil ——列表挂在节点的孩子上。
		//     要照顾多值就用 Right。
		Value any
		// Right 是右值节点本身，多值场景（in / not in）必须用它。
		// 转发给别的字段时直接 Right.Clone() 作为新叶子的第三个孩子。
		Right *domain.TDomainNode
		// Leaf 是原始叶子，需要比 Operator/Value 更精细的信息时用（如右值是列表）。
		Leaf *domain.TDomainNode
	}

	// FieldSearchFunc 把一条落在非存储字段上的叶子翻译成由存储列构成的 domain。
	FieldSearchFunc func(ctx *TFieldSearchContext) (*domain.TDomainNode, error)
)

// Searcher 返回字段的 search 钩子，没有则 nil。
func (self *TField) Searcher() FieldSearchFunc { return self.searchFunc }

// SetSearcher 挂上 search 钩子。挂了之后该字段即视为可搜（SearchOnSelf 为真）。
func (self *TField) SetSearcher(fn FieldSearchFunc) { self.searchFunc = fn }

// IsNegative 报告操作符是否是否定型（`!=` / `not like` / `not ilike` / `not in`）。
func (self *TFieldSearchContext) IsNegative() bool {
	return utils.IndexOf(self.Operator, domain.NEGATIVE_TERM_OPERATORS...) != -1
}

// Forward 把本字段的叶子原样转发到另一条路径上，操作符与右值都不变。
//
// 用它而不是手写 domain.New(path, ctx.Operator, ctx.Value)：右值是列表时
// （`x in [a,b]`）ctx.Value 是 nil，手写那句拼出来的条件恒不匹配——"多选几个"
// 反而一条都筛不出来。
func (self *TFieldSearchContext) Forward(path string) *domain.TDomainNode {
	node := domain.NewDomainNode()
	node.Push(path)
	node.Push(self.Operator)
	if self.Right != nil {
		node.Push(self.Right.Clone())
	} else {
		node.Push(self.Value)
	}
	return node
}

// Any 把本字段转发到多条路径并合并——计算列由几段拼成时（"[编码] 模板名 (属性值)"）
// 就是这个形状。
//
// ★ 否定型操作符用 **AND** 合并，不是 OR。"名字里不含 X" 意思是**每一条**来源都不含
// X；写成 OR 的话，只要有一条来源不含就算命中，几乎等于不筛——而这正是本类 bug 最爱
// 的伪装：条件写了、结果几乎全回来了、没有任何报错。（德摩根：!(a|b) == !a & !b）
func (self *TFieldSearchContext) Any(paths ...string) *domain.TDomainNode {
	if len(paths) == 0 {
		return nil
	}
	node := self.Forward(paths[0])
	for _, p := range paths[1:] {
		if self.IsNegative() {
			node.AND(self.Forward(p))
		} else {
			node.OR(self.Forward(p))
		}
	}
	return node
}
