package orm

import (
	"context"
	stdErrors "errors"
	"path/filepath"
	"testing"

	ormerr "github.com/volts-dev/orm/errors"
)

/*
部分唯一索引 / 表达式索引在真库（SQLite）上的端到端行为：约束真的生效、反查认得出、
二次 SyncModel 不重建、改了定义会重建。
*/

type piJob struct {
	TModel   `table:"name('pi_job')"`
	Id       int64  `field:"pk autoincr"`
	TenantId int64  `field:"int()"`
	JobKey   string `field:"varchar(64)"`
	State    string `field:"varchar(16)"`
	Name     string `field:"varchar(64)"`
}

func (self *piJob) OnBuildFields() error {
	b := self.Builder()
	b.SetPartialUniqueIndex("state = 'pending'", "tenant_id", "job_key")
	b.SetUniqueExprIndex("lower(name)")
	b.SetPartialIndex("state = 'done'", "name")
	return b.Err()
}

// 同一张表、谓词改成 queued —— 模拟"模型作者改了声明再上线"。
type piJobV2 struct {
	TModel   `table:"name('pi_job')"`
	Id       int64  `field:"pk autoincr"`
	TenantId int64  `field:"int()"`
	JobKey   string `field:"varchar(64)"`
	State    string `field:"varchar(16)"`
	Name     string `field:"varchar(64)"`
}

func (self *piJobV2) OnBuildFields() error {
	b := self.Builder()
	b.SetPartialUniqueIndex("state = 'queued'", "tenant_id", "job_key")
	b.SetUniqueExprIndex("lower(name)")
	return b.Err()
}

