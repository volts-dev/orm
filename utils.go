package orm

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/volts-dev/orm/logger"
	"github.com/volts-dev/utils"
)

// contains reports whether the string contains the byte c.
func contains(s string, c byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return true
		}
	}
	return false
}

// format the model name to the same
func fmtModelName(name string) string {
	name = strings.Replace(name, ".", "_", -1)
	return utils.SnakeCasedName(utils.TrimQuotes(utils.Trim(name)))
}

// format the field name to the same
func fmtFieldName(name string) string {
	name = strings.Replace(name, ".", "_", -1)
	return utils.SnakeCasedName(utils.TrimQuotes(utils.Trim(name)))
}

func lookup(tag string, key ...string) (value string) {
	// When modifying this code, also update the validateStructTag code
	// in golang.org/x/tools/cmd/vet/structtag.go.

	for tag != "" {
		// Skip leading space.
		i := 0
		for i < len(tag) && tag[i] == ' ' {
			i++
		}
		tag = tag[i:]
		if tag == "" {
			break
		}

		// Scan to colon. A space, a quote or a control character is a syntax error.
		// Strictly speaking, control chars include the range [0x7f, 0x9f], not just
		// [0x00, 0x1f], but in practice, we ignore the multi-byte control characters
		// as it is simpler to inspect the tag's bytes than the tag's runes.
		i = 0
		for i < len(tag) && tag[i] > ' ' && tag[i] != ':' && tag[i] != '"' && tag[i] != 0x7f {
			i++
		}
		if i == 0 || i+1 >= len(tag) || tag[i] != ':' || tag[i+1] != '"' {
			break
		}
		name := string(tag[:i])
		tag = tag[i+1:]
		//fmt.Println("tag", tag)
		// Scan quoted string to find value.
		i = 1
		for i < len(tag) && (tag[i] != '"' || (i+1 < len(tag) && tag[i+1] != ' ' && tag[i] == '"')) {
			if tag[i] == '\\' {
				i++
			}
			i++
		}
		if i >= len(tag) {
			break
		}
		//fmt.Println("tag", tag)
		qvalue := string(tag[:i+1])
		tag = tag[i+1:]
		//fmt.Println("key", key, name, qvalue)
		if utils.InStrings(name, key...) != -1 {
			value, err := unquote(qvalue)
			if err != nil {
				logger.Errf("parse Tag error: %s, %s : %s", qvalue, value, err.Error())
				break
			}
			return value
		}
	}
	return ""
}

func splitTag(tag string) (tags []string) {
	tag = strings.TrimSpace(tag)
	var hasQuote = false
	var lastIdx = 0
	for i, t := range tag {
		if t == '\'' { // #  t == '(' || t == ')' { //
			hasQuote = !hasQuote
		} else if t == ' ' {
			if lastIdx < i && !hasQuote {
				newtag := strings.TrimSpace(tag[lastIdx:i])

				// #去除换行缩进的空格
				if newtag != "" {
					tags = append(tags, newtag)
				}

				lastIdx = i + 1
			}
		}
	}
	if lastIdx < len(tag) {
		tags = append(tags, strings.TrimSpace(tag[lastIdx:len(tag)]))
	}
	return
}

// TODO 支持Vals 包含空格 one2many(product.attribute.price,空格value_id)
// #　解析 'Tag(vals)' 整个字符串
func parseTag(tag string) (tags []string) {
	tag = strings.TrimSpace(tag)
	var (
		hasQuote          = false
		hasSquareBrackets = false
		//Bracket           = false
		lastIdx = 0
		l       = len(tag)
	)
	for i, t := range tag {
		if t == '\'' {
			hasQuote = !hasQuote
		}
		//fmt.Println(t,i)
		if t == '[' || t == ']' {
			hasSquareBrackets = !hasSquareBrackets
		} else if t == '(' || t == ',' || t == ')' { //处理 Tag(xxx)类型
			//if t == '(' && !Bracket {
			//	Bracket = true
			//}

			if lastIdx < i && !hasQuote && !hasSquareBrackets {
				//tags = append(tags, strings.Trim(strings.TrimSpace(tag[lastIdx:i]), "'"))
				tags = append(tags, strings.TrimSpace(tag[lastIdx:i]))
				lastIdx = i + 1
			}
		} else if i+1 == l { // 处理无括号类型的Tag
			tags = append(tags, strings.TrimSpace(tag[lastIdx:l]))
		}

	}
	//if lastIdx < len(tag) {
	//	tags = append(tags, strings.TrimSpace(tag[lastIdx:len(tag)]))
	//}
	return
}

// #修改支持多行的Tag
// Unquote interprets s as a single-quoted, double-quoted,
// or backquoted Go string literal, returning the string value
// that s quotes.  (If s is single-quoted, it would be a Go
// character literal; Unquote returns the corresponding
// one-character string.)
func unquote(s string) (string, error) {
	n := len(s)
	if n < 2 {
		return "", errors.New("invalid quoted string")
	}
	quote := s[0]
	if quote != s[n-1] {
		return "", errors.New("lost the quote symbol on the end")
	}
	s = s[1 : n-1]

	if quote == '`' {
		if contains(s, '`') {
			return "", errors.New("the '`' symbol is found on the content")
		}
		return s, nil
	}

	if quote != '"' && quote != '\'' {
		return "", errors.New("lost the quote symbol on the begin")
	}

	//if contains(s, '\n') {
	//	//Println("contains(s, '\n')")
	//	return "contains(s, '\n')", strconv.ErrSyntax
	//}

	// Is it trivial?  Avoid allocation.
	if !contains(s, '\\') && !contains(s, quote) {
		switch quote {
		case '"':
			return s, nil
		case '\'':
			r, size := utf8.DecodeRuneInString(s)
			if size == len(s) && (r != utf8.RuneError || size != 1) {
				return s, nil
			}
		}
	}

	var runeTmp [utf8.UTFMax]byte
	buf := make([]byte, 0, 3*len(s)/2) // Try to avoid more allocations.
	for len(s) > 0 {
		c, multibyte, ss, err := strconv.UnquoteChar(s, quote)
		if err != nil {
			return "", err
		}
		s = ss
		if c < utf8.RuneSelf || !multibyte {
			buf = append(buf, byte(c))
		} else {
			n := utf8.EncodeRune(runeTmp[:], c)
			buf = append(buf, runeTmp[:n]...)
		}
		if quote == '\'' && len(s) != 0 {
			// single-quoted must be single character
			return "", strconv.ErrSyntax
		}
	}
	return string(buf), nil
}
