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
