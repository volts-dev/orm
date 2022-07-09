package domain

import (
	"strings"

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
	s = strings.Trim(s, `'`)
	s = strings.Trim(s, `"`)
	return s
}

//TODO 描述例句
// transfer string domain to a domain object
func String2Domain(domain string) (*TDomainNode, error) {
	parser := newDomainParser(domain)
	return parseQuery(parser, 0)
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
			return utils.Itf2Str(node.Value)
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
			} else*/if item.IsListNode() {
				str := parseDomain(item)
				str_lst = append(str_lst, str)
			} else {
				//str_lst = append(str_lst, node.Quote(item.Text))
				str_lst = append(str_lst, item.String())
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
			parser.Next()

			// 检测是否到尾部
			if parser.IsEnd() {
				goto exit
			}

			//开始列表采集 { xx,xx } 处理XX,XX进List
			new_leaf, err := parseQuery(parser, level+1)
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

				break
			} else if utils.InStrings(value, "and", "or") != -1 {
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
			} else {
				v := trimQuotes(item.Val)
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

	if list.IsValueNode() && result.IsValueNode() {
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
		case lexer.TokenWhitespace, lexer.SAPCE:
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

		parser.items = append(parser.items, item)
	}

	parser.Count = len(parser.items)
	return parser
}
