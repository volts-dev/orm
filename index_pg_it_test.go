package orm

import (
	"context"
	stdErrors "errors"
	"fmt"
	"testing"

	"github.com/volts-dev/dataset"
	ormerr "github.com/volts-dev/orm/errors"
)

// PostgreSQL 上的部分索引 / 表达式索引端到端集成。连不上库自动 Skip。
//
// 与 SQLite 端到端那份互补：这里验证的是**真 PG** 上约束真的生效、pg_indexes 反查回来
// 的表达式/谓词能被认成"同一条声明"（幂等，不重建），以及改了谓词会 DROP 旧建新。

const pgITIndexDB = "orm_index_it"

func newPgIndexOrm(t *testing.T) *TOrm {
	t.Helper()
	ds := &TDataSource{DbType: "postgres", Host: "localhost", Port: "5432",
		UserName: "postgres", Password: "postgres", DbName: "test_orm", SSLMode: "disable"}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	if _, err := o.Exec(`DROP TABLE IF EXISTS pgi_job`); err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	t.Cleanup(func() { o.Exec(`DROP TABLE IF EXISTS pgi_job`) })
	return o
}

type pgiJob struct {
	TModel   `table:"name('pgi_job')"`
	Id       int64  `field:"pk autoincr"`
	TenantId int64  `field:"int()"`
	JobKey   string `field:"varchar(64)"`
	State    string `field:"varchar(16)"`
	Name     string `field:"varchar(64)"`
}

func (self *pgiJob) OnBuildFields() error {
	b := self.Builder()
	b.SetPartialUniqueIndex("state = 'pending'", "tenant_id", "job_key")
	b.SetUniqueExprIndex("lower(name)")
	b.SetPartialIndex("state = 'done'", "name")
	return b.Err()
}

type pgiJobV2 struct {
	TModel   `table:"name('pgi_job')"`
	Id       int64  `field:"pk autoincr"`
	TenantId int64  `field:"int()"`
	JobKey   string `field:"varchar(64)"`
	State    string `field:"varchar(16)"`
	Name     string `field:"varchar(64)"`
}

func (self *pgiJobV2) OnBuildFields() error {
	b := self.Builder()
	b.SetPartialUniqueIndex("state = 'queued'", "tenant_id", "job_key")
	b.SetUniqueExprIndex("lower(name)")
	return b.Err()
}

