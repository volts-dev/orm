package orm

import (
	"fmt"
	"strings"

	"github.com/volts-dev/lexer"
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
	PrintToken       = false // print token
	DOMAIN_OPERATORS = []string{NOT_OPERATOR, OR_OPERATOR, AND_OPERATOR}
	//# Negation of domain expressions
	DOMAIN_OPERATORS_NEGATION = map[string]string{
		AND_OPERATOR: OR_OPERATOR,
		OR_OPERATOR:  AND_OPERATOR,
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
		"=like", "=ilike", "like", "not like", "ilike", "not ilike", "in", "not in", "child_of",
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
)

// ItemType identifies the type of lex Items.
type (
	TDomainParser struct {
		items []lexer.TToken
		Pos   int
		Count int
		isEnd bool
	}

	TDomainNode struct {
		Value    interface{}
		children []*TDomainNode
	}
)

func PrintDomain(list *TDomainNode) {
	fmt.Printf(
		"[Root]  Count:%d  Text:%v  IsList:%v ",
		list.Count(),
		list.String(),
		list.IsList(),
	)
	fmt.Println()

	printNodes(1, list)
	fmt.Println()
}

func printNodes(idx int, list *TDomainNode) {
	for i, Item := range list.Nodes() {
		for j := 0; j < idx; j++ { // 空格距离ss
			fmt.Print("  ")
		}
		if idx > 0 {
			fmt.Print("┗", "  ")
		}
		fmt.Printf(
			"[%d]  Count:%d  Text:%v  IsList:%v ",
			i,
			Item.Count(),
			Item.String(),
			Item.IsList(),
		)

		fmt.Println()
		printNodes(idx+1, Item)
	}
}

func NewDomainNode(value ...interface{}) *TDomainNode {
	node := &TDomainNode{}

	cnt := len(value)
	if cnt > 0 {
		if cnt == 1 {
			node.Value = value[0] // 创建空白
		} else {
			node.Push(value...)
		}
	}

	return node
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

	if self.IsList() {
		return parseDomain(self)
	}

	return utils.Itf2Str(self.Value)
}

func (self *TDomainNode) Clear() {
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

//栈方法Pop :取栈方式出栈<最后一个>元素 即最后一个添加进列的元素
func (self *TDomainNode) Shift() *TDomainNode {
	var lst *TDomainNode
	if len(self.children) > 0 {
		lst = self.children[0]
		self.children = self.children[1:]
	}

	// # 正式清空所有字符
	if len(self.children) == 0 {
		self.Value = nil
	}

	return lst
}

//""" Pop a leaf to process. """
//栈方法Pop :取栈方式出栈<最后一个>元素 即最后一个添加进列的元素
func (self *TDomainNode) Pop() *TDomainNode {
	cnt := len(self.children)
	if cnt == 0 {
		return nil
	}

	lst := self.children[cnt-1]
	self.children = self.children[:cnt-1]

	//# 正式清空所有字符 避免Push时添加Text为新item
	if len(self.children) == 0 {
		self.Value = nil
	}

	return lst
}

//栈方法Push：叠加元素
func (self *TDomainNode) Push(item ...interface{}) *TDomainNode {
	// 当Self是一个值时必须添加自己到items里成为列表的一部分
	if self.Value != nil && len(self.children) == 0 {
		self.children = append(self.children, NewDomainNode(self.Value))
		self.Value = nil
	}

	for _, node := range item {
		switch node.(type) {
		case *TDomainNode:
			self.children = append(self.children, node.(*TDomainNode))
		default:
			self.children = append(self.children, NewDomainNode(node))

		}
	}

	return self
}

// #push node in to it
func (self *TDomainNode) PushNode(nodes ...*TDomainNode) *TDomainNode {
	// 当Self是一个值时必须添加自己到items里成为列表的一部分
	if self.Value != nil && len(self.children) == 0 {
		self.children = append(self.children, NewDomainNode(self.Value))
	}

	for _, n := range nodes {
		self.children = append(self.children, n)
	}

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
	if self.IsList() {
		if idx < len(self.children) {
			return self.children[idx]
		}
	}

	logger.Panicf("bound idx %d", idx)
	return nil
}

// TODO: 为避免错乱,移除后复制一个新的返回结果
func (self *TDomainNode) Remove(idx int) *TDomainNode {
	self.children = append(self.children[:idx], self.children[idx+1:]...)

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

func (self *TDomainNode) IsList() bool {
	return len(self.children) > 0
}

// empty is have not value and children
func (self *TDomainNode) IsEmpty() bool {
	n := len(self.children)
	return n == 0 || (n == 0 && self.Value == nil)
}

// 所有Item 都是String
func (self *TDomainNode) IsSimpleLeaf() bool {
	for _, node := range self.children {
		if node.IsList() {
			return false
		}
	}

	return true
}

func (self *TDomainNode) IsString() bool {
	_, ok := self.Value.(string)
	return ok
}

// 所有Item 都是String
func (self *TDomainNode) IsStringLeaf() bool {
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
		if node.IsList() || !node.IsInt() {
			return false
		}
	}

	return true
}

func (self *TDomainNode) IsDomainOperator() bool {
	return utils.InStrings(self.String(), DOMAIN_OPERATORS...) != -1
}

func (self *TDomainNode) IsTermOperator() bool {
	return utils.InStrings(self.String(), TERM_OPERATORS...) != -1
}

func (self *TDomainNode) In(strs ...interface{}) bool {
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

func (self *TDomainNode) Insert(idx int, value interface{}) {
	var must_move bool = false
	var node *TDomainNode

	switch value.(type) {
	case *TDomainNode:
		node = value.(*TDomainNode)
		// Grow the slice by one element.
		// make([]Token, len(self.Child)+1)
		// self.Child[0 : len(self.Child)+1]
		self.children = append(self.children, node)
		// Use copy to move the upper part of the slice out of the way and open a hole.
		must_move = true
	default:
		node = NewDomainNode(value)
		self.children = append(self.children, node)
		must_move = true
	}

	if must_move {
		copy(self.children[idx+1:], self.children[idx:])
		// Store the new value.
		self.children[idx] = node
		// Return the result.
	}

	return
}

//主要-略过特殊字符移动
// 并返回不符合条件的Item
// 回退Pos 到空白Item处,保持下一个有效字符
func (self *TDomainParser) ConsumeWhitespace() (item lexer.TToken) {
	for {
		self.Next()
		if self.isEnd {
			break
		}
		//fmt.Println("consume_whitespace", self.Item().Val)
		switch self.Item().Type {
		case lexer.TokenWhitespace:
			continue
		default:
			item = self.Item()
			self.Backup()
			goto exit
		}
	}
exit:
	//fmt.Println("exit consume_whitespace", self.Item().Val)
	return
}

func (self *TDomainParser) Backup(cnt ...int) {
	count := 1
	if len(cnt) > 0 {
		count = cnt[0]
	}

	//fmt.Println("Backup", (self.Count-count) > 0)
	if (self.Count - count) > 0 {
		self.Pos = self.Pos - count
	}
}

func (self *TDomainParser) Next() {
	if self.Pos >= self.Count-1 { //如果大于Buf 则停止
		self.isEnd = true
		return
	}

	self.Pos++
}

func (self *TDomainParser) IsEnd() bool {
	return self.isEnd
}

func (self *TDomainParser) Item() lexer.TToken {
	return self.items[self.Pos]
}

func (self *TDomainParser) Find(i lexer.TToken) int {
	return 0
}

func NewDomainParser(sql string) *TDomainParser {
	lex, err := lexer.NewLexer(strings.NewReader(sql))
	if err != nil {
		logger.Err(err.Error())
	}

	parser := &TDomainParser{
		items: make([]lexer.TToken, 0),
		Pos:   0,
	}

	for {
		item, ok := <-lex.Tokens
		if !ok {
			break
		}

		// print token
		if PrintToken {
			fmt.Println(lexer.PrintToken(item))
		}

		parser.items = append(parser.items, item)
	}

	parser.Count = len(parser.items)
	return parser
}

// s Html文件流
/*
[('foo', '=', 'bar')]
foo = 'bar'

[('id', 'in', [1,2,3])]
id in (1, 2, 3)

[('field', '=', 'value'), ('field', '<>', 42)]
( field = 'value' AND field <> 42 )

[('&', ('field', '<', 'value'), ('field', '>', 'value'))]
( field < 'value' AND field > 'value' )

[('|', ('field', '=', 'value'), ('field', '=', 'value'))]
( field = 'value' OR field = 'value' )

[('&', ('field1', '=', 'value'), ('field2', '=', 'value'), ('|', ('field3', '<>', 'value'), ('field4', '=', 'value')))]
( field1 = 'value' AND field2 = 'value' AND ( field3 <> 'value' OR field4 = 'value' ) )

[('&', ('|', ('a', '=', 1), ('b', '=', 2)), ('|', ('c', '=', 3), ('d', '=', 4)))]
( ( a = 1 OR b = 2 ) AND ( c = 3 OR d = 4 ) )

[('|', (('a', '=', 1), ('b', '=', 2)), (('c', '=', 3), ('d', '=', 4)))]
( ( a = 1 AND b = 2 ) OR ( c = 3 AND d = 4 ) )
*/
// ['&', ('active', '=', True), ('value', '!=', 'foo')]
// ['|', ('active', '=', True), ('state', 'in', ['open', 'draft'])
// ['&', ('active', '=', True), '|', '!', ('state', '=', 'closed'), ('state', '=', 'draft')]
// ['|', '|', ('state', '=', 'open'), ('state', '=', 'closed'), ('state', '=', 'draft')]
// ['!', '&', '!', ('id', 'in', [42, 666]), ('active', '=', False)]
// ['!', ['=', 'company_id.name', ['&', ..., ...]]]
//	[('picking_id.picking_type_id.code', '=', 'incoming'), ('location_id.usage', '!=', 'internal'), ('location_dest_id.usage', '=', 'internal')]

func parseQuery(parser *TDomainParser, level int) (*TDomainNode, error) {
	result := NewDomainNode() // 存储临时列表 提供给AND
	list := NewDomainNode()   // 存储临时叶 提供给所有

	for !parser.IsEnd() {
		item := parser.Item()
		switch item.Type {
		case lexer.LPAREN, lexer.LBRACK:
			// 检测是否到尾部
			parser.Next()
			if parser.IsEnd() {
				goto exit
			}

			//开始列表采集 { xx,xx } 处理XX,XX进List
			new_leaf, err := parseQuery(parser, level+1)
			if err != nil {
				logger.Err(err)
			}

			// 将所有非 domain 操作符的列表平铺添加到主domain
			if new_leaf.IsList() && new_leaf.Item(0).IsDomainOperator() {
				result.PushNode(new_leaf.children...)
				//list = NewDomainNode()
				break
			} else {
				cnt := list.Count()
				if cnt == 3 && list.Item(1).IsTermOperator() {
					// 兼容 [('model', '=','%s'),('type', '=', '%s'), ('mode', '=', 'primary')]
					ls := NewDomainNode()
					ls.Push(list)
					ls.Push(new_leaf)

					list = ls
				} else if cnt > 0 {
					// 兼容In里的小列表("id" in ("a","b")
					list.Push(new_leaf)

				} else {
					list = new_leaf
				}
			}

		case lexer.RPAREN, lexer.RBRACK:
			goto exit

		case lexer.IDENT, lexer.HOLDER, lexer.STRING, lexer.NUMBER:
			value := strings.ToLower(item.Val)
			if utils.InStrings(value, "and", "or") != -1 {
				if value == "and" {
					result.Insert(0, AND_OPERATOR)
				} else if value == "or" {
					result.Insert(0, OR_OPERATOR)
				}

				// 过滤空list
				if !list.IsEmpty() {
					result.Push(list) // 已经完成了一个叶的采集 暂存列表里
				}
				list = NewDomainNode() // 新建一个列表继续采集
				break
			} else {
				list.Push(trimQuotes(item.Val))
				break
			}

		case lexer.OPERATOR, lexer.KEYWORD:
			list.Push(trimQuotes(item.Val))

		}

		parser.Next()
	}

exit:
	if result.Count() == 0 { // 当temp为0 时证明不是带Or And 的条件 直接返回List
		return list, nil
	} else {
		// 由于(XX=1 and XX='111') 已经把 and,XX=1, XX='111'当子项传给Temp 所以返回Temp
		if isLeaf(list) {
			result.Push(list)
		} else {
			// 已经完成了一个叶的采集 暂存列表里
			result.PushNode(list.children...) // 已经完成了一个叶的采集 暂存列表里
		}

		/*// 由于SQL where语句一般不包含[] 所以添加一层以抵消
		if level == 0 {
			shell := NewDomainNode()
			shell.Push(temp)
			return shell, nil
		}
		*/
		return result, nil // 结束该列表采集
	}
}

func trimQuotes(s string) string {
	s = strings.Trim(s, `'`)
	s = strings.Trim(s, `"`)
	return s
}

// transfer sql query to a domain node
func Query2Domain(qry string) (*TDomainNode, error) {
	parser := NewDomainParser(qry)
	return parseQuery(parser, 0)

	//fmt.Println("asdfa", res_domain.Flatten())

	// 确保Domain为List形态
	/*for {
		if res_domain.Count() == 1 {
			res_domain = res_domain.Item(0)
			continue
		}

		break
	}*/

}

//TODO 描述例句
// transfer string domain to a domain object
func String2Domain(domain string) (*TDomainNode, error) {
	parser := NewDomainParser(domain)
	return parseQuery(parser, 0)

	/*
		res_domain, err := parseQuery(parser, 0)
		if err != nil {
			return nil, err
		}

		if res_domain.Count() == 0 {
			return nil, fmt.Errorf("could not parse the string('%s') to domain", domain)
		}

		return res_domain.Item(0), nil
	*/
}

// transfer the domain object to string
func Domain2String(domain *TDomainNode) string {
	return parseDomain(domain)
}

// 更新生成所有Text内容
func parseDomain(node *TDomainNode) string {
	//STEP  如果是Value Object 不处理
	IsList := false
	if node.Count() == 0 {
		if node.Value != nil {
			return utils.Itf2Str(node.Value)
		}

		return ""

	} else {
		IsList = true
	}

	// 处理有Child的Object
	lStr := ""
	lStrLst := make([]string, 0)

	for _, item := range node.children {
		//fmt.Println("item", item.String())
		/*if isLeaf(item) {
			lStr = `(` + node.Quote(item.String(0)) + `, ` + node.Quote(item.String(1)) + `, `
			if item.Item(2).IsList() {
				lStr = lStr + `[` + item.String(2) + `])`
			} else {
				lStr = lStr + node.Quote(item.String(2)) + `)`
			}
			item.Text = lStr
			//item.SetText(lStr)
			fmt.Println("_update leaf", lStr)
			IsList = true
			lStrLst = append(lStrLst, item.Text)
		} else*/if item.IsList() {
			//utils.Dbg("IsList", item.text)
			str := parseDomain(item)
			lStrLst = append(lStrLst, str)
		} else {
			//fmt.Println("_update val", item.Text)
			//lStrLst = append(lStrLst, node.Quote(item.Text))
			lStrLst = append(lStrLst, item.String())
		}
	}

	/*	// 组合 XX,XX
		lStrLst := make([]string, 0)
		for _, item := range node.items {
			if item.IsList() {
				lStrLst = append(lStrLst, item.text)
			} else {
				lStrLst = append(lStrLst, self._quote(item.text))
			}
		}*/
	lStr = strings.Join(lStrLst, ",")

	// 组合[XX,XX]
	if isLeaf(node) {
		lStr = `(` + lStr + `)`
	} else if IsList {
		lStr = `[` + lStr + `]`
	}

	return lStr
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
func isLeaf(element *TDomainNode, internal ...bool) bool {
	INTERNAL_OPS := append(TERM_OPERATORS, "<>")

	if internal != nil && internal[0] {
		INTERNAL_OPS = append(INTERNAL_OPS, "inselect")
		INTERNAL_OPS = append(INTERNAL_OPS, "not inselect")
	}
	//??? 出现过==Nil还是继续执行之下的代码
	return (element != nil && element.Count() == 3) &&
		utils.InStrings(element.String(1), INTERNAL_OPS...) != -1 // 注意操作符确保伟小写
	//||utils.InStrings(Domain2String(element), TRUE_LEAF, FALSE_LEAF) != -1 // BUG

	/*
	   def isLeaf(element, internal=False):

	       INTERNAL_OPS = TERM_OPERATORS + ('<>',)
	       if internal:
	           INTERNAL_OPS += ('inselect', 'not inselect')
	       return (isinstance(element, tuple) or isinstance(element, list)) \
	           and len(element) == 3 \
	           and element[1] in INTERNAL_OPS \
	           and ((isinstance(element[0], basestring) and element[0]) or tuple(element) in (TRUE_LEAF, FALSE_LEAF))
	*/
}
