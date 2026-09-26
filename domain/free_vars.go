package domain

import (
	"strings"

	"github.com/volts-dev/lexer"
)

// domainLiteralIdents 是不带引号、却不是变量的那几个词：Python 字面量，以及本解析器
// 额外接受的 SQL 风格连接词（见 parseQuery 的 is / and / or 分支）。
var domainLiteralIdents = map[string]bool{
	"true": true, "false": true, "none": true,
	"and": true, "or": true, "is": true, "not": true,
}

// FreeVariables 返回 domain 串里引用的变量名（不带引号的标识符，如
// `[('attribute_id', '=', attribute_id)]` 里第二个 attribute_id、`uid`、`parent.x` 的 parent），
// 按首次出现去重。没有变量时返回 nil。
//
// 用处：Odoo 的字段 domain 只要引用了记录上的值就只能写成**字符串**，而
// Field.get_comodel_domain 对字符串 domain 直接回 Domain.TRUE——"domain=str is used only
// for the client-side"。本仓的 tag / Builder 里 domain 一律是字符串，区分不了这两种写法，
// 能区分的只有内容：带变量的必然是给前端求值的那种，服务端没有可代入的值（批量读时
// 每行各自一个值，写入时连父记录都没有），硬套只会把变量名当成字面串去比。
//
// 判据与 parseQuery 的「匹配变量值」分支一致：IDENT 词元、且不紧跟在引号之后。
// 引号里的内容词法上是 STRING，数字是 NUMBER / FLOAT，占位符 `?` 是 HOLDER，都不算。
func FreeVariables(expr string) []string {
	if expr == "" {
		return nil
	}
	parser := newDomainParser(expr)
	var out []string
	seen := map[string]bool{}
	for i, item := range parser.items {
		if item.Type != lexer.IDENT {
			continue
		}
		if i > 0 && parser.items[i-1].Type == lexer.QUOTES {
			continue
		}
		name := strings.TrimSpace(item.Val)
		if name == "" || domainLiteralIdents[strings.ToLower(name)] {
			continue
		}
		// `parent.company_id` 词法上是 IDENT . IDENT：点号后面那段是属性，不是另一个变量。
		if i > 0 && parser.items[i-1].Val == "." {
			continue
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}
