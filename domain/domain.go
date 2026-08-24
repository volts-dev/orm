package domain

import (
	"fmt"

	"github.com/volts-dev/utils"
	"github.com/volts-dev/volts/logger"
)

//TODO: domain 解析速度必须比Json序列化快
//TODO: support other encodings besides utf-8 (conversion before the lexer?)

const (
	// Domain operators.
	QUOTES       = `"`
	NOT_OPERATOR = "!"
	OR_OPERATOR  = "|"
	AND_OPERATOR = "&"

	TRUE_LEAF  = "(1, '=', 1)"
	FALSE_LEAF = "(0, '=', 1)"

	TRUE_DOMAIN  = "[" + TRUE_LEAF + "]"
	FALSE_DOMAIN = "[" + FALSE_LEAF + "]"
)

var (
	DOMAIN_OPERATORS = []string{NOT_OPERATOR, OR_OPERATOR, AND_OPERATOR}
	//# Negation of domain expressions
	DOMAIN_OPERATORS_NEGATION = map[string]string{
		AND_OPERATOR: OR_OPERATOR,
		OR_OPERATOR:  AND_OPERATOR,
	}

	DOMAIN_OPERATORS_KEYWORDS = map[string]string{
		OR_OPERATOR:  " OR ",
		AND_OPERATOR: " AND ",
	}

	/*# List of available term operators. It is also possible to use the '<>'
	# operator, which is strictly the same as '!='; the later should be prefered
	# for consistency. This list doesn't contain '<>' as it is simpified to '!='
	# by the normalize_operator() function (so later part of the code deals with
	# only one representation).
	# Internals (i.e. not available to the user) 'inselect' and 'not inselect'
	# operators are also used. In this case its right operand has the form (subselect, params).
	*/
	// parent_of 与 child_of 一样在 expr.go 的 HIERARCHY_FUNCS 里注册着，却一直没登记
	// 到这里。后果不是"不支持"这么干净：IsLeafNode() 认不出这个操作符 → 三元组不被
	// 当成叶子 → normalize_domain 的 FlattenNode 把它**拆成三个平级项**，最后以
	// `invalid domain leaf` / `Invalid field <id>` 之类文不对题的错误收场。
	TERM_OPERATORS = []string{"=", "!=", "<=", "<", ">", ">=", "=?",
		"=like", "=ilike", "like", "not like", "ilike", "not ilike", "in", "not in", "child_of", "parent_of", "inselect", "not inselect",
		"=LIKE", "=ILIKE", "LIKE", "NOT LIKE", "ILIKE", "NOT ILIKE", "IN", "NOT IN", "CHILD_OF", "PARENT_OF"}

	TERM_OPERATORS_NEGATION = map[string]string{
		"<":         ">=",
		">":         "<=",
		"<=":        ">",
		">=":        "<",
		"=":         "!=",
		"!=":        "=",
		"in":        "not in",
		"like":      "not like",
		"ilike":     "not ilike",
		"not in":    "in",
		"not like":  "like",
		"not ilike": "ilike",
		"IN":        "NOT IN",
		"LIKE":      "NOT LIKE",
		"ILIKE":     "NOT ILIKE",
		"NOT IN":    "IN",
		"NOT LIKE":  "LIKE",
		"NOT ILIKE": "ILIKE",
	}

	NEGATIVE_TERM_OPERATORS = []string{"!=", "not like", "not ilike", "not in"}

	NODE_NAME = map[NodeType]string{
		VALUE_NODE: "VALUE_NODE",
		LEAF_NODE:  "LEAF_NODE",
		LIST_NODE:  "LIST_NODE",
	}
)

const (
	VALUE_NODE NodeType = iota // 纯值
	LEAF_NODE                  // 单项域带操作符
	LIST_NODE                  // 纯列表不带操作符
	//DOMAIN_NODE                 // 多项域带操作符
)

type (
	NodeType int

	TDomainNode struct {
		nodeType NodeType // 不可外部修改
		Value    any
		children []*TDomainNode
	}
)

func PrintDomain(node *TDomainNode) {
	fmt.Printf(
		"[Root]  Count:%d  Text:%v  Type:%v ",
		node.Count(),
		node.String(),
		NODE_NAME[node.nodeType],
	)
	fmt.Println()

	printNodes(1, node)
	fmt.Println()
}

