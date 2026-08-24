package test

import (
	"path/filepath"
	"testing"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/orm"
	"github.com/volts-dev/utils"

	_ "modernc.org/sqlite"
)

// 删记录时必须把它在 m2m 关联表里的行一并删掉——**两个方向都要删**。
//
// 关联表没有任何外键约束（TMany2ManyField.update_db_foreign_keys 至今是空函数体），
// `ondelete` tag 又是写三处读零处的死配置，于是删一条记录不会清掉它在任何关联表里的
// 行，孤儿**持续积累**：2026-08-20 vectors 侧实测五张权限关系表 10~13% 是孤儿。
//
// 两个方向的后果不对等，只修一个等于没修：
//
//   - 源端（被删模型自己声明的 m2m 字段）丢掉 → 残留行而已；
//   - ★ 目标端（别人指向被删模型的 m2m 字段）丢掉 → 「菜单还在、组没了」，那个菜单
//     被限制到一个没人持有的组上，对所有人永久隐身。
type (
	m2mcTagModel struct {
		orm.TModel `table:"name('m2mc_tag')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Name       string `field:"varchar()"`
	}

	// 源端：帖子自己声明 tag_ids。
	m2mcPostModel struct {
		orm.TModel `table:"name('m2mc_post')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Name       string `field:"varchar()"`
		TagIds     []any  `field:"many2many(m2mc_tag,m2mc_post_tag_rel,post_id,tag_id)"`
	}

	// 目标端：菜单指向 tag，而 tag 上没有任何反向字段——正是 sys.menu ↔ res.group 的形状。
	m2mcMenuModel struct {
		orm.TModel `table:"name('m2mc_menu')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Name       string `field:"varchar()"`
		TagIds     []any  `field:"many2many(m2mc_tag,m2mc_menu_tag_rel,menu_id,tag_id)"`
	}

	// 声明的列名与库里对不上的一张关联表（真实库里指向 tag 的列叫 tag_ref，
	// 声明写的是 tag_id）。同一张关联表两侧各有一份声明、列名从各自的模型名推出来，
	// 只要有一侧用四参自定义过列名，这种不一致就会真实存在。
	m2mcOddModel struct {
		orm.TModel `table:"name('m2mc_odd')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Name       string `field:"varchar()"`
		TagIds     []any  `field:"many2many(m2mc_tag,m2mc_odd_rel,odd_id,tag_id)"`
	}
)

