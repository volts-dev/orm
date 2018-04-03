package orm

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
	"vectors/utils"
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

	ItemError             ItemType = iota // error occurred; value is text of error
	ItemEOF                               // end of the file
	ItemWhitespace                        // a run of spaces, tabs and newlines
	ItemSingleLineComment                 // A comment like --
	ItemMultiLineComment                  // A multiline comment like /* ... */
	ItemKeyword                           // SQL language keyword like SELECT, INSERT, etc.
	ItemIdentifier                        // alphanumeric identifier or complex identifier like `a.b` and `c`.*
	ItemOperator                          // operators like '=', '<>', etc.
	ItemLeftParen                         // '('
	ItemRightParen                        // ')'
	ItemComma                             // ','
	ItemDot                               // '.'
	ItemStetementEnd                      // ';'
	ItemNumber                            // simple number, including imaginary
	ItemString                            // quoted string (includes quotes)
	ItemValueHolder                       // ?

	EOF = -1
)

var (
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
	ItemType int

	// Item represents a token or text string returned from the scanner.
	Item struct {
		Type ItemType // The type of this Item.
		Pos  int      // The starting position, in bytes, of this item in the input string.
		Val  string   // The value of this Item.
	}

	// StateFn represents the state of the scanner as a function that returns the next state.
	StateFn func(*TLexer) StateFn

	// ValidatorFn represents a function that is used to check whether a specific rune matches certain rules.
	ValidatorFn func(rune) bool

	// Lexer holds the state of the scanner.
	TLexer struct {
		state             StateFn       // the next lexing function to enter
		input             io.RuneReader // the input source
		inputCurrentStart int           // start position of this item
		buffer            []rune        // a slice of runes that contains the currently lexed item
		bufferPos         int           // the current position in the buffer
		Items             chan Item     // channel of scanned Items
	}

	TDomainParser struct {
		items []Item
		Pos   int
		Count int

		isEnd bool
	}
)

