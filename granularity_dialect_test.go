package orm

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 按日期粒度截断 / 取日期部分：PG 与 MySQL 算出的值都等于 Go 算的（MySQL 兼容，2026-10-26）。期望值在 Go 里
// 独立算（不拿哪一家当基准）：datetime 先换到时区再截断、截断后的本地起点换回 UTC；date 不换时区；周从周一起；
// iso_week_number 是 ISO 周；day_of_week 周日 = 0。
func TestGranularitySql_MatchesGoOnPGAndMySQL(t *testing.T) {
	skipUnlessDialect(t, "postgres", "mysql")
	o, err := New(WithDataSource(testDataSource(t, filepath.Join(t.TempDir(), "gran.db"))))
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	d := o.Dialect()
	tsType := "timestamp"
	if d.DBType() == "mysql" {
		tsType = "datetime(6)"
	}
	// 跨年、跨季、周日 / 周一、月底深夜（东八区已是次月）、美东夏令时前后。
	stamps := []time.Time{
		time.Date(2026, 1, 1, 0, 30, 0, 0, time.UTC),
		time.Date(2025, 12, 31, 20, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 8, 6, 59, 0, 0, time.UTC),
		time.Date(2026, 3, 8, 7, 1, 0, 0, time.UTC),
		time.Date(2026, 6, 30, 18, 15, 42, 0, time.UTC),
		time.Date(2026, 10, 25, 23, 59, 59, 0, time.UTC),
		time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC),
	}
	if _, err := o.Exec(`CREATE TABLE gran_rec (id bigint PRIMARY KEY, ts ` + tsType + `, d date)`); err != nil {
		t.Fatal(err)
	}
	for i, s := range stamps {
		if _, err := o.Exec(`INSERT INTO gran_rec VALUES (?, ?, ?)`, i+1, s.Format("2006-01-02 15:04:05"), s.Format("2006-01-02")); err != nil {
			t.Fatal(err)
		}
	}
	truncs := []string{"hour", "day", "week", "month", "quarter", "year"}
	var parts []string
	for p := range numberGranularities {
		parts = append(parts, p)
	}
	for _, tz := range []string{"", "Asia/Shanghai", "America/New_York"} {
		loc := time.UTC
		if tz != "" {
			loc, _ = time.LoadLocation(tz)
		}
		for _, gran := range append(append([]string{}, truncs...), parts...) {
			for _, isDate := range []bool{false, true} {
				if isDate && gran == "hour" {
					continue // date 列不按小时分组
				}
				col := "ts"
				if isDate {
					col = "d"
				}
				name := fmt.Sprintf("%s/%s/date=%v", tz, gran, isDate)
				t.Run(name, func(t *testing.T) {
					expr := granularitySql(d, "r."+col, gran, isDate, tz)
					ds, err := o.Query(`SELECT r.id AS id, ` + expr + ` AS v FROM gran_rec r ORDER BY r.id`)
					if err != nil {
						t.Fatalf("%s: %v", expr, err)
					}
					var got, want []string
					i := 0
					for ds.First(); !ds.Eof(); ds.Next() {
						v := ds.FieldByName("v").AsInterface()
						if tm, ok := v.(time.Time); ok {
							v = tm.Format("2006-01-02 15:04:05")
						} else if b, ok := v.([]byte); ok {
							v = string(b)
						}
						g := fmt.Sprint(v)
						w := expectGranularity(stamps[i], gran, isDate, tz, loc)
						// 夏令时结束那一小时本地时刻出现两次：截断后的本地起点换回 UTC 有两个答案，取哪个由库定（PG 取
						// 后一个、MySQL 与 Go 取前一个）。两个都认。
						if alt := ambiguousAlt(stamps[i], gran, isDate, tz, loc); g != w && g == alt {
							w = alt
						}
						got, want = append(got, g), append(want, w)
						i++
					}
					if strings.Join(got, " | ") != strings.Join(want, " | ") {
						t.Errorf("%s\n得   %s\nwant %s", expr, strings.Join(got, " | "), strings.Join(want, " | "))
					}
				})
			}
		}
	}
}

