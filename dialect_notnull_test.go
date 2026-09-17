package orm

import "testing"

// 数据库自己拒绝 NOT NULL 时也要点得出列名——新建路径有 ORM 自己那道必填检查，
// **更新路径没有**（显式往 required 列写空值），那一条只能靠驱动错误。
func TestNotNullColumn_SQLite(t *testing.T) {
	cases := map[string]string{
		"NOT NULL constraint failed: sys_attachment.name":      "name",
		"constraint failed\nNOT NULL constraint failed: t.col": "col",
		"UNIQUE constraint failed: t.col":                      "",
		"NOT NULL constraint failed: name":                     "name",
	}
	for msg, want := range cases {
		if got := notNullColumn(msg); got != want {
			t.Fatalf("notNullColumn(%q) = %q, want %q", msg, got, want)
		}
	}
}

func TestBadNullColumn_MySQL(t *testing.T) {
	if got := badNullColumn("Column 'name' cannot be null"); got != "name" {
		t.Fatalf("badNullColumn = %q, want name", got)
	}
	if got := badNullColumn("something else"); got != "" {
		t.Fatalf("badNullColumn = %q, want empty", got)
	}
}