func printNodes(idx int, node *TDomainNode) {
	for i, Item := range node.Nodes() {
		for j := 0; j < idx; j++ { // 空格距离ss
			fmt.Print("  ")
		}
		if idx > 0 {
			fmt.Print("┗", "  ")
		}
		fmt.Printf(
			"[%d]  Count:%d  Text:%v  Type:%v ",
			i,
			Item.Count(),
			Item.String(),
			NODE_NAME[Item.nodeType],
		)

		fmt.Println()
		printNodes(idx+1, Item)
	}
}

// TODO 考虑废除 不简便易混要
// domain 只负责where条件语句编写
func NewDomainNode(values ...any) *TDomainNode {
	node := &TDomainNode{}

	// set the default option
	if len(values) == 0 {
		node.nodeType = VALUE_NODE
	}

	node.Push(values...)
	return node
}

func New(field any, operators any, values ...any) *TDomainNode {
	node := &TDomainNode{
		nodeType: LEAF_NODE,
	}
	node.Push(field)
	node.Push(operators)
	// values 必须落成 leaf 的第三个孩子(且仅这一个),不能被 Push 拍平成 N 个平级
	// 孩子——否则 IsLeafNode() 的 len(children)==3 判断失效,该 leaf 后续被
	// .AND()/.OR() 或 TStatement.Op() 里的 self.domain.OP() 合并时会被
	// OP() 误当成"没有逻辑前缀的隐式 AND 列表"处理,插入多余的 "&" 前缀节点,
	// 产出结构性错误但不报错的 domain 树(normalize_domain 的 arity 校验对
	// 未知 token 按 0 记账,恰好也不会报错)。单值场景(len==1)历史上一直是
	// "3 孩子" 的巧合正确形状,原样保留,不改变其行为。
	switch len(values) {
	case 0:
	case 1:
		node.Push(values[0])
	default:
		node.Push(NewDomainNode(values...))
	}
	return node
}

// domain节点添加逻辑规则
func (self *TDomainNode) OP(op string, node *TDomainNode) *TDomainNode {
	if self.IsValueNode() {
		*self = *node // copy pointer
	} else {
		if self.IsLeafNode() {
			if node.IsLeafNode() {
				// 合并两个Leaf
				cond := NewDomainNode()
				cond.Insert(0, op)      // 添加操作符
				cond.Push(self.Clone()) // 第一条件
				cond.Push(node)         // 第二条件
				*self = *cond
			} else {
				cond := NewDomainNode()
				cond.Insert(0, op)      // 添加操作符
				cond.Push(self.Clone()) // 第一条件
				cond.Merge(node)        // 第二条件
				*self = *cond
			}

		} else if self.Item(0).IsDomainOperator() {
			if node.IsLeafNode() {
				// 添加单叶新条件
				self.Insert(0, op) // 添加操作符
				self.Push(node)    // 第二条件
			} else {
				// 添加多叶新条件
				self.Insert(0, op) // 添加操作符
				self.Merge(node)   // 第二条件
			}
		} else {
			// self 是一个没有逻辑操作符前缀的列表(隐式 AND),
			// 例如 [["id","in",[...]]] 解析出来的 [leaf],或多条件 [leaf1, leaf2]。
			// 先归一化成合法 domain 再重试 OP,而不是 panic。
			if self.Count() == 1 {
				// 单条件被包了一层列表 -> 解包当作该条件处理
				*self = *self.Item(0).Clone()
			} else {
				// 多条件隐式 AND -> 把 '&' 显式补到前面(n 个条件需 n-1 个 '&')。
				// 注意: Insert 会增加 Count(), 必须先固定循环次数避免死循环。
				for i, n := 0, self.Count()-1; i < n; i++ {
					self.Insert(0, AND_OPERATOR)
				}
			}
			return self.OP(op, node)
		}
	}

	return self
}

func (self *TDomainNode) Type() NodeType {
	return self.nodeType
}

func (self *TDomainNode) AND(nodes ...*TDomainNode) *TDomainNode {
	for _, node := range nodes {
		self.OP(AND_OPERATOR, node)
	}

	return self
}

func (self *TDomainNode) OR(nodes ...*TDomainNode) *TDomainNode {
	for _, node := range nodes {
		self.OP(OR_OPERATOR, node)
	}

	return self
}

