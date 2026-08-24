package orm

import (
	"strings"
	"testing"

	_ "github.com/lib/pq"
)

/*
公开的 Exec/Query 在 IsAutoClose 的会话上会 `defer self.Close()`，而 Close 把
db 与 tx 都置 nil —— 同一个会话的**第二次**调用是空指针崩溃，整个进程带走，
调用栈指向驱动层，完全看不出根因是"这个会话早就关了"。

真实形状：m2m 的 OnWrite 先 unlink_all 再 link，两次都用 ctx.Session.Exec，
于是 `orm.Model(x).Create(带 m2m 值)` **必然 panic**（TOrm.Model 把 IsAutoClose
置 true）。vectors 一直走 model.Records()/Tx()（IsAutoClose=false），才没撞上。

两道修：关系字段的内部写入改用 _exec（不带 autoclose 契约），
以及把"用已关闭的会话"从崩溃降级成可诊断的错误。
*/

type scTag struct {
	TModel `table:"name('sc_tag')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(32) recname()"`
}

type scMain struct {
	TModel `table:"name('sc_main')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(32) recname()"`
	TagIds []any  `field:"many2many(sc_tag,sc_main_tag_rel,main_id,tag_id)"`
}

// m2m 写入不能把会话关掉。这条在修复前是 SIGSEGV，不是失败。
func TestSession_M2MWriteOnAutoCloseSession(t *testing.T) {
	ds := &TDataSource{DbType: "postgres", Host: "localhost", Port: "5432", UserName: "postgres", Password: "postgres", DbName: "test_orm", SSLMode: "disable"}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	tbs := []string{"sc_main_tag_rel", "sc_main", "sc_tag"}
	drop := func() {
		for _, tb := range tbs {
			o.Exec("DROP TABLE IF EXISTS " + tb + " CASCADE")
		}
	}
	drop()
	t.Cleanup(drop)
	if _, err := o.SyncModel("", new(scTag), new(scMain)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}

	t1, err := o.Model("sc.tag").Create(map[string]any{"name": "T1"})
	if err != nil {
		t.Fatal(err)
	}
	t2, err := o.Model("sc.tag").Create(map[string]any{"name": "T2"})
	if err != nil {
		t.Fatal(err)
	}

	// TOrm.Model() 起的会话 IsAutoClose=true —— 正是会踩雷的那种
	sess := o.Model("sc.main")
	if !sess.IsAutoClose {
		t.Fatal("用例失效：TOrm.Model() 应当起一个 IsAutoClose 的会话")
	}

	ids, err := o.Model("sc.main").Create(map[string]any{
		"name": "M1", "tag_ids": []any{[]any{6, 0, []any{t1[0], t2[0]}}}})
	if err != nil {
		t.Fatalf("带 m2m 值的 Create 失败：%v", err)
	}

	ds2, err := o.Model("sc.main").Classic().Ids(ids[0]).Read()
	if err != nil {
		t.Fatal(err)
	}
	tags, _ := ds2.Record().GetByField("tag_ids").([]map[string]any)
	if len(tags) != 2 {
		t.Fatalf("两个 tag 都该关联上，实得 %d 条", len(tags))
	}
}

// 用已关闭的会话必须报错，而不是空指针崩溃。
func TestSession_ClosedSessionErrsInsteadOfPanicking(t *testing.T) {
	o := newRlOrm(t) // 复用 read_limit_test.go 的小夹具（sqlite）

	sess := o.NewSession()
	sess.Close()

	if _, err := sess.Query("SELECT 1"); err == nil {
		t.Fatal("已关闭的会话上 Query 应当报错")
	} else if !strings.Contains(err.Error(), "session is closed") {
		t.Fatalf("错误信息应说清是会话已关闭：%v", err)
	}

	if _, err := sess.Exec("SELECT 1"); err == nil {
		t.Fatal("已关闭的会话上 Exec 应当报错")
	} else if !strings.Contains(err.Error(), "session is closed") {
		t.Fatalf("错误信息应说清是会话已关闭：%v", err)
	}
}