func TestPGIndex_PartialAndExpr_ConstraintsWork(t *testing.T) {
	o := newPgIndexOrm(t)
	if _, err := o.SyncModel("", new(pgiJob)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	c := func(tenant int64, key, state, name string) error {
		_, err := o.Model("pgi.job").Create(map[string]any{
			"tenant_id": tenant, "job_key": key, "state": state, "name": name})
		return err
	}
	if err := c(1, "k1", "pending", "Alice"); err != nil {
		t.Fatal(err)
	}
	if err := c(1, "k1", "done", "Bob"); err != nil {
		t.Fatalf("谓词外的行不该受部分唯一约束：%v", err)
	}
	if err := c(1, "k1", "pending", "Carol"); err == nil || !stdErrors.Is(err, ormerr.ErrDuplicate) {
		t.Fatalf("pending 内重复应 ErrDuplicate，得到 %v", err)
	}
	if err := c(2, "k2", "pending", "alice"); err == nil || !stdErrors.Is(err, ormerr.ErrDuplicate) {
		t.Fatalf("lower(name) 唯一应拦下 alice/Alice，得到 %v", err)
	}
	if err := c(2, "k2", "pending", "Dave"); err != nil {
		t.Fatal(err)
	}
}

func TestPGIndex_IntrospectionIdempotent(t *testing.T) {
	o := newPgIndexOrm(t)
	if _, err := o.SyncModel("", new(pgiJob)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	model, _ := o.GetModel("pgi.job")
	declared := model.GetIndexes()
	if len(declared) != 3 {
		t.Fatalf("应声明 3 条索引，得到 %d", len(declared))
	}

	assertPresentAndEqual := func(label string) {
		t.Helper()
		got, err := o.dialect.GetIndexes(context.Background(), nil, "pgi_job")
		if err != nil {
			t.Fatalf("%s GetIndexes: %v", label, err)
		}
		for _, idx := range declared {
			db := got[idx.GetName("pgi_job")]
			if db == nil {
				t.Fatalf("%s: 库里缺 %s；库里有 %v", label, idx.GetName("pgi_job"), keysOf(got))
			}
			if !db.Equal(idx) {
				t.Fatalf("%s: %s 判不等（会每次重建）：db=%+v decl=%+v", label, db.Name, db, idx)
			}
			if idx.IsPartial() && db.Where == "" {
				t.Errorf("%s: 部分索引 %s 反查无谓词", label, db.Name)
			}
			if idx.HasExprs() && !db.HasExprs() {
				t.Errorf("%s: 表达式索引 %s 反查无表达式：%+v", label, db.Name, db)
			}
		}
	}
	assertPresentAndEqual("首次")

	// 二次同步不应有任何 DROP/CREATE —— 用一个能计数 DDL 的探针不方便，这里退而验证
	// 集合与判等仍成立（不判等就必然重建）。
	if _, err := o.SyncModel("", new(pgiJob)); err != nil {
		t.Fatalf("二次 SyncModel: %v", err)
	}
	assertPresentAndEqual("二次")
}

func TestPGIndex_DefinitionChangeRebuilds(t *testing.T) {
	// 用**两个 orm 实例**对同一物理库先后同步，模拟进程重启：旧进程声明 v1，新进程
	// 声明 v2（谓词 pending→queued）。新实例启动时先内省到 v1 的旧索引（fromDb），
	// 再登记 v2 新声明，据此把过期定义 DROP。同实例内重复声明同一模型不是真实场景。
	o1 := newPgIndexOrm(t) // 建表 + 注册 cleanup（DROP TABLE）
	if _, err := o1.SyncModel("", new(pgiJob)); err != nil {
		t.Fatalf("SyncModel v1: %v", err)
	}
	v1, _ := o1.GetModel("pgi.job")
	var oldPartial string
	for _, idx := range v1.GetIndexes() {
		if idx.IsPartial() && idx.Type == UniqueType {
			oldPartial = idx.GetName("pgi_job")
		}
	}
	if oldPartial == "" {
		t.Fatal("v1 应有唯一部分索引")
	}

	ds := &TDataSource{DbType: "postgres", Host: "localhost", Port: "5432",
		UserName: "postgres", Password: "postgres", DbName: "test_orm", SSLMode: "disable"}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	if _, err := o.SyncModel("", new(pgiJobV2)); err != nil {
		t.Fatalf("SyncModel v2: %v", err)
	}
	got, err := o.dialect.GetIndexes(context.Background(), nil, "pgi_job")
	if err != nil {
		t.Fatal(err)
	}
	if _, still := got[oldPartial]; still {
		t.Fatalf("旧谓词索引 %s 应被删除；库里有 %v", oldPartial, keysOf(got))
	}
	v2, _ := o.GetModel("pgi.job")
	for _, idx := range v2.GetIndexes() {
		if got[idx.GetName("pgi_job")] == nil {
			t.Fatalf("新声明 %s 没建出来；库里有 %v", idx.GetName("pgi_job"), keysOf(got))
		}
	}
	// 行为跟着新谓词
	c := func(key, state, name string) error {
		_, err := o.Model("pgi.job").Create(map[string]any{"tenant_id": 1, "job_key": key, "state": state, "name": name})
		return err
	}
	if err := c("k", "pending", "n1"); err != nil {
		t.Fatal(err)
	}
	if err := c("k", "pending", "n2"); err != nil {
		t.Fatalf("pending 已不在谓词内：%v", err)
	}
	if err := c("q", "queued", "n3"); err != nil {
		t.Fatal(err)
	}
	if err := c("q", "queued", "n4"); err == nil || !stdErrors.Is(err, ormerr.ErrDuplicate) {
		t.Fatalf("queued 在新谓词内应冲突，得到 %v", err)
	}
}

// PG 上验证 pg_indexes.indexdef 的解析确实抽出了谓词与表达式（不是靠名字蒙对）。
func TestPGIndex_RawIndexdefParsed(t *testing.T) {
	o := newPgIndexOrm(t)
	if _, err := o.SyncModel("", new(pgiJob)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	ds, err := o.Query(`SELECT indexname, indexdef FROM pg_indexes WHERE tablename='pgi_job' ORDER BY indexname`)
	if err != nil {
		t.Fatal(err)
	}
	var sawPartial, sawExpr bool
	ds.Range(func(_ int, rec *dataset.TRecordSet) error {
		def := rec.FieldByName("indexdef").AsString()
		if _, e, w, ok := parseIndexDef(def); ok {
			if w != "" {
				sawPartial = true
			}
			if len(e) > 0 {
				sawExpr = true
			}
		}
		return nil
	})
	if !sawPartial {
		t.Error("pg_indexes 里应有带 WHERE 的部分索引")
	}
	if !sawExpr {
		t.Error("pg_indexes 里应有 lower(name) 表达式索引")
	}
	_ = fmt.Sprint
}
