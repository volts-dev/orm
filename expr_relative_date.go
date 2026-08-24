package orm

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/volts-dev/orm/domain"
)

// domain 里的相对日期字面量。
//
// Odoo 的搜索视图直接把相对日期写进 domain 值里，例如 sale 的销售分析：
//
//	<filter name="filter_order_date" domain="[('date', '>=', '-365d')]"/>
//
// 本仓的视图里还有一种等价写法 `'today -365d'`（website_sale 的"去年"过滤器，
// Odoo 那边原文是 python 表达式 `(context_today()-relativedelta(days=365))`，
// 移植时因为本仓不 eval python 而改成了这个形式）。两种都在这里认。
//
// 没有这一步，字面量会原样当成参数发给数据库：
//
//	pq: time zone displacement out of range: "-365d" (22009)
//
// 即 500，而且报错里没有任何东西指向那张搜索视图。sale 的四个销售分析动作
// (view_order_product_graph / _pivot / user / product / partner) 的 context 全带
// `search_default_filter_order_date: 1`，所以是**打开即炸**，不是边角情况。
//
// 语义与 Odoo 一致：值相对于**今天**，单位 d/w/m/y = 日/周/月/年，符号 +/- 必带
// （`365d` 这种不带符号的不认，避免把普通字符串误当日期）。月和年按 relativedelta
// 的口径处理月末：1月31日减一个月是 12月31日，加一个月是 2月28/29日，而不是 Go
// AddDate 规范化出来的 3月2日。
var reRelativeDate = regexp.MustCompile(`(?i)^\s*(?:today\s*)?([+-])\s*(\d+)\s*([dwmy])\s*$`)

// resolveRelativeDate 就地把 leaf 右值里的相对日期字面量换成绝对日期。
//
// 只对 date / datetime 字段生效：别的字段类型上 "-365d" 只是一个普通字符串，
// 改写它就是篡改用户条件。右值既可能是单值，也可能是一组值（in / not in）。
func resolveRelativeDate(field IField, right *domain.TDomainNode) {
	if field == nil || right == nil {
		return
	}

	var withTime bool
	switch strings.ToLower(field.TypeName()) {
	case "date":
		withTime = false
	case "datetime":
		withTime = true
	default:
		return
	}

	if right.Count() > 0 {
		for _, node := range right.Nodes() {
			replaceRelativeDate(node, withTime)
		}
		return
	}
	replaceRelativeDate(right, withTime)
}

func replaceRelativeDate(node *domain.TDomainNode, withTime bool) {
	if node == nil {
		return
	}
	raw, ok := node.Value.(string)
	if !ok {
		return
	}
	if v, ok := RelativeDateToAbsolute(raw, withTime, time.Now()); ok {
		node.Value = v
	}
}

// RelativeDateToAbsolute 解析 '-365d' / '+2w' / 'today' / 'today -365d' 这类字面量，
// 返回可直接入库比较的日期串。第二个返回值为 false 表示"这不是相对日期"，调用方
// 应当原样保留该值——绝不能在这里替换成零值日期，那会把条件悄悄改成另一个意思。
//
// now 由调用方传入而不是内部取，是为了让测试可复现；生产路径一律传 time.Now()，
// 与全仓"墙上时间"的口径一致（数据库里的 date/datetime 存的就是墙上时间）。
func RelativeDateToAbsolute(raw string, withTime bool, now time.Time) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", false
	}

	var d time.Time
	if strings.EqualFold(s, "today") {
		d = now
	} else {
		m := reRelativeDate.FindStringSubmatch(s)
		if m == nil {
			return "", false
		}
		n, err := strconv.Atoi(m[2])
		if err != nil {
			return "", false
		}
		if m[1] == "-" {
			n = -n
		}
		switch strings.ToLower(m[3]) {
		case "d":
			d = now.AddDate(0, 0, n)
		case "w":
			d = now.AddDate(0, 0, n*7)
		case "m":
			d = addMonthsClamped(now, n)
		case "y":
			d = addMonthsClamped(now, n*12)
		default:
			return "", false
		}
	}

	if withTime {
		return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, d.Location()).
			Format("2006-01-02 15:04:05"), true
	}
	return d.Format("2006-01-02"), true
}

// addMonthsClamped 按 python relativedelta 的口径加减月份：目标月没有这一天就取
// 该月最后一天。Go 原生的 AddDate 是规范化溢出（3月31日 -1 月 = 3月3日），用在
// "上个月"这种过滤器上会把边界甩到错误的一侧。
func addMonthsClamped(t time.Time, months int) time.Time {
	year, month, day := t.Date()
	first := time.Date(year, month, 1, 0, 0, 0, 0, t.Location()).AddDate(0, months, 0)
	if last := daysInMonth(first.Year(), first.Month()); day > last {
		day = last
	}
	return time.Date(first.Year(), first.Month(), day,
		t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
}

func daysInMonth(year int, month time.Month) int {
	// 下个月的第 0 天 = 本月最后一天
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}
