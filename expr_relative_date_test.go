package orm

import (
	"fmt"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/volts-dev/dataset"
)

/*
domain 里的相对日期字面量（'-365d' / 'today -365d'）的回归。

病灶：这两种写法从来没有被解析过，字面量原样当参数绑进 SQL。真栈上是

	SELECT ... FROM system.sale_report WHERE ... AND sale_report."date" >= $5
	[args] [... -365d ...]
	pq: time zone displacement out of range: "-365d" (22009)   → 500

sale 的四个销售分析动作 context 里全带 `search_default_filter_order_date: 1`
（report/sale_report_views.xml），所以是打开菜单即炸；website_sale 另有三处
`'today -365d'` 的"去年"过滤器，只因默认不启用才没一起暴露。

方言差异要留个心眼，跟 expr_m2o_false_test.go 里那条一样：这些用例跑在 sqlite 上，
它不校验日期字面量，`date >= '-365d'` 退化成字符串比较，而 '-'(0x2D) 小于 '2'，
于是**每一行都匹配** —— 修复前的表现是"筛选完全失效、悄悄返回全表"，PG 上则是
直接 500。两者是同一个病灶，所以下面用"是否只剩窗口内的行"作判据，反证一样成立。

反证实测（把 expr.go 里 resolveRelativeDate 那行注释掉）：

	('d','>=','-365d')        得到 [ancient old recent today_row]  应为 [recent today_row]
	('d','>','today -365d')   得到 [ancient old recent today_row]  应为 [recent today_row]
	('dt','>=','-30d')        得到 [ancient old recent today_row]  应为 [recent today_row]
*/

type RelDateRow struct {
	TModel `table:"name('reldate_row')"`
	Id     int64  `field:"pk autoincr title('ID')"`
	Name   string `field:"varchar() size(64)"`
	D      string `field:"date"`
	Dt     string `field:"datetime"`
	Txt    string `field:"varchar() size(64)"`
}

// setupRelDate 造四行，日期一律相对**今天**算出来，这样用例不会随日历走坏：
// 今天 / 10 天前（一年窗口内）/ 400 天前 / 3 年前（窗口外）。
func setupRelDate(t *testing.T) *TOrm {
	t.Helper()
	ds := &TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "reldate.db")}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(RelDateRow)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}

	now := time.Now()
	add := func(name string, daysAgo int, txt string) {
		d := now.AddDate(0, 0, -daysAgo)
		_, err := o.Exec(`INSERT INTO reldate_row ("name","d","dt","txt") VALUES (?,?,?,?)`,
			name, d.Format("2006-01-02"), d.Format("2006-01-02")+" 08:30:00", txt)
		if err != nil {
			t.Fatalf("insert %s: %v", name, err)
		}
	}
	add("today_row", 0, "-365d") // txt 故意存字面量：非日期字段不该被改写
	add("recent", 10, "x")
	add("old", 400, "x")
	add("ancient", 3*365+2, "x")
	return o
}

func relDateNames(t *testing.T, o *TOrm, dom any) []string {
	t.Helper()
	ds, err := o.Model("reldate.row").Domain(dom).Limit(-1).Read()
	if err != nil {
		t.Fatalf("read %v: %v", dom, err)
	}
	var out []string
	if ds != nil {
		ds.Range(func(_ int, rec *dataset.TRecordSet) error {
			out = append(out, rec.FieldByName("name").AsString())
			return nil
		})
	}
	sort.Strings(out)
	return out
}

// 病灶本身：sale 销售分析那条 filter 的原样形状。
func TestRelativeDate_BareOffsetOnDateField(t *testing.T) {
	o := setupRelDate(t)

	got := relDateNames(t, o, []any{"d", ">=", "-365d"})
	want := []string{"recent", "today_row"}
	if !sameStrings(got, want) {
		t.Errorf("('d','>=','-365d') 得到 %v，应为 %v\n"+
			"（四行全回来说明字面量没被解析：sqlite 上是字符串比较恒真，PG 上直接 22009 报 500）",
			got, want)
	}
}