func (self *TDomainNode) IN(name string, args ...any) *TDomainNode {
	if len(args) == 0 {
		// TODO report err stack
		return self
	}

	cond := NewDomainNode()
	cond.Push(name)
	cond.Push("IN")
	cond.Push(NewDomainNode(args...))
	cond.nodeType = LEAF_NODE
	self.OP(AND_OPERATOR, cond)

	return self
}

func (self *TDomainNode) NotIn(name string, args ...any) *TDomainNode {
	if len(args) == 0 {
		// TODO report err stack
		return self
	}

	cond := NewDomainNode()
	cond.Push(name)
	cond.Push("NOT IN")
	cond.Push(NewDomainNode(args...))
	cond.nodeType = LEAF_NODE
	self.OP(AND_OPERATOR, cond)

	return self
}

// parse node or subnode to string. it will panice when the data is not available
// return 'xx','xx'
// 当self 时列表时idx有效,反则返回Text
func (self *TDomainNode) String(idx ...int) string {
	if len(idx) > 0 {
		cnt := len(self.children)
		i := idx[0]
		if cnt > 0 {
			if i > -1 && i < cnt {
				return parseDomain(self.children[i])
			} else {
				logger.Panicf("bounding idx %d", idx)
			}
		}
	}

	if !self.IsValueNode() {
		return parseDomain(self)
	}

	return utils.ToString(self.Value)
}

func (self *TDomainNode) Clear() {
	self.nodeType = VALUE_NODE
	self.Value = nil
	self.children = nil
}

// 返回所有Items字符
//
// ★ 原来一律用 `self.Value.(string)` 硬断言：值是数字/布尔就直接 panic，而调用点
// 恰恰包括 leaf_to_sql 里 "Invalid operator/field" 的**错误日志**——报错本身把进程
// 打崩。下标也没有任何范围检查。一律改为安全取值。
func (self *TDomainNode) Strings(idx ...int) (result []string) {
	toStr := func(n *TDomainNode) string {
		if n == nil {
			return ""
		}
		return utils.ToString(n.Value)
	}

	cnt := len(idx)
	switch {
	case cnt == 0: // 返回所有
		if len(self.children) == 0 {
			result = append(result, utils.ToString(self.Value))
		} else {
			for _, node := range self.children {
				result = append(result, toStr(node))
			}
		}

	case cnt == 1:
		if idx[0] >= 0 && idx[0] < len(self.children) {
			result = append(result, toStr(self.children[idx[0]]))
		}

	default:
		lo, hi := idx[0], idx[1]
		if lo < 0 {
			lo = 0
		}
		if hi > len(self.children) {
			hi = len(self.children)
		}
		for i := lo; i < hi; i++ {
			result = append(result, toStr(self.children[i]))
		}
	}

	return
}

// 栈方法Pop :取栈方式出栈<最前一个>元素 即最后一个添加进列的元素
func (self *TDomainNode) Shift() *TDomainNode {
	var node *TDomainNode
	if len(self.children) > 0 {
		node = self.children[0]
		self.children = self.children[1:]
	}

	// # 正式清空所有字符
	if len(self.children) == 0 {
		self.Value = nil
		self.nodeType = VALUE_NODE
	}

	return node
}

// """ Pop a leaf to process. """
// 栈方法Pop :取栈方式出栈<最后一个>元素 即最后一个添加进列的元素
func (self *TDomainNode) Pop() *TDomainNode {
	// ★ 单元素的栈是 VALUE_NODE 而不是 LIST_NODE(Push 到空节点只写 Value，
	//   第二次 Push 才转成列表)。这里原来只认 LIST_NODE，于是"栈里正好剩一个"
	//   时 Pop() 返回 nil —— 调用方 `stack.Pop().String()` 直接空指针崩溃
	//   (expr.go toSql 的 '!' 分支，真栈实测 SIGSEGV)。
	//   Push/Pop 必须对称：值节点也要能出栈。
	if self.nodeType == VALUE_NODE {
		if self.Value == nil {
			return nil
		}
		one := NewDomainNode(self.Value)
		self.Value = nil
		return one
	}

	if self.nodeType == LIST_NODE {
		cnt := len(self.children)
		if cnt == 0 {
			return nil
		}

		one := self.children[cnt-1]
		self.children = self.children[:cnt-1]

		//# 正式清空所有字符 避免Push时添加Text为新item
		if len(self.children) == 0 {
			self.Value = nil
			self.nodeType = VALUE_NODE
		}

		return one
	}

	return nil
}

