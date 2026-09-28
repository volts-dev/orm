package orm

import (
	"context"
	"testing"

	_ "github.com/lib/pq"
)

type addfieldPgProbe struct {
	TModel `table:"name('addfield_pg_probe')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(32)"`
}

// postgres 上的 AddField：列要补到**每一个**有这张表的 schema，带 index 的要把索引建出来，
// many2many 要把关联表建出来。
//
// 多 schema 是真实形态：vectors 的 VectorsSystem 租户把模型物化进自己的 `system` schema。
// 只补默认 schema 的话，那个租户读这个模型从此报 column does not exist。
func TestAddField_Postgres_AllSchemasIndexAndM2M(t *testing.T) {
	bootstrap := &TDataSource{DbType: "postgres", Host: "localhost", Port: "5432", UserName: "postgres", Password: "postgres", DbName: "test_orm", SSLMode: "disable"}
	boot, err := New(WithDataSource(bootstrap))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	// 独占一个库，理由同 addcolumn_test.go（与别的包并行跑整库内省会互相撞）。
	boot.Exec(`CREATE DATABASE test_orm_addfield`)

	ds := &TDataSource{DbType: "postgres", Host: "localhost", Port: "5432", UserName: "postgres", Password: "postgres", DbName: "test_orm_addfield", SSLMode: "disable"}

	// 清场必须在**另一个** TOrm 上做、且早于建被测的那个：New() 会把库里已有的表反查进
	// osv，上一轮留下的表带着 warn_msg，于是被测模型一注册就"已经有" warn_msg，
	// AddField 什么都不做也能通过下面每一条断言。
	cleaner, err := New(WithDataSource(ds))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	for _, q := range []string{
		`DROP SCHEMA IF EXISTS addfield_s2 CASCADE`,
		`DROP TABLE IF EXISTS public.addfield_pg_probe_tag_rel`,
		`DROP TABLE IF EXISTS public.addfield_pg_probe CASCADE`,
	} {
		if _, err := cleaner.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}

	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	names, err := o.SyncModel("test", new(addfieldPgProbe))
	if err != nil || len(names) != 1 {
		t.Fatalf("SyncModel: %v %v", names, err)
	}
	modelName := names[0]

	// 第二份表：模拟 schema 隔离租户物化出来的那一份。
	for _, q := range []string{
		`CREATE SCHEMA addfield_s2`,
		`CREATE TABLE addfield_s2.addfield_pg_probe (LIKE public.addfield_pg_probe INCLUDING ALL)`,
	} {
		if _, err := o.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	defer o.Exec(`DROP SCHEMA IF EXISTS addfield_s2 CASCADE`)

	if err := o.Freeze(context.Background()); err != nil {
		t.Fatalf("Freeze: %v", err)
	}

	if added, err := o.AddField(modelName, "warn_msg", "varchar(64) index title('Warning')"); err != nil || !added {
		t.Fatalf("AddField varchar+index: added=%v err=%v（added=false 说明模型上早就有这个字段，本用例前提不成立）", added, err)
	}
	for _, schema := range []string{"public", "addfield_s2"} {
		ds, err := o.Query(`SELECT 1 FROM information_schema.columns WHERE table_schema = ? AND table_name = 'addfield_pg_probe' AND column_name = 'warn_msg'`, schema)
		if err != nil {
			t.Fatal(err)
		}
		if ds.Count() == 0 {
			t.Fatalf("schema %s 里没有补出 warn_msg 列", schema)
		}
		ds, err = o.Query(`SELECT indexname FROM pg_indexes WHERE schemaname = ? AND tablename = 'addfield_pg_probe' AND indexdef LIKE '%warn_msg%'`, schema)
		if err != nil {
			t.Fatal(err)
		}
		if ds.Count() == 0 {
			t.Fatalf("schema %s 里没有建出 warn_msg 的索引", schema)
		}
	}

	// 写读一轮：走的是普通的会话路径，不是裸 SQL。
	if _, err := o.Model(modelName).Create(map[string]any{"name": "n", "warn_msg": "careful"}); err != nil {
		t.Fatalf("create with the new column: %v", err)
	}
	got, err := o.Model(modelName).Domain(`[('warn_msg','=','careful')]`).Read()
	if err != nil || got.Count() != 1 {
		t.Fatalf("read by the new column: %v (%d rows)", err, got.Count())
	}

	// 幂等：再补一次不报错（列已在、索引已在）。
	if added, err := o.AddField(modelName, "warn_msg", "varchar(64) index title('Warning')"); err != nil || added {
		t.Fatalf("second AddField: added=%v err=%v", added, err)
	}

	// many2many：关联表要建出来，且能写进去。
	if _, err := o.AddField(modelName, "tag_ids", "many2many(addfield.pg.probe,addfield_pg_probe_tag_rel,probe_id,tag_id)"); err != nil {
		t.Fatalf("AddField many2many: %v", err)
	}
	rel, err := o.Query(`SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'addfield_pg_probe_tag_rel'`)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Count() == 0 {
		t.Fatal("many2many 的关联表没有建出来")
	}
}

type addfieldRestartProbe struct {
	TModel `table:"name('addfield_restart_probe')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(32)"`
}

// 重启之后再补同一批字段：列已经在库里，New() 的反查把它们带进了模型（只有 SQL 类型，
// 没有标题，多对一退化成 BIGINT）。AddField 必须按声明重建它们，而不是当成「已声明」。
//
// 真栈 2026-09-28 撞到：kylin 重启后 res.partner.sale_warn_msg 的标题变成空串 ——
// AddField 看到同名同类型的反查字段就返回了 no-op。多对一更糟：反查出来是 BIGINT，
// AddField 会当成「换类型」拒绝，字段从此是一个裸整数。
func TestAddField_Postgres_RedeclaresIntrospectedColumnsAfterRestart(t *testing.T) {
	ds := &TDataSource{DbType: "postgres", Host: "localhost", Port: "5432", UserName: "postgres", Password: "postgres", DbName: "test_orm_addfield", SSLMode: "disable"}
	cleaner, err := New(WithDataSource(ds))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	if _, err := cleaner.Exec(`DROP TABLE IF EXISTS public.addfield_restart_probe CASCADE`); err != nil {
		t.Fatal(err)
	}

	decl := map[string]string{
		"warn_msg":  "text() title('Warning Message')",
		"parent_id": "many2one(addfield.restart.probe) title('Parent')",
	}

	// 第一次运行：建表、补字段（列落库）。
	first, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatal(err)
	}
	names, err := first.SyncModel("test", new(addfieldRestartProbe))
	if err != nil || len(names) != 1 {
		t.Fatalf("SyncModel: %v %v", names, err)
	}
	modelName := names[0]
	if err := first.Freeze(context.Background()); err != nil {
		t.Fatal(err)
	}
	for f, tag := range decl {
		if _, err := first.AddField(modelName, f, tag); err != nil {
			t.Fatalf("first run AddField %s: %v", f, err)
		}
	}

	// 「重启」：新的 TOrm 反查到带着 warn_msg / parent_id 两列的表。
	second, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.SyncModel("test", new(addfieldRestartProbe)); err != nil {
		t.Fatal(err)
	}
	if err := second.Freeze(context.Background()); err != nil {
		t.Fatal(err)
	}
	for f, tag := range decl {
		added, err := second.AddField(modelName, f, tag)
		if err != nil {
			t.Fatalf("after restart AddField %s: %v", f, err)
		}
		if !added {
			t.Fatalf("after restart AddField %s 返回 added=false：反查来的字段被当成了已声明", f)
		}
	}
	m := second.Model(modelName).Statement.Model
	if got := m.GetFieldByName("warn_msg").Label(); got != "Warning Message" {
		t.Fatalf("重启后 warn_msg 的标题 = %q", got)
	}
	if got := m.GetFieldByName("parent_id").TypeName(); got != TYPE_M2O {
		t.Fatalf("重启后 parent_id 的类型 = %q，应为 many2one", got)
	}
	// 结构体上声明过的字段不受影响：仍然拒绝改类型。
	if _, err := second.AddField(modelName, "name", "int()"); err == nil {
		t.Fatal("结构体字段 name 被 AddField 改了类型")
	}
}