// website_sale 的"去年"过滤器写法。
func TestRelativeDate_TodayPrefixedOffset(t *testing.T) {
	o := setupRelDate(t)

	got := relDateNames(t, o, []any{"d", ">", "today -365d"})
	want := []string{"recent", "today_row"}
	if !sameStrings(got, want) {
		t.Errorf("('d','>','today -365d') 得到 %v，应为 %v", got, want)
	}
}

// datetime 字段要补上时间部分，否则 'YYYY-MM-DD' 与 'YYYY-MM-DD hh:mm:ss' 的
// 字符串比较会把当天的行整批切掉。
func TestRelativeDate_DatetimeFieldKeepsSameDayRows(t *testing.T) {
	o := setupRelDate(t)

	got := relDateNames(t, o, []any{"dt", ">=", "-30d"})
	want := []string{"recent", "today_row"}
	if !sameStrings(got, want) {
		t.Errorf("('dt','>=','-30d') 得到 %v，应为 %v\n"+
			"（缺 today_row 说明右值没补 00:00:00，当天 08:30 的行被判成更小）", got, want)
	}
}

// 只有 date/datetime 字段参与解析。别的字段类型上 '-365d' 就是一个普通字符串，
// 改写它等于篡改用户条件。
func TestRelativeDate_NonDateFieldUntouched(t *testing.T) {
	o := setupRelDate(t)

	got := relDateNames(t, o, []any{"txt", "=", "-365d"})
	want := []string{"today_row"}
	if !sameStrings(got, want) {
		t.Errorf("('txt','=','-365d') 得到 %v，应为 %v（字符字段上的字面量被当日期换掉了）", got, want)
	}
}

// 一组值（in）里的字面量同样要认。
func TestRelativeDate_InListResolved(t *testing.T) {
	o := setupRelDate(t)

	today := time.Now().Format("2006-01-02")
	got := relDateNames(t, o, []any{"d", "in", []any{"today", "1999-01-01"}})
	want := []string{"today_row"}
	if !sameStrings(got, want) {
		t.Errorf("('d','in',['today','1999-01-01']) 得到 %v，应为 %v（今天=%s）", got, want, today)
	}
}

func TestRelativeDateToAbsolute_Forms(t *testing.T) {
	// 固定基准日，覆盖月末 clamp：2026-03-31。
	base := time.Date(2026, 3, 31, 14, 5, 6, 0, time.Local)
	day := func(y int, m time.Month, d int) string {
		return fmt.Sprintf("%04d-%02d-%02d", y, m, d)
	}

	cases := []struct {
		in       string
		withTime bool
		want     string
		ok       bool
	}{
		{"-365d", false, day(2025, 3, 31), true},
		{"+7d", false, day(2026, 4, 7), true},
		{"-2w", false, day(2026, 3, 17), true},
		{"today", false, day(2026, 3, 31), true},
		{"today -365d", false, day(2025, 3, 31), true},
		{"TODAY +1D", false, day(2026, 4, 1), true},
		{" -1m ", false, day(2026, 2, 28), true}, // clamp：2月没有31号
		{"+1m", false, day(2026, 4, 30), true},   // clamp：4月没有31号
		{"-1y", false, day(2025, 3, 31), true},
		{"-1d", true, day(2026, 3, 30) + " 00:00:00", true},
		// 不是相对日期的一律原样放行，绝不能悄悄换成零值日期
		{"2026-01-01", false, "", false},
		{"365d", false, "", false}, // 不带符号不认
		{"-365x", false, "", false},
		{"", false, "", false},
		{"yesterday", false, "", false},
	}
	for _, c := range cases {
		got, ok := RelativeDateToAbsolute(c.in, c.withTime, base)
		if ok != c.ok || (c.ok && got != c.want) {
			t.Errorf("RelativeDateToAbsolute(%q, withTime=%v) = (%q,%v)，应为 (%q,%v)",
				c.in, c.withTime, got, ok, c.want, c.ok)
		}
	}
}