//主要-略过特殊字符移动
// 并返回不符合条件的Item
// 回退Pos 到空白Item处,保持下一个有效字符
func (self *TDomainParser) ConsumeWhitespace() (item Item) {
	for {
		self.Next()
		if self.isEnd {
			break
		}
		//fmt.Println("consume_whitespace", self.Item().Val)
		switch self.Item().Type {
		case ItemWhitespace:
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

func (self *TDomainParser) Item() Item {
	return self.items[self.Pos]
}

func (self *TDomainParser) Find(i Item) int {
	return 0
}

// next() returns the next rune in the input.
func (self *TLexer) next() rune {
	if self.bufferPos < len(self.buffer) {
		res := self.buffer[self.bufferPos]
		self.bufferPos++
		return res
	}

	r, _, err := self.input.ReadRune()
	if err == io.EOF {
		r = EOF
	} else if err != nil {
		panic(err)
	}

	self.buffer = append(self.buffer, r)
	self.bufferPos++
	return r
}

// peek() returns but does not consume the next rune in the input.
func (self *TLexer) peek() rune {
	if self.bufferPos < len(self.buffer) {
		return self.buffer[self.bufferPos]
	}

	r, _, err := self.input.ReadRune()
	if err == io.EOF {
		r = EOF
	} else if err != nil {
		panic(err)
	}

	self.buffer = append(self.buffer, r)
	return r
}

// peek() returns but does not consume the next few runes in the input.
func (self *TLexer) peekNext(length int) string {
	lenDiff := self.bufferPos + length - len(self.buffer)
	if lenDiff > 0 {
		for i := 0; i < lenDiff; i++ {
			r, _, err := self.input.ReadRune()
			if err == io.EOF {
				r = EOF
			} else if err != nil {
				panic(err)
			}

			self.buffer = append(self.buffer, r)
		}
	}

	return string(self.buffer[self.bufferPos : self.bufferPos+length])
}

// backup steps back one rune
func (self *TLexer) backup() {
	self.backupWith(1)
}

// backup steps back many runes
func (self *TLexer) backupWith(length int) {
	if self.bufferPos < length {
		panic(fmt.Errorf("lexer: trying to backup with %d when the buffer position is %d", length, self.bufferPos))
	}

	self.bufferPos -= length
}

// emit passes an Item back to the client.
func (self *TLexer) emit(t ItemType) {
	self.Items <- Item{t, self.inputCurrentStart, string(self.buffer[:self.bufferPos])}
	self.ignore()
}

// ignore skips over the pending input before this point.
func (self *TLexer) ignore() {
	itemByteLen := 0
	for i := 0; i < self.bufferPos; i++ {
		itemByteLen += utf8.RuneLen(self.buffer[i])
	}

	self.inputCurrentStart += itemByteLen
	self.buffer = self.buffer[self.bufferPos:] //TODO: check for memory leaks, maybe copy remaining items into a new slice?
	self.bufferPos = 0
}

// accept consumes the next rune if it's from the valid set.
func (self *TLexer) accept(valid string) int {
	r := self.next()
	if strings.IndexRune(valid, r) >= 0 {
		return 1
	}
	self.backup()
	return 0
}

// acceptWhile consumes runes while the specified condition is true
func (self *TLexer) acceptWhile(fn ValidatorFn) int {
	r := self.next()
	count := 0
	for fn(r) {
		r = self.next()
		count++
	}
	self.backup()
	return count
}

// acceptUntil consumes runes until the specified contidtion is met
func (self *TLexer) acceptUntil(fn ValidatorFn) int {
	r := self.next()
	count := 0
	for !fn(r) && r != EOF {
		r = self.next()
		count++
	}
	self.backup()
	return count
}

// errorf returns an error token and terminates the scan by passing
// back a nil pointer that will be the next state, terminating self.nextItem.
func (self *TLexer) errorf(format string, args ...interface{}) StateFn {
	self.Items <- Item{ItemError, self.inputCurrentStart, fmt.Sprintf(format, args...)}
	return nil
}

// nextItem returns the next Item from the input.
func (self *TLexer) nextItem() Item {
	return <-self.Items
}

func NewDomainParser(sql string) *TDomainParser {
	lexer := NewLex(strings.NewReader(sql))
	parser := &TDomainParser{
		items: make([]Item, 0),
		Pos:   0,
	}

	for {
		item, ok := <-lexer.Items
		if !ok {
			break
		}

		parser.items = append(parser.items, item)
	}

	parser.Count = len(parser.items)
	return parser
}

// lex creates a new scanner for the input string.
func NewLex(input io.Reader) *TLexer {
	lexer := &TLexer{
		input:  bufio.NewReader(input),
		buffer: make([]rune, 0, 10),
		Items:  make(chan Item),
	}
	go lexer.run()
	return lexer
}

// run runs the state machine for the Lexer.
func (self *TLexer) run() {
	for state := lexWhitespace; state != nil; {
		state = state(self)
	}
	close(self.Items)
}

// isSpace reports whether r is a whitespace character (space or end of line).
func isWhitespace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\r' || r == '\n'
}

// isSpace reports whether r is a space character.
func isSpace(r rune) bool {
	return r == ' ' || r == '\t'
}

// isEndOfLine reports whether r is an end-of-line character.
func isEndOfLine(r rune) bool {
	return r == '\r' || r == '\n' || r == EOF
}

// isOperator reports whether r is an operator.
func isOperator(r rune) bool {
	return r == '+' || r == '-' || r == '*' || r == '/' || r == '=' || r == '>' || r == '<' || r == '~' || r == '|' || r == '^' || r == '&' || r == '%'
	//return utils.InStrings()
}

func isHolder(r rune) bool {
	return r == '?' || r == '%' || r == 's'
}

func lexWhitespace(lexer *TLexer) StateFn {
	lexer.acceptWhile(isWhitespace)
	if lexer.bufferPos > 0 {
		lexer.emit(ItemWhitespace)
	}

	next := lexer.peek()
	nextTwo := lexer.peekNext(2)

	switch {
	case next == EOF:
		lexer.emit(ItemEOF)
		return nil

	case nextTwo == "--":
		return lexSingleLineComment

	case nextTwo == "/*":
		return lexMultiLineComment

	case nextTwo == "%s":
		return lexHolder
	case next == '(' || next == '[':
		lexer.next()
		lexer.emit(ItemLeftParen)
		return lexWhitespace

	case next == ')' || next == ']':
		lexer.next()
		lexer.emit(ItemRightParen)
		return lexWhitespace

	case next == ',':
		lexer.next()
		lexer.emit(ItemComma)
		return lexWhitespace

	case next == ';':
		lexer.next()
		lexer.emit(ItemStetementEnd)
		return lexWhitespace

	case isOperator(next):
		return lexOperator

	case next == '"' || next == '\'':
		return lexString

	case ('0' <= next && next <= '9'):
		return lexNumber

	case utils.IsAlphaNumericRune(next) || next == '`':
		return lexIdentifierOrKeyword

	case next == '?':
		return lexHolder

	default:
		lexer.errorf("don't know what to do with '%s'", nextTwo)
		return nil
	}
}

func lexSingleLineComment(lexer *TLexer) StateFn {
	lexer.acceptUntil(isEndOfLine)
	lexer.emit(ItemSingleLineComment)
	return lexWhitespace
}

func lexMultiLineComment(lexer *TLexer) StateFn {
	lexer.next()
	lexer.next()
	for {
		lexer.acceptUntil(func(r rune) bool { return r == '*' })
		if lexer.peekNext(2) == "*/" {
			lexer.next()
			lexer.next()
			lexer.emit(ItemMultiLineComment)
			return lexWhitespace
		}

		if lexer.peek() == EOF {
			lexer.errorf("reached EOF when looking for comment end")
			return nil
		}

		lexer.next()
	}
}

func lexOperator(lexer *TLexer) StateFn {
	lexer.acceptWhile(isOperator)
	lexer.emit(ItemOperator)
	return lexWhitespace
}

func lexNumber(lexer *TLexer) StateFn {
	count := 0
	count += lexer.acceptWhile(unicode.IsDigit)
	if lexer.accept(".") > 0 {
		count += 1 + lexer.acceptWhile(unicode.IsDigit)
	}
	if lexer.accept("eE") > 0 {
		count += 1 + lexer.accept("+-")
		count += lexer.acceptWhile(unicode.IsDigit)
	}

	if utils.IsAlphaNumericRune(lexer.peek()) {
		// We were lexing an identifier all along - backup and pass the ball
		lexer.backupWith(count)
		return lexIdentifierOrKeyword
	}

	lexer.emit(ItemNumber)
	return lexWhitespace
}

func lexString(lexer *TLexer) StateFn {
	quote := lexer.next()

	for {
		n := lexer.next()

		if n == EOF {
			return lexer.errorf("unterminated quoted string")
		}
		if n == '\\' {
			//TODO: fix possible problems with NO_BACKSLASH_ESCAPES mode
			if lexer.peek() == EOF {
				return lexer.errorf("unterminated quoted string")
			}
			lexer.next()
		}

		if n == quote {
			if lexer.peek() == quote {
				lexer.next()
			} else {
				lexer.emit(ItemString)
				return lexWhitespace
			}
		}
	}

}

func lexIdentifierOrKeyword(lexer *TLexer) StateFn {
	for {
		s := lexer.next()

		if s == '`' {
			for {
				n := lexer.next()

				if n == EOF {
					return lexer.errorf("unterminated quoted string")
				} else if n == '`' {
					if lexer.peek() == '`' {
						lexer.next()
					} else {
						break
					}
				}
			}
			lexer.emit(ItemIdentifier)
		} else if utils.IsAlphaNumericRune(s) {
			lexer.acceptWhile(utils.IsAlphaNumericRune)

			//TODO: check whether token is a keyword or an identifier
			lexer.emit(ItemIdentifier)
		}

		lexer.acceptWhile(isWhitespace)
		if lexer.bufferPos > 0 {
			lexer.emit(ItemWhitespace)
		}

		if lexer.peek() != '.' {
			break
		}

		lexer.next()
		lexer.emit(ItemDot)
	}

	return lexWhitespace
}

func lexHolder(lexer *TLexer) StateFn {
	lexer.acceptWhile(isHolder)
	lexer.emit(ItemValueHolder)
	return lexWhitespace
}

var (
	itemNames = map[ItemType]string{
		ItemError:      "error",
		ItemEOF:        "EOF",
		ItemKeyword:    "keyword",
		ItemOperator:   "operator",
		ItemIdentifier: "identifier",
		ItemLeftParen:  "left_paren",
		ItemNumber:     "number",
		ItemRightParen: "right_paren",
		ItemWhitespace: "space",
		ItemString:     "string",
		//ItemComment:        "comment",
		//ItemStatementStart: "statement_start",
		ItemStetementEnd: "statement_end",
		ItemValueHolder:  "ItemValueHolder",
	}
)

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

func _parse_query(parser *TDomainParser, level int) (*utils.TStringList, error) {
	var (
		AndOrCount int = 0
	)
	temp := utils.NewStringList() // 存储临时列表 提供给AND
	list := utils.NewStringList() // 存储临时叶 提供给所有
	for !parser.IsEnd() {
		item := parser.Item()
		//fmt.Println(fmt.Sprintf("> %q('%q')", itemNames[item.Type], item.Val))
		switch item.Type {
		case ItemLeftParen:
			// 检测是否到尾部
			parser.Next()
			if parser.IsEnd() {
				goto exit
			}

			//开始列表采集 { xx,xx } 处理XX,XX进List
			new_list, err := _parse_query(parser, level+1)
			if err != nil {

			}

			list.Push(new_list)
			// 以下废弃
			/*
				// TODO 优化
				next := parser.ConsumeWhitespace()
				//parser.Next()
				//next := parser.Item()
				//parser.Backup()

				//fmt.Println(fmt.Sprintf("> %q('%q')", itemNames[next.Type], next.Val))
				//list 是整个[ xx,xx ],所以取到的xx,xx 必须平铺出来当List的子项
				if list.Count() == 0 && next.Val != "," {
					list.Push(new_list.Items()...)
				} else { // 如果带其他子项可能是 [ xx,xx,[xx,xx] ] 则不用展开

					list.Push(new_list)

				}
			*/
		case ItemRightParen:
			goto exit

		case ItemIdentifier, ItemString, ItemNumber, ItemValueHolder:
			if utils.InStrings(strings.ToLower(item.Val), "and", "or") != -1 {
				if strings.ToLower(item.Val) == "and" {
					temp.Insert(AndOrCount, AND_OPERATOR)
				}

				if strings.ToLower(item.Val) == "or" {
					temp.Insert(AndOrCount, OR_OPERATOR)
				}

				AndOrCount++
				temp.Push(list)              // 已经完成了一个叶的采集 暂存列表里
				list = utils.NewStringList() // 新建一个列表继续采集
				break
			}

			list.PushString(_trim_quotes(item.Val))

		case ItemOperator, ItemKeyword:
			list.PushString(_trim_quotes(item.Val))

		}

		parser.Next()
	}

exit:
	if temp.Count() == 0 { // 当temp为0 时证明不是带Or And 的条件 直接返回List
		return list, nil
	} else {
		// # 以下处理SQL语句
		// 由于(XX=1 and XX='111') 已经把 and,XX=1, XX='111'当子项传给Temp 所以返回Temp
		temp.Push(list) // 已经完成了一个叶的采集 暂存列表里

		// 由于SQL where语句一般不包含[] 所以添加一层以抵消
		if level == 0 {
			shell := utils.NewStringList()
			shell.Push(temp)
			return shell, nil
		}

		return temp, nil // 结束该列表采集
	}
}

func _trim_quotes(s string) string {
	s = strings.Trim(s, `'`)
	s = strings.Trim(s, `"`)
	return s
}

func Query2StringList(sql string) (res_domain *utils.TStringList) {
	parser := NewDomainParser(sql)

	res_domain, err := _parse_query(parser, 0)
	if err != nil {

	}

	// 确保Domain为List形态
	/*for {
		if res_domain.Count() == 1 {
			res_domain = res_domain.Item(0)
			continue
		}

		break
	}*/

	return res_domain.Item(0)
}

//TODO 描述例句
func Domain2StringList(domain string) *utils.TStringList {
	return Query2StringList(domain)
}

func StringList2Domain(lst *utils.TStringList) string {
	_parse_stringlist(lst)
	return lst.Text
}

// 更新生成所有Text内容
func _parse_stringlist(node *utils.TStringList) {
	//STEP  如果是Value Object 不处理
	IsList := false
	if node.Count() == 0 {
		return
	} else {
		IsList = true
	}

	// 处理有Child的Object
	lStr := ""
	lStrLst := make([]string, 0)

	for _, item := range node.Items() {
		//fmt.Println("item", item.String())
		/*if is_leaf(item) {
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
			_parse_stringlist(item)
			lStrLst = append(lStrLst, item.Text)
		} else {
			//fmt.Println("_update val", item.Text)
			//lStrLst = append(lStrLst, node.Quote(item.Text))
			lStrLst = append(lStrLst, item.Text)
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
	if is_leaf(node) {
		lStr = `(` + lStr + `)`
	} else if IsList {
		lStr = `[` + lStr + `]`
	}

	node.Text = lStr
	//utils.Dbg("_update lst", lStr)
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
func is_leaf(element *utils.TStringList, internal ...bool) bool {
	INTERNAL_OPS := append(TERM_OPERATORS, "<>")

	if internal != nil && internal[0] {
		INTERNAL_OPS = append(INTERNAL_OPS, "inselect")
		INTERNAL_OPS = append(INTERNAL_OPS, "not inselect")
	}
	//fmt.Println("is_leaf", element.Count(), strings.ToLower(element.String(1)), utils.InStrings(strings.ToLower(element.String(1)), INTERNAL_OPS...) != -1)
	//??? 出现过==Nil还是继续执行之下的代码
	return (element != nil && element.Count() == 3) &&
		utils.InStrings(strings.ToLower(element.String(1)), INTERNAL_OPS...) != -1 || // 注意操作符确保伟小写
		utils.InStrings(element.String(), TRUE_LEAF, FALSE_LEAF) != -1 // BUG

	/*
	   def is_leaf(element, internal=False):

	       INTERNAL_OPS = TERM_OPERATORS + ('<>',)
	       if internal:
	           INTERNAL_OPS += ('inselect', 'not inselect')
	       return (isinstance(element, tuple) or isinstance(element, list)) \
	           and len(element) == 3 \
	           and element[1] in INTERNAL_OPS \
	           and ((isinstance(element[0], basestring) and element[0]) or tuple(element) in (TRUE_LEAF, FALSE_LEAF))
	*/
}
