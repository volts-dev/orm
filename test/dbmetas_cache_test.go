package test

import (
	"path/filepath"
	"testing"

	"github.com/volts-dev/orm"
)

type (
	CacheModelA struct {
		orm.TModel `table:"name('cache_model_a')"`
		Id         int64  `field:"pk autoincr"`
		Name       string `field:"varchar()"`
	}

	CacheModelB struct {
		orm.TModel `table:"name('cache_model_b')"`
		Id         int64  `field:"pk autoincr"`
		Title      string `field:"varchar()"`
	}
)

// DBMetas 现在按 schema 缓存整库反查结果,靠 DDL 递增 epoch 失效(见 TOrm.metaCache)。
// 本用例守护:缓存不能把"刚建出来的表"藏起来——否则重复 SyncModel 会以为表还不
// 存在而再次 CREATE,在真实库上直接报"表已存在"。第二次 SyncModel 必须走到
// _alterTable(no-op)而不是重新建表。
func TestDBMetasCacheInvalidatesOnCreate(t *testing.T) {
	ds := &orm.TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "cache.db")}
	o, err := orm.New(orm.WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}

	// 第一次:表不存在 → 建表(内部会 bump epoch)。
	if _, err := o.SyncModel("test", new(CacheModelA)); err != nil {
		t.Fatalf("SyncModel #1: %v", err)
	}

	// 第二次:若缓存未随建表失效,DBMetas 会返回建表前的旧快照(表缺失),
	// 于是再次 CREATE 同名表 → sqlite 报错。失效正确则应无害通过。
	if _, err := o.SyncModel("test", new(CacheModelA)); err != nil {
		t.Fatalf("SyncModel #2 (idempotent re-sync) should succeed, got: %v", err)
	}

	// 之后引入一张全新表,同样必须能被建出并可用——证明缓存没有卡住后续建表。
	if _, err := o.SyncModel("test", new(CacheModelB)); err != nil {
		t.Fatalf("SyncModel(B): %v", err)
	}
	if _, err := o.Exec("INSERT INTO cache_model_b(title) VALUES ('x')"); err != nil {
		t.Fatalf("insert into freshly-synced table B failed: %v", err)
	}
}

// DisableSchemaSync=true 时 SyncModel 走生产快路径:注册模型但不发任何 DDL。
// 守护:模型对 osv 可见(GetModel 成功),但表确实没被建出来。
func TestDisableSchemaSyncSkipsDDL(t *testing.T) {
	ds := &orm.TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "noschema.db")}
	o, err := orm.New(orm.WithDataSource(ds), orm.WithDisableSchemaSync(true))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}

	names, err := o.SyncModel("test", new(CacheModelA))
	if err != nil {
		t.Fatalf("SyncModel (disabled): %v", err)
	}
	if len(names) != 1 {
		t.Fatalf("expected 1 registered model name, got %v", names)
	}

	// 模型已注册,元数据可用。
	if _, err := o.GetModel(names[0]); err != nil {
		t.Fatalf("GetModel(%q) after disabled sync: %v", names[0], err)
	}

	// 但没有发过 CREATE,表不应存在。
	exist, err := o.IsTableExist("cache_model_a")
	if err != nil {
		t.Fatalf("IsTableExist: %v", err)
	}
	if exist {
		t.Fatalf("DisableSchemaSync should not have created the table, but it exists")
	}
}
