package orm

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/volts-dev/orm/core"
)

// 「连续同步两次，第二次必须一条改表语句都不发」（MySQL 兼容 P0，2026-10-24）。
//
// 真实场景是进程重启：新的 TOrm 对着同一个库再 SyncModel 一遍。表结构没变，就不该有任何 DDL。
// 2026-10-24 的 MySQL 真库探针里 kylin 每次启动发 1290 条 ALTER … MODIFY（内省认不出主键、TEXT
// 长度 65535 对 0 对不上），PG / SQLite 上从没暴露——因为没有这道检查，每个方言各自"差一点点"
// 都没人知道。所以这里用一组覆盖模块里常用字段类型的模型，三个方言都跑（ORM_TEST_DB，见 testdb_test.go）。
//
// 模型里的类型按 2026-10-24 全仓字段标签的用量挑：varchar(816) many2one(800) int(542) bool(407)
// double(197) selection(192) text(150) many2many(135) one2many(103) datetime(93) date(44) html(37)
// binary(12) json(8)。

type ResyncTag struct {
	TModel `table:"name('resync_tag')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar() required"`
}

type ResyncPartner struct {
	TModel   `table:"name('resync_partner')"`
	Id       int64     `field:"pk autoincr"`
	Name     string    `field:"varchar() required index"`
	Code     string    `field:"varchar() size(16)"`
	Note     string    `field:"text()"`
	Dom      string    `field:"text() default('[]')"` // MySQL 的 TEXT 只接受表达式默认值，反查回来带 DEFAULT_GENERATED
	Url      string    `field:"varchar() unbounded"`   // 不限长 char：MySQL 上是 TEXT，模型里仍是 varchar
	Body     string    `field:"html()"`
	Qty      int64     `field:"int()"`
	Big      int64     `field:"bigint()"`
	Active   bool      `field:"bool() default(true)"`
	Price    float64   `field:"double()"`
	Ratio    float64   `field:"float()"`
	State    string    `field:"selection('{\"draft\":\"Draft\",\"done\":\"Done\"}') default('draft') index"`
	Born     time.Time `field:"date()"`
	Seen     time.Time `field:"datetime()"`
	Blob     []byte    `field:"binary()"`
	Data     string    `field:"json()"`
	ParentId int64     `field:"many2one(resync_partner) ondelete('set null')"`
	ChildIds []any     `field:"one2many(resync_partner,parent_id)"`
	TagIds   []any     `field:"many2many(resync_tag,resync_partner_tag_rel,partner_id,tag_id)"`
}

// ddlRecorder 记下执行成功的 DDL。
type ddlRecorder struct {
	mu  sync.Mutex
	ddl []string
}

var reDDL = regexp.MustCompile(`(?is)^\s*(create|alter|drop|rename|comment\s+on)\b`)

func (r *ddlRecorder) BeforeProcess(c *core.ContextHook) (context.Context, error) { return c.Ctx, nil }
func (r *ddlRecorder) AfterProcess(c *core.ContextHook) error {
	if c.Err == nil && reDDL.MatchString(c.SQL) {
		r.mu.Lock()
		r.ddl = append(r.ddl, strings.Join(strings.Fields(c.SQL), " "))
		r.mu.Unlock()
	}
	return nil
}

func TestSyncModel_SecondSyncIssuesNoDDL(t *testing.T) {
	ds := testDataSource(t, filepath.Join(t.TempDir(), "resync.db"))
	models := func() []IModel { return []IModel{new(ResyncTag), new(ResyncPartner)} }

	first, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	created := &ddlRecorder{}
	first.AddHook(created)
	if _, err := first.SyncModel("", models()...); err != nil {
		t.Fatalf("第一次 SyncModel: %v", err)
	}
	first.Close()
	if len(created.ddl) == 0 {
		t.Fatal("第一次同步一条 DDL 都没记到——钩子没挂上，下面的断言就是空转")
	}

	// 「重启」：新实例、同一个库。
	second, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("New（第二次）: %v", err)
	}
	defer second.Close()
	rec := &ddlRecorder{}
	second.AddHook(rec)
	if _, err := second.SyncModel("", models()...); err != nil {
		t.Fatalf("第二次 SyncModel: %v", err)
	}
	if len(rec.ddl) > 0 {
		t.Errorf("表结构没变，第二次同步仍发了 %d 条 DDL（%s）：", len(rec.ddl), ds.DbType)
		for _, s := range rec.ddl {
			t.Errorf("  %s", s)
		}
	}
}

// 关联表在、索引缺（老库上当年没建）：重同步要把缺的那条补上——此前每次启动的
// CREATE INDEX IF NOT EXISTS 顺手做了这件事，改成「表在就跳过」之后不能丢。
func TestSyncModel_ResyncRestoresMissingRelationIndex(t *testing.T) {
	ds := testDataSource(t, filepath.Join(t.TempDir(), "resync_idx.db"))
	models := func() []IModel { return []IModel{new(ResyncTag), new(ResyncPartner)} }
	first, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := first.SyncModel("", models()...); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	drop := `DROP INDEX "resync_partner_tag_rel_tag_id_idx"`
	if ds.DbType == "mysql" {
		drop = "DROP INDEX `resync_partner_tag_rel_tag_id_idx` ON `resync_partner_tag_rel`"
	}
	if _, err := first.Exec(drop); err != nil {
		t.Fatalf("删索引: %v", err)
	}
	first.Close()

	second, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("New（第二次）: %v", err)
	}
	defer second.Close()
	rec := &ddlRecorder{}
	second.AddHook(rec)
	if _, err := second.SyncModel("", models()...); err != nil {
		t.Fatalf("第二次 SyncModel: %v", err)
	}
	if len(rec.ddl) != 1 || !strings.Contains(rec.ddl[0], "resync_partner_tag_rel_tag_id_idx") {
		t.Fatalf("应只补回那一条索引，实际 DDL：%q", rec.ddl)
	}
}
