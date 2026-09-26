package orm

import (
	"testing"

	_ "github.com/lib/pq"
)

// 同一个写事务里「写 m2m、紧接着回读」必须读到刚写的关联。
//
// ManyToMany 读关系表时另起一个会话；那条连接不在调用方的事务里，看不到同一事务里刚
// link 进去的行，于是回读恒为空、不报错。2026-09-26 social.post 建帖（Create 覆写里按
// 选中的账号回算 media_ids）撞到：account_ids 读回来是空的，下游拿空列表写 [6,0,[]]
// 才炸出来。
func TestM2MRead_SameTxSeesUncommittedLinks(t *testing.T) {
	o, _, tagIds := setupM2MShape(t)

	sess := o.NewSession()
	defer sess.Close()
	if err := sess.Begin(); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer sess.Rollback(nil)

	ids, err := sess.Model("ms.main").Create(map[string]any{
		"name": "M2", "tag_ids": []any{[]any{6, 0, tagIds}}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	m := sess.Model("ms.main")
	m.UseNameGet = true
	ds, err := m.Ids(ids[0]).Read()
	if err != nil {
		t.Fatal(err)
	}
	got, ok := ds.Record().GetByField("tag_ids").([]any)
	if !ok || len(got) != len(tagIds) {
		t.Fatalf("同一事务里回读 m2m 应有 %d 个 id，实得 %T = %v", len(tagIds), ds.Record().GetByField("tag_ids"), ds.Record().GetByField("tag_ids"))
	}

	// 事务归调用方：回读那一下不许替它结束事务，之后在同一事务里照常能写。
	if _, err := sess.Model("ms.tag").Create(map[string]any{"name": "T3"}); err != nil {
		t.Fatalf("回读之后事务不该被关掉：%v", err)
	}
}
