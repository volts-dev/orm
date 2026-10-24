package orm

import (
	"strings"
	"testing"
)

// MySQL 的会话模式是连接约定的一部分（datasource.go MySQLSqlMode）：orm 内部和各模块手写的 SQL 都按
// PG 的写法——双引号是标识符、|| 是拼接。哪条连接没带上，前者报语法错，后者悄悄算出 0。
// 池里每条连接都要有：连接串参数在每次建连时由驱动 SET。
func TestMySQL_SessionModesMatchPostgres(t *testing.T) {
	skipUnlessDialect(t, "mysql")
	o, err := New(WithDataSource(testDataSource(t, "")), WithMaxOpenConns(3))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer o.Close()
	if _, err := o.Exec(`CREATE TABLE "mode_probe" ("id" int PRIMARY KEY, "name" varchar(16))`); err != nil {
		t.Fatalf("双引号标识符建表: %v", err)
	}
	if _, err := o.Exec(`INSERT INTO "mode_probe" ("id", "name") VALUES (1, 'a')`); err != nil {
		t.Fatal(err)
	}
	// 同时占住三条连接，各问一遍。
	sessions := make([]*TSession, 3)
	for i := range sessions {
		sessions[i] = o.NewSession()
		if err := sessions[i].Begin(); err != nil {
			t.Fatal(err)
		}
	}
	for i, s := range sessions {
		ds, err := s.Query(`SELECT @@SESSION.sql_mode AS m, "name" || '-' || 'b' AS c FROM "mode_probe" WHERE "id" = 1`)
		if err != nil {
			t.Fatalf("连接 %d: %v", i, err)
		}
		mode := ds.FieldByName("m").AsString()
		for _, want := range []string{"ANSI_QUOTES", "PIPES_AS_CONCAT", "STRICT_TRANS_TABLES"} {
			if !strings.Contains(mode, want) {
				t.Errorf("连接 %d 的 sql_mode 缺 %s：%s", i, want, mode)
			}
		}
		if c := ds.FieldByName("c").AsString(); c != "a-b" {
			t.Errorf("连接 %d：'a' || '-' || 'b' 得 %q，想要 a-b", i, c)
		}
	}
	for _, s := range sessions {
		s.Rollback(nil)
		s.Close()
	}
}
