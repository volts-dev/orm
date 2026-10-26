package orm

import (
	"path/filepath"
	"strings"
	"testing"
)

// 标识符长度（MySQL 兼容，2026-10-24）：MySQL 上限 64 字符，超了直接报 1059；PG 上限 63，超了**静默截断**。
// 本仓的关联表名 / 索引名由模型名拼出来，很容易超（真栈 PG 上 122 个索引名正好 63 字符＝被截过；kylin 在
// MySQL 上第一个就卡在 registry_language_install_lang_rel_registry_language_install_id_idx，69 字符）。
// 这里拼一个与真实同形的多对多：表名在上限内，两条关联索引名都超 64（PG 截到 63 后彼此不撞）。
// 三个方言都要建得出来、写得进去，重启后都不能再发 DDL。

type LongIdentTag struct {
	TModel `table:"name('long_ident_tag')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar()"`
}

type LongIdentOwnerInstall struct {
	TModel `table:"name('long_ident_owner_install')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar() index"`
	TagIds []any  `field:"many2many(long_ident_tag,long_ident_owner_install_lang_rel,long_ident_owner_install_id,long_ident_tag_for_language_id)"`
}

func TestSyncModel_LongIdentifiers(t *testing.T) {
	ds := testDataSource(t, filepath.Join(t.TempDir(), "long.db"))
	models := func() []IModel {
		return []IModel{new(LongIdentTag), new(LongIdentOwnerInstall)}
	}
	for round := 0; round < 2; round++ {
		o, err := New(WithDataSource(ds))
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		rec := &ddlRecorder{}
		o.AddHook(rec)
		if _, err := o.SyncModel("", models()...); err != nil {
			t.Fatalf("第 %d 次 SyncModel: %v", round+1, err)
		}
		if round == 0 {
			ids, err := o.Model("long_ident_tag").Create(map[string]any{"name": "t"})
			if err != nil {
				t.Fatalf("Create tag: %v", err)
			}
			if _, err := o.Model("long_ident_owner_install").Create(map[string]any{
				"name": "o", "tag_ids": []any{[]any{6, 0, ids}}}); err != nil {
				t.Fatalf("Create owner with m2m: %v", err)
			}
		}
		// 关联表的建索引失败此前只记日志不返回——断言要直接问库。
		s := o.NewSession()
		idx, err := o.dialect.GetIndexes(s.context, s, "long_ident_owner_install_lang_rel")
		s.Close()
		if err != nil {
			t.Fatalf("GetIndexes: %v", err)
		}
		plain := 0
		for _, ix := range idx {
			if ix.Type == IndexType {
				plain++
			}
		}
		for _, full := range []string{
			"long_ident_owner_install_lang_rel_long_ident_owner_install_id_idx",
			"long_ident_owner_install_lang_rel_long_ident_tag_for_language_id_idx",
		} {
			if _, ok := idx[o.dialect.FitIdentifier(full)]; !ok && ds.DbType != "sqlite" {
				t.Errorf("库里没有名为 %s 的索引（FitIdentifier 与库里实际的名字对不上）：%v", o.dialect.FitIdentifier(full), idx)
			}
		}
		if plain != 2 {
			t.Errorf("第 %d 次同步后关联表上的普通索引有 %d 条，想要 2：%v", round+1, plain, idx)
		}
		if round == 1 && len(rec.ddl) > 0 {
			t.Errorf("重启后仍发了 %d 条 DDL（%s）：", len(rec.ddl), ds.DbType)
			for _, s := range rec.ddl {
				t.Errorf("  %s", s)
			}
		}
		o.Close()
	}
}

func TestFitIdentifier(t *testing.T) {
	long1 := "long_ident_owner_install_lang_rel_long_ident_owner_install_id_idx_and_more_a"
	long2 := "long_ident_owner_install_lang_rel_long_ident_owner_install_id_idx_and_more_b"
	my := &mysql{}
	a, b := my.FitIdentifier(long1), my.FitIdentifier(long2)
	if len([]rune(a)) != 64 || len([]rune(b)) != 64 || a == b {
		t.Errorf("MySQL：%q / %q 应各 64 字符且不同", a, b)
	}
	if my.FitIdentifier("short_idx") != "short_idx" {
		t.Error("没超的名字不该动")
	}
	pg := &postgres{}
	if got := pg.FitIdentifier(long1); got != long1[:63] {
		t.Errorf("PG 截断 = %q，想要前 63 字节", got)
	}
	// 多字节：不切半个字符（与服务端一致）。
	mb := strings.Repeat("a", 62) + "名字"
	if got := pg.FitIdentifier(mb); got != strings.Repeat("a", 62) {
		t.Errorf("PG 多字节截断 = %q", got)
	}
	if (&sqlite{}).FitIdentifier(long1) != long1 {
		t.Error("SQLite 不限长")
	}
}

// 关联表名本身就长到两条关联索引名截到 63 之后撞在一起（PG）：第二条 CREATE INDEX 报「已存在」，同步事务整个中止、
// 进程起不来——2026-10-26 在全新的 PG 库上装 im_livechat 撞出（真表名 im_livechat_channel_member_history_im_livechat_expertise_rel）。
type LongCollExpertise struct {
	TModel `table:"name('long_coll_expertise')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar()"`
}

type LongCollMemberHistory struct {
	TModel       `table:"name('long_coll_member_history')"`
	Id           int64 `field:"pk autoincr"`
	ExpertiseIds []any `field:"many2many(long_coll_expertise,im_livechat_channel_member_history_im_livechat_expertise_rel,im_livechat_channel_member_history_id,im_livechat_expertise_id)"`
}

func TestSyncModel_RelationIndexNamesCollideAfterTruncation(t *testing.T) {
	ds := testDataSource(t, filepath.Join(t.TempDir(), "coll.db"))
	for round := 0; round < 2; round++ {
		o, err := New(WithDataSource(ds))
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		rec := &ddlRecorder{}
		o.AddHook(rec)
		if _, err := o.SyncModel("", new(LongCollExpertise), new(LongCollMemberHistory)); err != nil {
			t.Fatalf("第 %d 次 SyncModel: %v", round+1, err)
		}
		s := o.NewSession()
		idx, err := o.dialect.GetIndexes(s.context, s, "im_livechat_channel_member_history_im_livechat_expertise_rel")
		s.Close()
		if err != nil {
			t.Fatalf("GetIndexes: %v", err)
		}
		plain := 0
		for _, ix := range idx {
			if ix.Type == IndexType {
				plain++
			}
		}
		if plain != 2 && ds.DbType != "sqlite" {
			t.Errorf("第 %d 次同步后关联表上的普通索引有 %d 条，想要 2：%v", round+1, plain, idx)
		}
		if round == 1 && len(rec.ddl) > 0 {
			t.Errorf("重启后仍发了 %d 条 DDL：%v", len(rec.ddl), rec.ddl)
		}
		o.Close()
	}
}
