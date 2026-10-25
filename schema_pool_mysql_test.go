package orm

import (
	"context"
	"sync"
	"testing"

	"github.com/volts-dev/orm/core"
)

type sqlRecorder struct {
	mu  sync.Mutex
	all []string
}

func (r *sqlRecorder) BeforeProcess(c *core.ContextHook) (context.Context, error) { return c.Ctx, nil }
func (r *sqlRecorder) AfterProcess(c *core.ContextHook) error {
	r.mu.Lock()
	r.all = append(r.all, c.SQL)
	r.mu.Unlock()
	return nil
}

// MySQL 上设了 schema 的会话，裸 SQL 的未限定表名落在那个库（P4，2026-10-25，见 schema_pool.go）：此前一律落
// 默认库——探针里 PlatformSystemSession 读存储实例表 sys_blob_store 读成「表不存在」，整个进程判成没配 blob。
// 同时钉住连接状态不泄漏：schema 会话用过之后，默认会话的裸 SQL 仍落默认库。
func TestMySQL_RawSQLFollowsSessionSchema(t *testing.T) {
	skipUnlessDialect(t, "mysql")
	ds := testDataSource(t, "")
	other := ds.DbName + "_rawsys"
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rec := &sqlRecorder{}
	o.AddHook(rec) // 在 schema 池建出来之前登记：建池时要补上
	for _, q := range []string{
		"CREATE DATABASE `" + other + "`",
		"CREATE TABLE raw_where (v varchar(16))", "INSERT INTO raw_where VALUES ('default')",
		"CREATE TABLE `" + other + "`.raw_where (v varchar(16))", "INSERT INTO `" + other + "`.raw_where VALUES ('other')",
		"CREATE TABLE `" + other + "`.only_other (v int)",
	} {
		if _, err := o.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	t.Cleanup(func() { o.Exec("DROP DATABASE IF EXISTS `" + other + "`"); o.Close() })

	where := func(s *TSession) string {
		t.Helper()
		ds, err := s.Query("SELECT v FROM raw_where")
		if err != nil || ds.Count() != 1 {
			t.Fatalf("SELECT v FROM raw_where（schema %q）: %v", s.Schema, err)
		}
		return ds.FieldByName("v").AsString()
	}

	for round := 0; round < 3; round++ { // 多轮：连接回池后再借，状态不能串
		s := o.NewSession()
		s.SetSchema(other)
		if got := where(s); got != "other" {
			t.Errorf("schema=%s 的自动提交会话，裸 SQL 读到 %q", other, got)
		}
		if _, err := s.Exec("INSERT INTO only_other VALUES (?)", round); err != nil {
			t.Errorf("schema=%s 的会话写未限定表: %v", other, err)
		}
		if err := s.Begin(); err != nil {
			t.Fatal(err)
		}
		if got := where(s); got != "other" {
			t.Errorf("schema=%s 的事务里，裸 SQL 读到 %q", other, got)
		}
		s.Commit()
		s.Close()

		p := o.NewPrivilegedSession()
		p.SetSchema(other)
		if got := where(p); got != "other" {
			t.Errorf("schema=%s 的特权会话，裸 SQL 读到 %q", other, got)
		}
		p.Close()

		d := o.NewSession()
		if got := where(d); got != "default" {
			t.Errorf("用过 schema 会话之后，默认会话的裸 SQL 读到 %q（连接状态串了）", got)
		}
		d.SetSchema("public") // 与默认库同义
		if got := where(d); got != "default" {
			t.Errorf("schema=public 的会话读到 %q", got)
		}
		d.Close()
	}
	if len(o.SchemaPoolStats()) == 0 {
		t.Error("SchemaPoolStats 是空的：schema 会话没走单独的池？")
	}
	sawOther := false
	for _, q := range rec.all {
		if q == "INSERT INTO only_other VALUES (?)" {
			sawOther = true
		}
	}
	if !sawOther {
		t.Error("schema 池上的语句没经过 AddHook 登记的钩子")
	}
}
