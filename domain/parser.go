package domain

import (
	"strconv"
	"strings"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/lexer"
	"github.com/volts-dev/utils"
	"github.com/volts-dev/volts/logger"
)

type (
	TDomainParser struct {
		items []lexer.TToken
		Pos   int
		Count int
		isEnd bool
	}
)

var printToken bool = false // print token

func trimQuotes(s string) string {
	//s = strings.Trim(s, `'`)
	s = strings.Trim(s, `"`)
	return s
}

func Quote(s string) string {
	return strconv.Quote(s)
}

func Unquote(s string) string {
	s, _ = strconv.Unquote(`"` + s + `"`)
	return s
}

// relScalar 把数据集里取到的**关系值**归一成能进 SQL 的裸标量。
//
// 域里引用一个 many2one 列时（`[('attribute_id','=',attribute_id)]`），变量的值
// 是从当前数据集取的。而经典读之后那一列的值已经不是 id 了，是
// `{id, name, display_name}` 映射（name_get 路径下则是 `[id, 名称]` 元组）。映射
// 原样塞进域会一路走到驱动层：
//
//	sql: converting argument $1 type: unsupported type map[string]interface {}, a map
//
// 而调用方**看不到这个错误**——字段的 OnRead 拿到 error 就提前返回，SetByField 没
// 执行，于是整个关系字段的键从结果里消失（不是空数组，是键不存在）。真机 2026-08-04：
// pro.tmpl.attr.item 的 value_ids 就是这么整列空白的，同一轮 OnRead 里 attribute_id
// 先被展开成映射，value_ids 的域随后引用它。
//
// 只归一两种**明确的**关系形状：带 id 键的映射、以及长度为 2 且第二位是字符串的
// name_get 元组。别动其它切片——`'in'` 的值本来就是切片。
//
// 空关系（经典读回的 false）原样透传：leaf_to_sql 对 false 落的是 IS NULL/空值，
// 正是"这一列没有关联"该有的语义（见 expr_falsy_types_test.go）。
func relScalar(v any) any {
	switch t := v.(type) {
	case map[string]any:
		if id, ok := t["id"]; ok {
			return id
		}
	case []any:
		if len(t) == 2 {
			if _, ok := t[1].(string); ok {
				return t[0]
			}
		}
	}
	return v
}

// relScalars 对一列值逐个 relScalar。原切片不改（数据集还要用）。
func relScalars(vals []any) []any {
	out := make([]any, len(vals))
	for i, v := range vals {
		out[i] = relScalar(v)
	}
	return out
}

// String2Domain transfer string domain to a domain object
func String2Domain(domain string, context *dataset.TDataSet) (*TDomainNode, error) {
	parser := newDomainParser(domain)
	return parseQuery(parser, 0, context)
}

// Any2Domain accepts a slice of interface{} and parses it into a TDomainNode.
// It supports Odoo-style domain syntax:
// [term1, term2, ...] -> term1 AND term2 AND ...
// ['&', term1, term2] -> term1 AND term2 (Polish notation)
// ['!', term] -> NOT term
// ['field', 'operator', value] -> a single leaf term
func Any2Domain(domain []any, context *dataset.TDataSet) (*TDomainNode, error) {
	if domain == nil {
		return NewDomainNode(), nil
	}

	return parseAny(domain, context)
}

