package orm

import (
	"strings"
	"testing"
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