func openPiOrm(t *testing.T, dbPath string, m IModel) *TOrm {
	t.Helper()
	o, err := New(WithDataSource(&TDataSource{DbType: "sqlite", DbName: dbPath}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.SyncModel("", m); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	return o
}

func dbIndexes(t *testing.T, o *TOrm, table string) map[string]*TIndex {
	t.Helper()
	got, err := o.dialect.GetIndexes(context.Background(), nil, table)
	if err != nil {
		t.Fatalf("GetIndexes: %v", err)
	}
	return got
}

func TestPartialIndex_SQLite_ConstraintsWork(t *testing.T) {
	o := openPiOrm(t, filepath.Join(t.TempDir(), "pi.db"), new(piJob))

	create := func(tenant int64, key, state, name string) error {
		_, err := o.Model("pi.job").Create(map[string]any{
			"tenant_id": tenant, "job_key": key, "state": state, "name": name})
		return err
	}
	if err := create(1, "k1", "pending", "Alice"); err != nil {
		t.Fatal(err)
	}
	// 同键、不在谓词内 → 不冲突
	if err := create(1, "k1", "done", "Bob"); err != nil {
		t.Fatalf("谓词外的行不该受部分唯一约束：%v", err)
	}
	// 同键、谓词内 → 冲突
	if err := create(1, "k1", "pending", "Carol"); err == nil || !stdErrors.Is(err, ormerr.ErrDuplicate) {
		t.Fatalf("谓词内重复应报 ErrDuplicate，得到 %v", err)
	}
	// 表达式唯一：大小写不同的同名 → 冲突
	if err := create(2, "k2", "pending", "alice"); err == nil || !stdErrors.Is(err, ormerr.ErrDuplicate) {
		t.Fatalf("lower(name) 唯一应拦下 alice/Alice，得到 %v", err)
	}
	if err := create(2, "k2", "pending", "Dave"); err != nil {
		t.Fatal(err)
	}
}

func TestPartialIndex_SQLite_IntrospectionAndIdempotentSync(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pi.db")
	o := openPiOrm(t, dbPath, new(piJob))

	model, err := o.GetModel("pi.job")
	if err != nil {
		t.Fatal(err)
	}
	declared := model.GetIndexes()
	if len(declared) != 3 {
		t.Fatalf("模型应声明 3 条索引，得到 %d：%v", len(declared), declared)
	}

	check := func(label string) {
		t.Helper()
		got := dbIndexes(t, o, "pi_job")
		for name, idx := range declared {
			dbIdx := got[idx.GetName("pi_job")]
			if dbIdx == nil {
				t.Fatalf("%s: 库里缺索引 %s（声明名 %s）；库里有：%v", label, idx.GetName("pi_job"), name, keysOf(got))
			}
			if !dbIdx.Equal(idx) {
				t.Fatalf("%s: 库里的 %s 与模型声明判不等——每次启动都会 DROP/CREATE：db=%+v model=%+v", label, dbIdx.Name, dbIdx, idx)
			}
			if idx.IsPartial() && dbIdx.Where == "" {
				t.Errorf("%s: 部分索引 %s 反查回来没有谓词", label, dbIdx.Name)
			}
			if idx.HasExprs() && !dbIdx.HasExprs() {
				t.Errorf("%s: 表达式索引 %s 反查回来没有表达式：%+v", label, dbIdx.Name, dbIdx)
			}
			if dbIdx.Type != idx.Type {
				t.Errorf("%s: %s 类型不一致 db=%d model=%d", label, dbIdx.Name, dbIdx.Type, idx.Type)
			}
		}
	}
	check("首次同步")

	// 二次同步：同一份声明，索引集合不变、仍然判等（说明没有被 DROP 再 CREATE 的必要）
	if _, err := o.SyncModel("", new(piJob)); err != nil {
		t.Fatalf("second SyncModel: %v", err)
	}
	check("二次同步")
}

func TestPartialIndex_SQLite_DefinitionChangeRebuilds(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pi.db")
	o1 := openPiOrm(t, dbPath, new(piJob))
	v1, _ := o1.GetModel("pi.job")
	var oldPartial string
	for _, idx := range v1.GetIndexes() {
		if idx.IsPartial() && idx.Type == UniqueType {
			oldPartial = idx.GetName("pi_job")
		}
	}
	if oldPartial == "" {
		t.Fatal("v1 应有唯一部分索引")
	}

	// 新进程、新声明（谓词 pending → queued）
	o2 := openPiOrm(t, dbPath, new(piJobV2))
	got := dbIndexes(t, o2, "pi_job")
	if _, still := got[oldPartial]; still {
		t.Fatalf("旧谓词的索引 %s 应随定义变化被删除；库里有：%v", oldPartial, keysOf(got))
	}
	v2, _ := o2.GetModel("pi.job")
	for _, idx := range v2.GetIndexes() {
		if got[idx.GetName("pi_job")] == nil {
			t.Fatalf("新声明的索引 %s 没建出来；库里有：%v", idx.GetName("pi_job"), keysOf(got))
		}
	}
	// 行为跟着新谓词走：pending 不再受约束，queued 受约束
	c := func(key, state, name string) error {
		_, err := o2.Model("pi.job").Create(map[string]any{"tenant_id": 1, "job_key": key, "state": state, "name": name})
		return err
	}
	if err := c("k", "pending", "n1"); err != nil {
		t.Fatal(err)
	}
	if err := c("k", "pending", "n2"); err != nil {
		t.Fatalf("pending 已不在谓词内，不该冲突：%v", err)
	}
	if err := c("q", "queued", "n3"); err != nil {
		t.Fatal(err)
	}
	if err := c("q", "queued", "n4"); err == nil || !stdErrors.Is(err, ormerr.ErrDuplicate) {
		t.Fatalf("queued 在新谓词内应冲突，得到 %v", err)
	}
}

// 声明错误不能静默跳过：列名拼错要在注册阶段报出来。
type piBadDecl struct {
	TModel `table:"name('pi_bad_decl')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(64)"`
}

func (self *piBadDecl) OnBuildFields() error {
	self.Builder().SetPartialUniqueIndex("state = 'x'", "tenant_idd")
	return nil // 故意不返回 b.Err()：声明错误也必须由 orm 自己兜住
}

func TestPartialIndex_DeclarationErrorIsSurfaced(t *testing.T) {
	o, err := New(WithDataSource(&TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "bad.db")}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.SyncModel("", new(piBadDecl)); err == nil {
		t.Fatal("拼错的列名应让 SyncModel 报错，而不是静默少一条索引")
	}
}

func keysOf(m map[string]*TIndex) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