func parseAny(data any, context *dataset.TDataSet) (*TDomainNode, error) {
	if data == nil {
		return NewDomainNode(), nil
	}

	switch v := data.(type) {
	case *TDomainNode:
		return v, nil

	case []any:
		// Try to parse as a leaf node [field, operator, value]
		if len(v) == 3 {
			if op, ok := v[1].(string); ok {
				// Normalize operator to matching standard term operators
				if utils.IndexOf(strings.ToUpper(op), TERM_OPERATORS...) != -1 {
					node := NewDomainNode()
					node.nodeType = LEAF_NODE

					// Recursively parse parts.
					fNode, _ := parseAny(v[0], context)
					oNode, _ := parseAny(v[1], context)
					vNode, _ := parseAny(v[2], context)

					node.Push(fNode)
					node.Push(oNode)
					node.Push(vNode)
					return node, nil
				}
			}
		}

		// Otherwise parse as a container of nodes (Polish notation or implicit list)
		node := NewDomainNode()
		for _, item := range v {
			child, err := parseAny(item, context)
			if err != nil {
				return nil, err
			}
			node.Push(child)
		}
		return node, nil

	case string:
		val := v
		// Handle obvious operators
		if utils.IndexOf(val, DOMAIN_OPERATORS...) != -1 {
			return NewDomainNode(val), nil
		}

		// Handle natural language operators for consistency with parseQuery
		low := strings.ToLower(val)
		if low == "and" {
			return NewDomainNode(AND_OPERATOR), nil
		}
		if low == "or" {
			return NewDomainNode(OR_OPERATOR), nil
		}
		if low == "not" {
			return NewDomainNode(NOT_OPERATOR), nil
		}

		// Handle context variables if provided
		if context != nil {
			valus := context.ValueBy(val)
			if len(valus) > 0 {
				if len(valus) == 1 {
					return parseAny(relScalar(valus[0]), context)
				}
				return parseAny(relScalars(valus), context)
			}
		}

		// Numeric logic consistent with existing parser
		if vv, err := utils.IsNumeric(val); err == nil {
			return NewDomainNode(vv), nil
		}

		return NewDomainNode(val), nil

	default:
		// Literal basic types
		return NewDomainNode(v), nil
	}
}

// transfer the domain object to string
func Domain2String(domain *TDomainNode) string {
	return parseDomain(domain)
}

// 更新生成所有Text内容
func parseDomain(node *TDomainNode) string {
	//STEP  如果是Value Object 不处理
	if node.IsValueNode() {
		if node.Value != nil {
			return utils.ToString(node.Value)
		}
	} else {
		// 处理有Child的Object
		lStr := ""
		str_lst := make([]string, 0)
		for _, item := range node.children {
			//logger.Dbg("item", item)
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
				str_lst = append(str_lst, item.Text)
			} else*/if len(item.children) > 0 {
				// ★ 判据是「有没有子节点」，**不能判 IsListNode()**。
				//
				// IsLeafNode() 是个**带副作用的谓词**：认出一个三元 LIST_NODE 是叶子
				// 之后，它会把 nodeType 就地改写成 LEAF_NODE 作记忆化。而本函数每渲染
				// 完一个子节点，末尾那句 `if node.IsLeafNode()` 正好把它改掉。于是
				// **同一棵树第二次 String() 时**，这里的 IsListNode() 变成 false，
				// 整条叶子掉进下面的 else，被 Quote() 当成一个字符串值加上引号：
				//
				//	1st = ["&",("a","=",7),("b","=","x")]
				//	2nd = ["&","(\"a\",\"=\",7)","(\"b\",\"=\",\"x\")"]
				//
				// 第二份再喂回 String2Domain 就不再是叶子，ORM 认不出就**整条 domain
				// 丢掉、返回全表**——外部特征是"筛得越多出来的越多"，而且全程不报错。
				// 此前一直被当成"String() 是有损往返"，其实往返本身无损，坏的是
				// **调用第二次**（日志打一遍、再交给 ORM 一遍，就够了）。
				//
				// 有子节点的一律递归渲染：值节点没有子节点，走不到这条。
				// 回归：domain/parser_idempotent_test.go。
				str := parseDomain(item)
				str_lst = append(str_lst, str)
			} else {
				//str_lst = append(str_lst, node.Quote(item.Text))
				if item.IsNumeric() {
					str_lst = append(str_lst, item.String())
				} else {
					str_lst = append(str_lst, Quote(item.String()))
				}
			}
		}

		/*	// 组合 XX,XX
			str_lst := make([]string, 0)
			for _, item := range node.items {
				if item.IsList() {
					str_lst = append(str_lst, item.text)
				} else {
					str_lst = append(str_lst, self._quote(item.text))
				}
			}*/
		lStr = strings.Join(str_lst, ",")

		// 组合[XX,XX]
		if node.IsLeafNode() {
			lStr = `(` + lStr + `)`
		} else if node.IsListNode() {
			lStr = `[` + lStr + `]`
		}
		return lStr
	}

	return ""
}