// PUSH只完成节点添加,不涉及到domain逻辑结构规则
// 栈方法Push：叠加元素
// 推入的值除TDomainNode类型外一律存储Value值
// 所有推入的数据都使得节点成为LIST_NODE节点
func (self *TDomainNode) Push(items ...any) *TDomainNode {
	for _, node := range items {
		if n, ok := node.(*TDomainNode); ok {
			if self.nodeType == VALUE_NODE && self.Value != nil {
				self.children = append(self.children, NewDomainNode(self.Value))
				self.Value = nil
			}
			self.children = append(self.children, n)
			self.nodeType = LIST_NODE
		} else {
			if self.nodeType == VALUE_NODE && self.Value == nil {
				self.Value = node
			} else {
				if self.nodeType == VALUE_NODE && self.Value != nil {
					// VALUE_NODE 转 LIST_NODE 当Self是一个值时必须添加自己到items里成为列表的一部分
					self.children = append(self.children, NewDomainNode(self.Value))
					self.Value = nil
				} /*else if self.nodeType == LEAF_NODE {
					if len(self.children) < 3 {
						self.children = append(self.children, NewDomainNode(node))
						continue
					}
					self.children[2].children = append(self.children[2].children, NewDomainNode(node))
					self.children[2].nodeType = LIST_NODE
				} */
				self.children = append(self.children, NewDomainNode(node))
				self.nodeType = LIST_NODE

			}
		}
	}

	return self
}

// merge node's children in to it
func (self *TDomainNode) Merge(node *TDomainNode) *TDomainNode {
	// VALUE_NODE 转 LIST_NODE
	if self.nodeType == VALUE_NODE {
		self.children = append(self.children, NewDomainNode(self.Value))
		self.Value = nil
		self.nodeType = LIST_NODE
	}

	if node.nodeType == VALUE_NODE {
		self.children = append(self.children, NewDomainNode(node.Value))
	} else {
		self.children = append(self.children, node.children...)
	}
	self.nodeType = LIST_NODE

	return self
}

// len(idx)==0:返回所有
// len(idx)==1:返回Idx 指定item
// len(idx)>1:返回Slice 范围的items
func (self *TDomainNode) Nodes(idx ...int) []*TDomainNode {
	cnt := len(idx)
	if cnt == 0 {
		return self.children // 返回所有
	} else if cnt == 1 && idx[0] < self.Count() { // idex 必须小于Self长度
		result := make([]*TDomainNode, 0)
		result = append(result, self.children[idx[0]])

		return result
	} else if cnt > 1 && idx[0] < self.Count() && idx[1] < self.Count() {
		result := make([]*TDomainNode, 0)
		result = append(result, self.children[idx[0]:idx[1]]...)
		return result
	}

	return nil
}

// -----------list
func (self *TDomainNode) Item(idx int) *TDomainNode {
	if !self.IsValueNode() {
		// idx >= 0：原来只判了上界，负下标一路走到 children[-1] 直接 panic，
		// 而 Panicf 那条带上下文的报错反而走不到。
		if idx >= 0 && idx < len(self.children) {
			return self.children[idx]
		}
	}

	PrintDomain(self)
	logger.Panicf("bound index < %d >", idx)
	return nil
}

// TODO: 为避免错乱,移除后复制一个新的返回结果
func (self *TDomainNode) Remove(idx int) *TDomainNode {
	// 越界直接返回，不 panic（原来无任何下标检查）。
	if self.nodeType != VALUE_NODE && idx >= 0 && idx < len(self.children) {
		self.children = append(self.children[:idx], self.children[idx+1:]...)
	}

	return self
}

// 复制一个反转版
func (self *TDomainNode) Reversed() *TDomainNode {
	result := NewDomainNode()
	cnt := self.Count()
	for i := cnt - 1; i >= 0; i-- {
		result.Push(self.children[i]) //TODO: 复制
	}

	return result
}

// return the list length
func (self *TDomainNode) Count() int {
	if self == nil {
		return 0
	}
	return len(self.children)
}

