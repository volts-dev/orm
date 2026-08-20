package orm

import (
	"github.com/volts-dev/orm/domain"
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
		// Value 是右值的原始形态。注意它可能是 encoding/json.Number 之类的具名
		// 字符串类型，别直接 `.(string)` 断言，用 utils.ToString。
		Value any
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
