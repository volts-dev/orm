package orm

import (
	"testing"

	_ "github.com/lib/pq"
)

// addcolProbe 的表在测试里先用裸 SQL 建成「少一列」的样子，再让 SyncModel 去补。
type addcolProbe struct {
	TModel `table:"name('addcol_probe')"`
	Id     int64  `field:"pk autoincr"`
	Note   string `field:"varchar(32)"`
	Fresh  int64  `field:"int"` // ← 库里没有这一列，SyncModel 必须 ALTER TABLE ADD 出来
}

// TestSyncModelAddsColumnToExistingTable 锁死「给已存在的表新增字段」这条路。
//
// 曾经它是**彻底静默失效**的：osv.RegisterModel 把结构体模型的字段合并进
// **共享的** TModelObject，而 DBMetas 反查出来的 oldModel 与之共用同一个 obj
//（New() 里的 _reverse 启动时就把反查模型注册进 osv 了）。于是 _alterTable 里
// oldModel.GetFieldByName(新字段) 不再是 nil，新列被误判成"库里已有"，
// ALTER TABLE ADD 一条都不发，也不打任何日志——直到下一条 INSERT 报
// `pq: column "xxx" of relation "yyy" does not exist`。
//
// 复现必须开两个 TOrm：污染的前提是**建表发生在 New() 之前**，这样 _reverse
// 才会把该表注册进 osv 并让后来的结构体模型合并到同一个 obj 上。单进程内先建表
// 再同步不会触发。
func TestSyncModelAddsColumnToExistingTable(t *testing.T) {
	ds := &TDataSource{DbType: "postgres", Host: "localhost", Port: "5432", UserName: "postgres", Password: "postgres", DbName: "test_orm", SSLMode: "disable"}

	o1, err := New(WithDataSource(ds))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	if _, err := o1.Exec(`DROP TABLE IF EXISTS addcol_probe`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	defer o1.Exec(`DROP TABLE IF EXISTS addcol_probe`)
	if _, err := o1.Exec(`CREATE TABLE addcol_probe (id BIGSERIAL PRIMARY KEY, note VARCHAR(32))`); err != nil {
		t.Fatalf("create: %v", err)
	}

	// 第二个 TOrm：New() 内的 _reverse 会把已存在的 addcol_probe 反查并注册进 osv。
	o2, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("second orm: %v", err)
	}
	if _, err := o2.SyncModel("test", new(addcolProbe)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}

	ds2, err := o2.Query(`SELECT column_name FROM information_schema.columns WHERE table_name='addcol_probe' AND column_name='fresh'`)
	if err != nil {
		t.Fatalf("introspect: %v", err)
	}
	if ds2.Count() == 0 {
		t.Fatal("SyncModel 没有为已存在的表补出新列 fresh —— 结构体新增字段被共享 TModelObject 污染成「库里已有」")
	}
}