// Clone 深拷贝一棵子树。
//
// ★ 原来是**浅拷贝**：`node.children = self.children` 直接共享同一个切片。
// 之后在克隆体上做 Remove/Insert 会就地移动元素，把原树一起改坏——
//
//	a := ["x","y","z"]; b := a.Clone(); b.Remove(0)
//	=> a 变成 ["y","z","z"]
//
// OP()/field_searcher/expr 里到处在 Clone 之后继续加工，这是个定时炸弹。
// 注意 Value 里若装着可变对象(如 []any)仍是按引用复制——domain 的值语义只到标量。
func (self *TDomainNode) Clone() *TDomainNode {
	if self == nil {
		return nil
	}
	node := &TDomainNode{
		nodeType: self.nodeType,
		Value:    self.Value,
	}
	if len(self.children) > 0 {
		node.children = make([]*TDomainNode, len(self.children))
		for i, child := range self.children {
			node.children[i] = child.Clone()
		}
	}
	return node
}

func (self *TDomainNode) IsValueNode() bool {
	return self.nodeType == VALUE_NODE
}

/*""" Test whether an object is a valid domain term:
    - is a list or tuple
    - with 3 elements
    - second element if a valid op

    :param tuple element: a leaf in form (left, operator, right)
    :param boolean internal: allow or not the 'inselect' internal operator
        in the term. This should be always left to False.

    Note: OLD TODO change the share wizard to use this function.
"""*/
// [field,op,value]
func (self *TDomainNode) IsLeafNode() bool {
	if self.nodeType == LEAF_NODE {
		return true
	}

	result := self.nodeType == LIST_NODE && len(self.children) == 3 &&
		utils.IndexOf(self.String(1), TERM_OPERATORS...) != -1
	if result {
		self.nodeType = LEAF_NODE
	}
	return result

}

// [XX,XX,XX]
func (self *TDomainNode) IsListNode() bool {
	return self.nodeType == LIST_NODE
}

// IsTrueLeaf/IsFalseLeaf 判断一个叶子是不是恒真/恒假常量。
//
// ★ 必须**按结构**判，不能拿 Domain2String() 的输出去和 TRUE_LEAF/FALSE_LEAF
// 常量比字符串：常量写的是 `(1, '=', 1)`（带空格、单引号），而 Domain2String 出的是
// `(1,"=",1)`，两者**永远不相等**。于是 expr_leaf.go 的 is_true_leaf/is_false_leaf
// 和 leaf_to_sql 里的两处比较全是恒 false —— 把 TRUE_LEAF 压回栈的那条路径
// (expr.go 非存储字段分支) 最终以 `Invalid field <1>` 报错收场，恒真/恒假这套
// 机制整个不能用。
func (self *TDomainNode) IsTrueLeaf() bool {
	return self.isConstLeaf("1")
}

func (self *TDomainNode) IsFalseLeaf() bool {
	return self.isConstLeaf("0")
}

// isConstLeaf 匹配 (left, '=', 1) 形状，left 为 "1"(恒真) 或 "0"(恒假)。
// 值可能是 int 也可能是 string（取决于是解析出来的还是手工搭的），一律按文本比。
func (self *TDomainNode) isConstLeaf(left string) bool {
	if self == nil || !self.IsLeafNode() || len(self.children) != 3 {
		return false
	}
	return self.children[0].String() == left &&
		self.children[1].String() == "=" &&
		self.children[2].String() == "1"
}

// empty is have not value and children
//
// ★ 原来写的是 `n == 0 || (n == 0 && self.Value == nil)`——后半永远被前半短路，
// 等价于只判 `n == 0`，于是"有值但没孩子"的值节点被判成空。
func (self *TDomainNode) IsEmpty() bool {
	return len(self.children) == 0 && self.Value == nil
}

func (self *TDomainNode) IsString() bool {
	_, ok := self.Value.(string)
	return ok
}

// 废弃 所有Item 都是String
//
// 空集合返回 false：这两个谓词都是"整列都是某类型"的判断，调用方(expr.to_ids)拿它
// 分流，空集合上返回**真空真**会把空列表同时判成"全是字符串"和"全是数字"。
func (self *TDomainNode) IsStringList() bool {
	if len(self.children) == 0 {
		return false
	}
	for _, node := range self.children {
		if !node.IsString() {
			return false
		}
	}

	return true
}

// 是否是最简单 3项列表 {xx,xx,xx}
func (self *TDomainNode) IsIntLeaf() bool {
	if len(self.children) == 0 {
		return false
	}
	for _, node := range self.children {
		if node.IsListNode() || !node.IsNumeric() {
			return false
		}
	}

	return true
}

