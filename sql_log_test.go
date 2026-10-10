package orm

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// 成批写入的 SQL 与参数在日志里要截断：一条多行 INSERT 原样记下来是几十万字节一行。
func TestFormatSqlLog_Truncates(t *testing.T) {
	sql := "INSERT INTO t (a) VALUES " + strings.Repeat("(?),", 5000) + "(?)"
	args := make([]any, 5001)
	for i := range args {
		args[i] = i
	}
	got := formatSqlLog(1.5, sql, args)
	if len(got) > maxLoggedSqlLen+600 {
		t.Fatalf("log line is %d bytes", len(got))
	}
	if !strings.Contains(got, "(+4969 args)") || !strings.Contains(got, "bytes)") {
		t.Fatalf("missing truncation markers: %s", got[len(got)-120:])
	}

	// 截断点落在多字节字符中间时退回字符边界。
	cn := strings.Repeat("器", maxLoggedSqlLen) // 每个 3 字节
	if out := formatSqlLog(0, cn, nil); !utf8.ValidString(out) {
		t.Fatal("truncated log line is not valid UTF-8")
	}

	// 短的原样。
	if got := formatSqlLog(0.25, "SELECT 1 WHERE a=?", []any{7}); got != "[SQL][0.25 ms] SELECT 1 WHERE a=? [args] [7]" {
		t.Fatalf("short = %q", got)
	}
}

// ShowSql 关着时只记达到阈值的慢语句（以 Warn），快的一条都不记；阈值为 0 时全不记。
// 生产上关 ShowSql 是为了不让每条 SQL 一行灌满 syslog，但慢查询不能跟着丢。
func TestSqlLogKind(t *testing.T) {
	o := &TOrm{config: &Config{SlowSqlThreshold: 20 * time.Millisecond}}
	cases := []struct {
		show bool
		th   time.Duration
		d    time.Duration
		want int
	}{
		{false, 20 * time.Millisecond, 5 * time.Millisecond, sqlLogNone},
		{false, 20 * time.Millisecond, 20 * time.Millisecond, sqlLogSlow},
		{false, 20 * time.Millisecond, time.Second, sqlLogSlow},
		{false, 0, time.Second, sqlLogNone},
		{true, 0, 0, sqlLogAll},
		{true, 20 * time.Millisecond, time.Second, sqlLogAll},
	}
	for _, c := range cases {
		o.config.ShowSql, o.config.SlowSqlThreshold = c.show, c.th
		if got := o.sqlLogKind(c.d); got != c.want {
			t.Errorf("show=%v 阈值=%v 耗时=%v → %d，期望 %d", c.show, c.th, c.d, got, c.want)
		}
	}
}
