package test

import (
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/volts-dev/dataset"
	"github.com/volts-dev/orm"
)

// m2m 的关联表**同时也是一张模型表**时，SyncModel 不能把自己卡死。
//
// 这不是假想的形状，Odoo 到处这么用：mailing.list.contact_ids 与
// mailing.contact.list_ids 的关联表就是 mailing_subscription —— 一张有主键、
// 有 opt_out/opt_out_datetime 等字段、自己也要被注册成模型的真表。
//
// 曾经的失败方式：TOrm.SyncModel 把整批模型的建表放在**一个事务**里
// （orm.go: session.Begin/Commit），而 TMany2ManyField.UpdateDb 的关联表 DDL 走
// orm.Exec —— 另开一条连接、自动提交。同步事务刚在这张表上建过索引、持着锁不放，
// 另一条连接的 `CREATE INDEX ... ON mrt_sub` 就永远等下去：
//
//   - **没有超时、没有报错**，日志停在上一条 SQL 上；
//   - 进程活着、etcd 租约照续，只是端口永不监听；
//   - pg_blocking_pids 里看得一清二楚：blocked=CREATE INDEX，
//     blocking=同进程那条 "idle in transaction"。
//
// sqlite 上同一个 bug 表现为 SQLITE_BUSY（见 m2m_cleanup_test.go 里那段注释的
// 前身），错误被 UpdateDb 吞掉，于是关联表**静默漏建**。
//
// 判据必须带超时：回归了就该红，而不是把整个测试进程挂到 go test 的总超时。
//
// ★ 关联表所属的模型要**先于**用它的模型注册（下面 MRTSub 在前）。同步事务开始前
// 快照过一次库结构，反过来注册会让 m2m 先建出一张两列的 mrt_sub，随后模型自己的
// 建表撞上"表已存在"。这条要求在修之前就存在，与本用例锁的东西无关。
type (
	// MRTSub 是关联表本身的模型：有自己的主键与业务字段，还有索引 —— 索引正是
	// 同步事务在这张表上持锁的原因。
	MRTSub struct {
		orm.TModel `table:"name('mrt_sub')"`
		Id         int64     `field:"pk autoincr title('ID') index"`
		ListId     int64     `field:"many2one(mrt_list) index"`
		ContactId  int64     `field:"many2one(mrt_contact) index"`
		OptOut     bool      `field:"bool() index"`
		OptOutAt   time.Time `field:"datetime()"`
	}

	MRTList struct {
		orm.TModel `table:"name('mrt_list')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Name       string `field:"varchar() required"`
		// 关联表写的就是上面那张模型表。
		ContactIds []any `field:"many2many(mrt_contact,mrt_sub,list_id,contact_id)"`
	}

	MRTContact struct {
		orm.TModel `table:"name('mrt_contact')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Email      string `field:"varchar()"`
		ListIds    []any  `field:"many2many(mrt_list,mrt_sub,contact_id,list_id)"`
	}
)

func TestSyncModel_M2MRelationTableIsAlsoAModel_PG(t *testing.T) {
	ensurePG(t)

	o, err := orm.New(orm.WithDataSource(defaultPostgresSource()))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}

	// 幂等清场。顺序：先关联表再主表（关联表带 m2o 引用）。
	for _, q := range []string{
		`DROP TABLE IF EXISTS public.mrt_sub`,
		`DROP TABLE IF EXISTS public.mrt_list`,
		`DROP TABLE IF EXISTS public.mrt_contact`,
	} {
		if _, err := o.Exec(q); err != nil {
			t.Fatalf("清场失败 %q: %v", q, err)
		}
	}

	done := make(chan error, 1)
	go func() {
		_, err := o.SyncModel("test", new(MRTSub), new(MRTList), new(MRTContact))
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SyncModel: %v", err)
		}
	case <-time.After(45 * time.Second):
		t.Fatal("SyncModel 卡住了：m2m 关联表的 DDL 又跑到同步事务之外的连接上去了，" +
			"正在等本进程自己持有的表锁。真机上的样子是进程活着但端口永不监听。")
	}

	// 表真的建出来了，而且是模型那份结构（有 id 主键与 opt_out），不是 m2m 那份两列表。
	ds, err := o.Query(`SELECT column_name FROM information_schema.columns
	                    WHERE table_schema='public' AND table_name='mrt_sub'`)
	if err != nil {
		t.Fatalf("查列: %v", err)
	}
	cols := map[string]bool{}
	if ds != nil {
		ds.Range(func(_ int, rec *dataset.TRecordSet) error {
			cols[rec.FieldByName("column_name").AsString()] = true
			return nil
		})
	}
	for _, want := range []string{"id", "list_id", "contact_id", "opt_out"} {
		if !cols[want] {
			t.Errorf("mrt_sub 少了列 %q —— 关联表被当成纯 m2m 中间表建成了两列，"+
				"模型自己的字段全没了。实得 %v", want, cols)
		}
	}
}
