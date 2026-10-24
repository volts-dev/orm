package orm

import (
	"testing"
)

// MySQL 上 schema 即库（P4，2026-10-25）：SetSchema 的会话建表、建索引、反查都要落在那个库里。此前建表
// 与反查写死 TABLE_SCHEMA = DbName——vectors 的 S 模型（system schema）全建进了默认库，去掉探针的
// SystemSchema 补丁后 kylin 启动即 FATAL（system.sys_tenant doesn't exist）。
// PG 的 public 是默认 schema，在 MySQL 上对应当前库：显式传 "public" 也落当前库。
// 起了名字的索引（index('idx_x')，stock 的 barcode.nomenclature 就是）：库里的名字是加工过的
// IDX_<表缩写>_idx_x。探针在已有的 system 库上重启时报 1061 Duplicate key name——反查没认出来又建一遍。
type SchemaNamedIdx struct {
	TModel    `table:"name('schema_named_idx')"`
	Id        int64 `field:"pk autoincr"`
	CompanyId int64 `field:"bigint() index('idx_schema_named_idx_company_id')"`
}

func TestMySQL_SchemaIsDatabase(t *testing.T) {
	skipUnlessDialect(t, "mysql")
	ds := testDataSource(t, "")
	other := ds.DbName + "_sys"
	admin, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := admin.Exec("CREATE DATABASE `" + other + "`"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Exec("DROP DATABASE IF EXISTS `" + other + "`"); admin.Close() })

	tablesIn := func(o *TOrm, db string) map[string]bool {
		t.Helper()
		s := o.NewSession()
		defer s.Close()
		rs, err := s.Query("SELECT TABLE_NAME AS t FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA = ?", db)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		rs.First()
		for !rs.Eof() {
			out[rs.FieldByName("t").AsString()] = true
			rs.Next()
		}
		return out
	}

	for round := 0; round < 2; round++ {
		o, err := New(WithDataSource(ds))
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		rec := &ddlRecorder{}
		o.AddHook(rec)
		if _, err := o.SyncModel("test", new(SchemaDdlPoc), new(SchemaNamedIdx)); err != nil {
			t.Fatalf("默认库 SyncModel: %v", err)
		}
		sess := o.NewSession()
		sess.SetSchema(other)
		if _, err := sess.SyncModel("test", new(SchemaDdlPoc), new(SchemaNamedIdx)); err != nil {
			t.Fatalf("%s SyncModel: %v", other, err)
		}
		sess.Close()
		if round == 1 && len(rec.ddl) > 0 {
			t.Errorf("重启后仍发 DDL：%q", rec.ddl)
		}

		if !tablesIn(o, ds.DbName)["schema_ddl_poc"] || !tablesIn(o, other)["schema_ddl_poc"] {
			t.Fatalf("两个库里应各有一张 schema_ddl_poc：默认库 %v，%s %v", tablesIn(o, ds.DbName), other, tablesIn(o, other))
		}
		for _, sch := range []string{"", other} {
			s := o.NewSession()
			s.SetSchema(sch)
			idx, err := o.dialect.GetIndexes(s.context, s, "schema_ddl_poc")
			s.Close()
			if err != nil {
				t.Fatal(err)
			}
			if len(idx) != 1 {
				t.Errorf("schema %q 里 schema_ddl_poc 的索引 %d 条，想要 1：%v", sch, len(idx), idx)
			}
		}

		if round == 0 {
			// 两个库各写一行，ORM 读写跟着会话的 schema 走。
			for _, sch := range []string{"", other} {
				s := o.NewSession()
				s.SetSchema(sch)
				if _, err := s.Model("schema.ddl.poc").Create(map[string]any{"code": "in-" + sch}); err != nil {
					t.Fatalf("schema %q Create: %v", sch, err)
				}
				s.Close()
			}
		}
		for _, sch := range []string{"", other, "public"} {
			s := o.NewSession()
			s.SetSchema(sch)
			rs, err := s.Model("schema.ddl.poc").Limit(-1).Read()
			s.Close()
			if err != nil {
				t.Fatalf("schema %q Read: %v", sch, err)
			}
			want := "in-" + sch
			if sch == "public" {
				want = "in-"
			}
			if rs.Count() != 1 || rs.FieldByName("code").AsString() != want {
				t.Errorf("schema %q 读到 %d 行 %q，想要一行 %q", sch, rs.Count(), rs.FieldByName("code").AsString(), want)
			}
		}
		o.Close()
	}
}

// MySQL 建索引没有 IF NOT EXISTS：两路同时同步同一个模型，都先查「不在」再各自建，后到的报 1061。
// 探针首启撞上过（kylin 启动 5 秒 FATAL：Duplicate key name 'IDX_bnomenclature_…'）。建索引的地方把它当成功。
func TestMySQL_CreateIndexRaceIsNotFatal(t *testing.T) {
	skipUnlessDialect(t, "mysql")
	o, err := New(WithDataSource(testDataSource(t, "")))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer o.Close()
	if _, err := o.SyncModel("test", new(SchemaNamedIdx)); err != nil {
		t.Fatal(err)
	}
	m, _ := o.GetModel("schema.named.idx")
	s := o.NewSession()
	defer s.Close()
	for _, idx := range m.GetIndexes() {
		// 「查过不在」之后别人已经建好了：直接再执行一遍建索引语句。
		if err := s.execIndexDDL(o.dialect.CreateIndexUniqueSql("", "schema_named_idx", idx)); err != nil {
			t.Fatalf("索引 %s 已在时再建应当成功，得 %v", idx.Name, err)
		}
	}
}
