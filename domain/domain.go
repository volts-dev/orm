package domain

import (
	"fmt"

	"github.com/volts-dev/orm/logger"
	"github.com/volts-dev/utils"
)

//TODO: domain 解析速度必须比Json序列化快
//TODO: support other encodings besides utf-8 (conversion before the lexer?)

const (
	// Domain operators.
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
	TERM_OPERATORS = []string{"=", "!=", "<=", "<", ">", ">=", "=?",
		"=like", "=ilike", "like", "not like", "ilike", "not ilike", "in", "not in", "child_of", "inselect", "not inselect",
		"=LIKE", "=ILIKE", "LIKE", "NOT LIKE", "ILIKE", "NOT ILIKE", "IN", "NOT IN", "CHILD_OF"}

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
		Value    interface{}
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

// domain 只负责where条件语句编写
func NewDomainNode(values ...interface{}) *TDomainNode {
	node := &TDomainNode{}

	// set the default option
	if values == nil {
		node.nodeType = VALUE_NODE
	}

	node.Push(values...)
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

func (self *TDomainNode) IN(name string, args ...interface{}) *TDomainNode {
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

	return utils.Itf2Str(self.Value)
}

func (self *TDomainNode) Clear() {
	self.nodeType = VALUE_NODE
	self.Value = nil
	self.children = nil
}

// 返回所有Items字符
func (self *TDomainNode) Strings(idx ...int) (result []string) {
	cnt := len(idx)

	if cnt == 0 { // 返回所有
		if len(self.children) == 0 {
			result = append(result, self.Value.(string))
		} else {
			for _, node := range self.children {
				result = append(result, node.Value.(string))
			}
		}

	} else if cnt == 1 {
		result = append(result, self.children[idx[0]].Value.(string))

	} else if cnt > 1 {
		for _, node := range self.children[idx[0]:idx[1]] {
			result = append(result, node.Value.(string))
		}
	}

	return
}

//栈方法Pop :取栈方式出栈<最前一个>元素 即最后一个添加进列的元素
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

//""" Pop a leaf to process. """
//栈方法Pop :取栈方式出栈<最后一个>元素 即最后一个添加进列的元素
func (self *TDomainNode) Pop() *TDomainNode {
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
func (self *TDomainNode) Push(items ...interface{}) *TDomainNode {
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
				}
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
		for _, node := range self.children[idx[0]:idx[1]] {
			result = append(result, node)
		}

		return result
	}

	return nil
}

//-----------list
func (self *TDomainNode) Item(idx int) *TDomainNode {
	if !self.IsValueNode() {
		if idx < len(self.children) {
			return self.children[idx]
		}
	}

	PrintDomain(self)
	logger.Panicf("bound index < %d >", idx)
	return nil
}

// TODO: 为避免错乱,移除后复制一个新的返回结果
func (self *TDomainNode) Remove(idx int) *TDomainNode {
	if self.nodeType != VALUE_NODE {
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
	return len(self.children)
}

// clone a new pointer of node
func (self *TDomainNode) Clone() *TDomainNode {
	node := NewDomainNode()
	node.Value = self.Value
	node.children = self.children
	node.nodeType = self.nodeType
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
		utils.InStrings(self.String(1), TERM_OPERATORS...) != -1
	if result {
		self.nodeType = LEAF_NODE
	}
	return result

}

// [XX,XX,XX]
func (self *TDomainNode) IsListNode() bool {
	return self.nodeType == LIST_NODE
}

// empty is have not value and children
func (self *TDomainNode) IsEmpty() bool {
	n := len(self.children)
	return n == 0 || (n == 0 && self.Value == nil)
}

func (self *TDomainNode) IsString() bool {
	_, ok := self.Value.(string)
	return ok
}

// 废弃 所有Item 都是String
func (self *TDomainNode) IsStringList() bool {
	for _, node := range self.children {
		if !node.IsString() {
			return false
		}
	}

	return true
}

// 是否是最简单 3项列表 {xx,xx,xx}
func (self *TDomainNode) IsIntLeaf() bool {
	for _, node := range self.children {
		if node.IsListNode() || !node.IsInt() {
			return false
		}
	}

	return true
}

/*""" Test whether an object is a valid domain operator. """*/
func (self *TDomainNode) IsDomainOperator() bool {
	return utils.InStrings(self.String(), DOMAIN_OPERATORS...) != -1
}

func (self *TDomainNode) IsTermOperator() bool {
	return utils.InStrings(self.String(), TERM_OPERATORS...) != -1
}

func (self *TDomainNode) ValueIn(strs ...interface{}) bool {
	for _, itr := range strs {
		switch itr.(type) {
		case string: // 处理字符串
			if self.String() == itr.(string) {
				return true
			}
		case *TDomainNode: // 处理*TStringList 类型
			if self.Value == itr.(*TDomainNode).Value {
				return true
			}

		}
	}

	return false
}

func (self *TDomainNode) IsInt() bool {
	_, ok := self.Value.(int)
	return ok
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
func (self *TDomainNode) Flatten() []interface{} {
	var lst []interface{}

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

func (self *TDomainNode) Insert(idx int, value interface{}) *TDomainNode {
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