// 规则：
//		1没有引号的字符串为值
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

func parseQuery(parser *TDomainParser, level int, context *dataset.TDataSet) (*TDomainNode, error) {
	result := NewDomainNode() // 存储临时列表 提供给AND
	list := NewDomainNode()   // 存储临时叶 提供给所有

	for !parser.IsEnd() {
		item := parser.Item()
		switch item.Type {
		case lexer.LPAREN, lexer.LBRACK:
			parser.Next()

			// 检测是否到尾部
			if parser.IsEnd() {
				goto exit
			}

			//开始列表采集 { xx,xx } 处理XX,XX进List
			new_leaf, err := parseQuery(parser, level+1, context)
			if err != nil {
				logger.Err(err)
			}

			// 暂存list,可能会出现[[]]现象
			list.Push(new_leaf)

		case lexer.RPAREN, lexer.RBRACK:
			goto exit

		case lexer.IDENT, lexer.HOLDER, lexer.STRING, lexer.NUMBER:
			value := strings.ToLower(item.Val)
			if value == "is" {
				parser.ConsumeWhitespace()
				parser.Next()
				nitem := parser.Item()
				if strings.ToLower(nitem.Val) != "not" {
					parser.Backup()
					list.Push("=")
				} else {
					list.Push("!=")
				}
				// break exits the switch case, not the outer for loop — intentional
				break //nolint:staticcheck // SA4011: break exits switch, not outer for; intentional
			} else if utils.IndexOf(value, "and", "or") != -1 {
				if value == "and" {
					result.Insert(0, AND_OPERATOR)
				} else if value == "or" {
					result.Insert(0, OR_OPERATOR)
				}

				// 由于[[]]现象,这里必须把list里一个条件的取出来
				if list.Count() > 1 {
					result.Push(list)
				} else {
					result.Merge(list)
				}

				list = NewDomainNode() // 新建一个列表继续采集
				break
			} else if item.Type == lexer.IDENT && (value == "true" || value == "false") {
				// Python 风格的布尔字面量 True/False（以及小写 true/false）。
				//
				// 只认**不带引号**的 IDENT：`'True'` 在词法上是 QUOTES+STRING+QUOTES，
				// 那仍然是字符串，不能动。
				//
				// 不认的后果不是报错，是 `('active','=',True)` 落成
				// `active = 'True'` —— 库里存的是 1/0 或 t/f，于是**恒回 0 条**。
				// 项目自己的 one2many 字段声明就是这个写法
				// (`domain([('active','=',True)])`)，域里筛不出东西还不报错。
				list.Push(value == "true")
				break
			} else {
				// 匹配变量值
				// TODO

				if context != nil && value != "" && item.Pos > 0 {
					last := parser.items[parser.Pos-1]
					if last.Type != lexer.QUOTES {
						// 如果ctx已经指定变量值
						valus := context.ValueBy(item.Val)
						ln := len(valus)
						if ln == 1 {
							list.Push(relScalar(valus[0]))
							break
						} else if ln > 1 {
							list.Push(relScalars(valus))
							break
						}
						list.Push(Unquote(item.Val))
						break
					}

				}

				v := Unquote(trimQuotes(item.Val))

				if vv, err := utils.IsNumeric(v); err == nil {
					list.Push(vv)
					break
				}

				list.Push(v)
				break
			}

		case lexer.OPERATOR, lexer.KEYWORD:
			list.Push(trimQuotes(item.Val))
		}

		parser.Next()
	}

exit:
	if list.Count() > 0 && list.IsValueNode() && result.IsValueNode() {
		// 当括号里面是单个值的时候需要将其直接返回
		// for (Id,in,[1])
		return result.Push(list), nil
	} else {
		if !result.IsValueNode() {
			// 由于[[]]现象,这里必须把list里一个条件的取出来
			if list.Count() == 1 {
				result.Push(list.Item(0))
			} else {
				result.Push(list)
			}
		} else {
			if list.Count() == 1 {
				return list.Item(0), nil
			}
			return list, nil
		}
	}

	return result, nil

}