/*""" Test whether an object is a valid domain operator. """*/
func (self *TDomainNode) IsDomainOperator() bool {
	return utils.IndexOf(self.String(), DOMAIN_OPERATORS...) != -1
}

func (self *TDomainNode) IsTermOperator() bool {
	return utils.IndexOf(self.String(), TERM_OPERATORS...) != -1
}

func (self *TDomainNode) ValueIn(strs ...any) bool {
	for _, itr := range strs {
		switch v := itr.(type) {
		case string: // 处理字符串
			if self.String() == v {
				return true
			}
		case *TDomainNode: // 处理*TStringList 类型
			if self.Value == v.Value {
				return true
			}

		}
	}

	return false
}

func (self *TDomainNode) IsNumeric() bool {
	switch self.Value.(type) {
	case int, uint, int16, uint16, int32, uint32, int64, uint64, float32, float64, bool:
		return true
	}
	return false
}

/*"Flatten a list of elements into a uniqu list
Author: Christophe Simonis (christophe@tinyerp.com)

Examples::
>>> flatten(['a'])
['a']
>>> flatten('b')
['b']
>>> flatten( [] )
[]
>>> flatten( [[], [[]]] )
[]
>>> flatten( [[['a','b'], 'c'], 'd', ['e', [], 'f']] )
['a', 'b', 'c', 'd', 'e', 'f']
>>> t = (1,2,(3,), [4, 5, [6, [7], (8, 9), ([10, 11, (12, 13)]), [14, [], (15,)], []]])
>>> flatten(t)
[1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15]
"*/
// 返回列表中的所有值
func (self *TDomainNode) Flatten() []any {
	var lst []any

	// # 当StringList作为一个单字符串
	if self.Value != nil && len(self.children) == 0 {
		lst = append(lst, self.Value)
		return lst
	}

	// # 当StringList作为字符串组
	for _, node := range self.children {
		if len(node.children) > 0 {
			for _, n := range node.children {
				lst = append(lst, n.Value)
			}
		} else {
			lst = append(lst, node.Value)
		}
	}

	return lst
}

func (self *TDomainNode) FlattenNode() *TDomainNode {
	if self == nil {
		return nil
	}
	if self.IsValueNode() || self.IsLeafNode() {
		return self
	}

	flat := NewDomainNode()
	for _, child := range self.Nodes() {
		if child.IsListNode() && !child.IsLeafNode() {
			flatChildren := child.FlattenNode()
			flat.Merge(flatChildren)
		} else {
			flat.Push(child)
		}
	}
	return flat
}

func (self *TDomainNode) Insert(idx int, value any) *TDomainNode {
	if self.nodeType == VALUE_NODE && self.Value != nil {
		self.children = append(self.children, NewDomainNode(self.Value))
		self.Value = nil
	}

	// Use copy to move the upper part of the slice out of the way and open a hole.
	var node *TDomainNode
	var ok bool
	if node, ok = value.(*TDomainNode); ok {
		self.children = append(self.children, node)
	} else {
		node = NewDomainNode(value)
		self.children = append(self.children, node)
	}

	// 下标夹到 [0, len-1]。原来无任何检查，idx 超界时下面那句 copy 直接
	// `slice bounds out of range` panic。
	if idx < 0 {
		idx = 0
	}
	if idx > len(self.children)-1 {
		idx = len(self.children) - 1
	}

	// 位移
	copy(self.children[idx+1:], self.children[idx:])
	self.children[idx] = node // Store the new value.

	self.nodeType = LIST_NODE
	/*
		if self.nodeType == VALUE_NODE && self.Value == nil {
			self.Value = value
		} else {
			var node *TDomainNode
			if self.nodeType == VALUE_NODE && self.Value != nil {
				self.children = append(self.children, NewDomainNode(self.Value))
				self.Value = nil
			}

			// Use copy to move the upper part of the slice out of the way and open a hole.
			must_move := false
			ok := false
			if node, ok = value.(*TDomainNode); ok {
				// Grow the slice by one element.
				// make([]Token, len(self.Child)+1)
				// self.Child[0 : len(self.Child)+1]
				self.children = append(self.children, node)
				must_move = true
			} else {
				node = NewDomainNode(value)
				self.children = append(self.children, node)
				must_move = true
			}

			if must_move {
				copy(self.children[idx+1:], self.children[idx:])
				self.children[idx] = node // Store the new value.
			}
			self.nodeType = LIST_NODE
		}
	*/
	return self
}