func expectGranularity(s time.Time, gran string, isDate bool, tz string, loc *time.Location) string {
	local := s
	if isDate {
		local = time.Date(s.Year(), s.Month(), s.Day(), 0, 0, 0, 0, time.UTC)
	} else if tz != "" {
		local = s.In(loc)
	}
	y, m, dd := local.Date()
	if part, ok := numberGranularities[gran]; ok {
		var n int
		switch part {
		case "day":
			n = dd
		case "dow":
			n = int(local.Weekday())
		case "doy":
			n = local.YearDay()
		case "week":
			_, n = local.ISOWeek()
		case "month":
			n = int(m)
		case "quarter":
			n = (int(m)-1)/3 + 1
		case "year":
			n = y
		case "hour":
			n = local.Hour()
		case "minute":
			n = local.Minute()
		case "second":
			n = local.Second()
		}
		return fmt.Sprint(n)
	}
	l := local.Location()
	var tr time.Time
	switch gran {
	case "hour":
		tr = time.Date(y, m, dd, local.Hour(), 0, 0, 0, l)
	case "day":
		tr = time.Date(y, m, dd, 0, 0, 0, 0, l)
	case "week":
		day := time.Date(y, m, dd, 0, 0, 0, 0, l)
		tr = day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
	case "month":
		tr = time.Date(y, m, 1, 0, 0, 0, 0, l)
	case "quarter":
		tr = time.Date(y, time.Month((int(m)-1)/3*3+1), 1, 0, 0, 0, 0, l)
	case "year":
		tr = time.Date(y, 1, 1, 0, 0, 0, 0, l)
	}
	if isDate {
		return tr.Format("2006-01-02") + " 00:00:00"
	}
	return tr.UTC().Format("2006-01-02 15:04:05")
}

// ambiguousAlt：截断后的本地起点在夏令时结束时出现两次时，另一个 UTC 答案（没有歧义时回空串）。
func ambiguousAlt(s time.Time, gran string, isDate bool, tz string, loc *time.Location) string {
	if isDate || tz == "" {
		return ""
	}
	w := expectGranularity(s, gran, isDate, tz, loc)
	if _, ok := numberGranularities[gran]; ok {
		return ""
	}
	t1, err := time.ParseInLocation("2006-01-02 15:04:05", w, time.UTC)
	if err != nil {
		return ""
	}
	t2 := t1.Add(time.Hour)
	if t1.In(loc).Format("15:04") == t2.In(loc).Format("15:04") {
		return t2.Format("2006-01-02 15:04:05")
	}
	return ""
}

// 库里没装时区表时退回此刻的固定偏移（见 dialect_mysql_granularity.go）。
func TestFixedOffset(t *testing.T) {
	if got := fixedOffset("Asia/Shanghai"); got != "+08:00" {
		t.Errorf("Asia/Shanghai = %s", got)
	}
	if got := fixedOffset("Asia/Kolkata"); got != "+05:30" {
		t.Errorf("Asia/Kolkata = %s", got)
	}
	if got := fixedOffset("UTC"); got != "+00:00" {
		t.Errorf("UTC = %s", got)
	}
	if got := fixedOffset("America/New_York"); got != "-05:00" && got != "-04:00" {
		t.Errorf("America/New_York = %s", got)
	}
	m := &mysql{}
	st := &mysqlTzState{}
	st.once.Do(func() {}) // 不去查库
	st.ok = false
	mysqlTzOnce.Store(m, st)
	if got := m.convertTz("x", "", "Asia/Shanghai", true); got != "CONVERT_TZ(x, '+00:00', '+08:00')" {
		t.Errorf("没有时区表时：%s", got)
	}
}