// 主要-略过特殊字符移动
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
		case lexer.SAPCE: //lexer.TokenWhitespace,
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

func newDomainParser(sql string) *TDomainParser {
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
		if printToken {
			logger.Info(lexer.PrintToken(item))
		}

		if item.Type == lexer.SAPCE {
			continue
		}
		parser.items = appendOperatorToken(parser.items, item)
	}

	parser.Count = len(parser.items)
	return parser
}

// MULTI_CHAR_OPERATORS 是**词法层**要重新拼回去的多字符比较符。
//
// 只列比较符，**绝不能**把 `=?` 放进来：`?` 是 HOLDER 不是 OPERATOR，而
// `field=?` 是本项目字符串条件里最常见的写法，一旦被并成一个算子，每一条
// Where("x=?") 都会散架。
//
// `<>` 顺带在这里归一成 `!=`。它虽然被词法器认成一个算子，却不在
// TERM_OPERATORS 里，IsLeafNode() 认不出来，于是三元组不被当成叶子——
// 症状与被拆开的算子一模一样。normalize_leaf 里那句 `<>` → `!=` 救不了这一步，
// 那是**已经成为叶子之后**才跑的。
var MULTI_CHAR_OPERATORS = map[string]string{
	"!=": "!=",
	"<>": "!=",
	"<=": "<=",
	">=": ">=",
}

// appendOperatorToken 把**紧邻的**两个 OPERATOR 词元拼回一个比较符。
//
// 起因：github.com/volts-dev/lexer 的 lexOperator 在 2025-11-22 那版把
// `AcceptWhile(isOperator)` 注释掉了，改成一次只 Next() 一个 rune。于是
// `state!=?` 出来的是 [state][!][=][?] 四个词元——那一条不再是叶子而是四个平级项，
// AND 合并时被摊进上层，最后报
//
//	invalid domain leaf: expected 3 elements, got 0: state
//
// 报的是**字段名**，一个字都没提算子。凡是字符串条件里写了 `!=` / `<>` / `<=` /
// `>=` 的写路径都必定 500（真机 2026-08-30：退货向导
// stock.return.picking.action_create_returns_all）。
//
// 修在这里而不是逐个改调用方：调用方有十几处、且分散在 stock / purchase /
// account / hr / registry，改完下一个人照样会再写一个。这里也不依赖 lexer 是哪一版
// ——老版本本来就只出一个词元，拼接条件不成立，行为不变。
//
// 判据是**字节位置相邻**（next.Pos == cur.Pos+len(cur.Val)），所以
// `a = ! b` 这种隔着空格的不会被并到一起；`qty>=-1` 也只并 `>=`，
// 后面的 `-` 仍是独立词元（老版本的 AcceptWhile 会贪成 `>=-`，比现在更坏）。
func appendOperatorToken(items []lexer.TToken, item lexer.TToken) []lexer.TToken {
	if item.Type != lexer.OPERATOR {
		return append(items, item)
	}

	if len(items) > 0 {
		last := items[len(items)-1]
		if last.Type == lexer.OPERATOR && last.Pos+len(last.Val) == item.Pos {
			if op, ok := MULTI_CHAR_OPERATORS[last.Val+item.Val]; ok {
				last.Val = op
				items[len(items)-1] = last
				return items
			}
		}
	}

	// 词法器**没**拆开的那些（老版本 lexer 的 AcceptWhile 路径，出的是完整的 `<>`）
	// 同样要归一，否则 `<>` 依旧进不了 TERM_OPERATORS。
	if op, ok := MULTI_CHAR_OPERATORS[item.Val]; ok {
		item.Val = op
	}
	return append(items, item)
}