func newM2MCleanupOrm(t *testing.T) *orm.TOrm {
	t.Helper()
	ds := &orm.TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "m2mc.db")}
	o, err := orm.New(orm.WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("test", new(m2mcTagModel), new(m2mcPostModel), new(m2mcMenuModel), new(m2mcOddModel)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}

	// 关联表现在由 SyncModel 自己建得出来（m2m 的 DDL 已改走同步会话那条连接，
	// 见 TMany2ManyField.UpdateDb）。此前这里必须手工补建，理由写的是
	// 「sqlite 下会撞 SQLITE_BUSY——同一次同步里另一条连接还占着写锁」，
	// 那正是同一个 bug 在 sqlite 上的样子：postgres 上它表现为**永远等下去**。
	// 下面这几条留作幂等的安全网，撞上已建好的表就是 no-op。
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS m2mc_post_tag_rel (tag_id INTEGER NOT NULL, post_id INTEGER NOT NULL, UNIQUE(tag_id, post_id))`,
		`CREATE TABLE IF NOT EXISTS m2mc_menu_tag_rel (tag_id INTEGER NOT NULL, menu_id INTEGER NOT NULL, UNIQUE(tag_id, menu_id))`,
	} {
		if _, err := o.Exec(q); err != nil {
			t.Fatalf("建关联表 %q: %v", q, err)
		}
	}

	// 故意与声明对不上：声明说指向 tag 的列叫 tag_id，库里叫 tag_ref。
	// **必须先删掉 SyncModel 建的那张**——否则 CREATE ... IF NOT EXISTS 是 no-op，
	// 列名对不上这个前提就没造出来，用例会退化成"什么都没验"。
	for _, q := range []string{
		`DROP TABLE IF EXISTS m2mc_odd_rel`,
		`CREATE TABLE m2mc_odd_rel (tag_ref INTEGER NOT NULL, odd_id INTEGER NOT NULL, UNIQUE(tag_ref, odd_id))`,
	} {
		if _, err := o.Exec(q); err != nil {
			t.Fatalf("造列名对不上的关联表 %q: %v", q, err)
		}
	}
	return o
}

// relColumn 直接读关联表某一列的全部值——判据必须落在关联表上，
// 从模型侧读 tag_ids 看到的是 join 之后的结果，孤儿行照样"看不见"。
func relColumn(t *testing.T, o *orm.TOrm, table, column string) []string {
	t.Helper()
	ds, err := o.Query("SELECT " + column + " FROM " + table)
	if err != nil {
		t.Fatalf("query %s: %v", table, err)
	}
	out := make([]string, 0)
	if ds == nil {
		return out
	}
	ds.Range(func(_ int, rec *dataset.TRecordSet) error {
		out = append(out, utils.ToString(rec.GetByField(column)))
		return nil
	})
	return out
}

// seedM2MCleanup 造两颗 tag，一篇挂着两颗 tag 的帖子、一个挂着同样两颗 tag 的菜单。
// 返回 (tagIds, postId, menuId)。
func seedM2MCleanup(t *testing.T, o *orm.TOrm) ([]any, any, any) {
	t.Helper()

	tag, err := o.GetModel("m2mc_tag")
	if err != nil {
		t.Fatal(err)
	}
	post, err := o.GetModel("m2mc_post")
	if err != nil {
		t.Fatal(err)
	}
	menu, err := o.GetModel("m2mc_menu")
	if err != nil {
		t.Fatal(err)
	}

	tagIds := make([]any, 0, 2)
	for _, name := range []string{"T1", "T2"} {
		ids, err := tag.Records().Create(map[string]any{"name": name})
		if err != nil || len(ids) == 0 {
			t.Fatalf("create tag %s: %v", name, err)
		}
		tagIds = append(tagIds, ids[0])
	}

	postIds, err := post.Records().Create(map[string]any{
		"name":    "P1",
		"tag_ids": []any{[]any{6, 0, tagIds}},
	})
	if err != nil || len(postIds) == 0 {
		t.Fatalf("create post: %v", err)
	}
	menuIds, err := menu.Records().Create(map[string]any{
		"name":    "M1",
		"tag_ids": []any{[]any{6, 0, tagIds}},
	})
	if err != nil || len(menuIds) == 0 {
		t.Fatalf("create menu: %v", err)
	}

	if got := relColumn(t, o, "m2mc_post_tag_rel", "tag_id"); len(got) != 2 {
		t.Fatalf("前置条件不成立：帖子关联行应有 2 条，实际 %v", got)
	}
	if got := relColumn(t, o, "m2mc_menu_tag_rel", "tag_id"); len(got) != 2 {
		t.Fatalf("前置条件不成立：菜单关联行应有 2 条，实际 %v", got)
	}
	return tagIds, postIds[0], menuIds[0]
}

// 源端：删掉帖子，帖子自己声明的那张关联表里的行必须一起走。
func TestDeleteCleansUpOwnM2MRelationRows(t *testing.T) {
	o := newM2MCleanupOrm(t)
	_, postId, _ := seedM2MCleanup(t, o)

	post, err := o.GetModel("m2mc_post")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := post.Records().Delete(postId); err != nil {
		t.Fatalf("delete post: %v", err)
	}

	if got := relColumn(t, o, "m2mc_post_tag_rel", "post_id"); len(got) != 0 {
		t.Fatalf("删帖子后 m2mc_post_tag_rel 仍有 %d 行 %v —— 源端没清", len(got), got)
	}
	if got := relColumn(t, o, "m2mc_menu_tag_rel", "tag_id"); len(got) != 2 {
		t.Fatalf("删帖子不该动菜单的关联行，实际 %v", got)
	}
}

// ★ 目标端：删掉一颗 tag，**别人**指向它的关联行也必须走。tag 上没有任何反向字段，
// 只清源端的实现在这里一行都删不掉——这正是「菜单还在、组没了」那类永久隐身的孤儿。
func TestDeleteCleansUpInboundM2MRelationRows(t *testing.T) {
	o := newM2MCleanupOrm(t)
	tagIds, _, _ := seedM2MCleanup(t, o)

	tag, err := o.GetModel("m2mc_tag")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tag.Records().Delete(tagIds[0]); err != nil {
		t.Fatalf("delete tag: %v", err)
	}

	got := relColumn(t, o, "m2mc_menu_tag_rel", "tag_id")
	if len(got) != 1 {
		t.Fatalf("删 T1 后菜单关联行应只剩 1 条（指向 T2），实际 %v —— 目标端没清", got)
	}
	if want := utils.ToString(tagIds[1]); got[0] != want {
		t.Fatalf("留下来的关联行指向 %s，应当是 T2(%s) —— 删错了行", got[0], want)
	}
	// 帖子那张表同样要被清到（帖子声明的 tag_ids 也指向这颗 tag）。
	if got := relColumn(t, o, "m2mc_post_tag_rel", "tag_id"); len(got) != 1 {
		t.Fatalf("删 T1 后帖子关联行应只剩 1 条，实际 %v", got)
	}
}

// 索引按「声明字段的模型」记，不按遍历到的模型对象记：模型对象在 inherits/合并下
// 是共享的，拿宿主模型的 id 去删别人的关系行会删掉无辜数据。这里锁死"删一个模型
// 不会碰到与它无关的关联表"。
func TestDeleteDoesNotTouchUnrelatedRelationTables(t *testing.T) {
	o := newM2MCleanupOrm(t)

	tag, err := o.GetModel("m2mc_tag")
	if err != nil {
		t.Fatal(err)
	}
	post, err := o.GetModel("m2mc_post")
	if err != nil {
		t.Fatal(err)
	}
	menu, err := o.GetModel("m2mc_menu")
	if err != nil {
		t.Fatal(err)
	}

	tagIds, err := tag.Records().Create(map[string]any{"name": "T1"})
	if err != nil || len(tagIds) == 0 {
		t.Fatalf("create tag: %v", err)
	}
	postIds, err := post.Records().Create(map[string]any{
		"name": "P1", "tag_ids": []any{[]any{6, 0, []any{tagIds[0]}}},
	})
	if err != nil || len(postIds) == 0 {
		t.Fatalf("create post: %v", err)
	}

	// 造一条 menu_id 与被删 post 的 id 相同的菜单关联行：若清理按错了列/表，
	// 删 post 就会把它一起删掉。
	menuIds, err := menu.Records().Create(map[string]any{
		"name": "M1", "tag_ids": []any{[]any{6, 0, []any{tagIds[0]}}},
	})
	if err != nil || len(menuIds) == 0 {
		t.Fatalf("create menu: %v", err)
	}
	if _, err := o.Exec("UPDATE m2mc_menu_tag_rel SET menu_id=?", postIds[0]); err != nil {
		t.Fatalf("改写菜单关联行: %v", err)
	}

	if _, err := post.Records().Delete(postIds[0]); err != nil {
		t.Fatalf("delete post: %v", err)
	}
	if got := relColumn(t, o, "m2mc_menu_tag_rel", "menu_id"); len(got) != 1 {
		t.Fatalf("删 post 把菜单关联行也删了：m2mc_menu_tag_rel 剩 %v", got)
	}
}

// 声明的列名与库里对不上时，那张表跳过即可：**不能让删除本身失败**。
// 在事务里发一条会报错的语句会把整个事务判死（postgres: current transaction is
// aborted），于是"清垃圾"把调用方那次删除一起搭进去。
func TestDeleteSurvivesRelationColumnMismatch(t *testing.T) {
	o := newM2MCleanupOrm(t)
	tagIds, _, _ := seedM2MCleanup(t, o)

	// 往那张列名对不上的关联表里塞一行，它清不掉是可以接受的（回到修之前的状态）。
	if _, err := o.Exec("INSERT INTO m2mc_odd_rel (tag_ref, odd_id) VALUES (?, ?)", tagIds[0], 1); err != nil {
		t.Fatalf("造行: %v", err)
	}

	tag, err := o.GetModel("m2mc_tag")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tag.Records().Delete(tagIds[0]); err != nil {
		t.Fatalf("删除不该因为一张对不上的关联表而失败: %v", err)
	}

	// 记录真的删掉了。
	ds, err := o.Query("SELECT id FROM m2mc_tag WHERE id = ?", tagIds[0])
	if err != nil {
		t.Fatalf("query tag: %v", err)
	}
	if ds != nil && ds.Count() != 0 {
		t.Fatal("记录没删掉——清理失败把主删除也带下水了")
	}
	// 能清的那张照常清干净。
	if got := relColumn(t, o, "m2mc_menu_tag_rel", "tag_id"); len(got) != 1 {
		t.Fatalf("列名正常的关联表应已清到只剩 1 行，实际 %v", got)
	}
}
