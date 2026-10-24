package orm

import (
	"path/filepath"
	"strings"
	"testing"
)

// 一张真表同时是别的模型多对多的关联表（mass_mailing 的 mailing_subscription：contact.list_ids /
// list.contact_ids 的关联表就是订阅模型自己的表）。建关联表时 ORM 在上面加 `<表>_<列>_idx`，
// 名字不是模型的命名规则（IDX_ / UQE_），模型上也没声明——同步模型时不能把它当过期索引删掉。
// MySQL 探针 10-25：每次重启都 DROP 再 CREATE mailing_subscription_contact_id_idx。
// 手工建的索引（运维加的、RLS 的 _rel_tenant_id_idx、向量 HNSW）同理：ORM 只管自己命名的索引。

type FiSub struct {
	TModel    `table:"name('fi_sub')"`
	Id        int64 `field:"pk autoincr"`
	ContactId int64 `field:"many2one(fi.contact) index"`
	ListId    int64 `field:"many2one(fi.list) index"`
	OptOut    bool  `field:"boolean()"`
}

type FiList struct {
	TModel     `table:"name('fi_list')"`
	Id         int64   `field:"pk autoincr"`
	Name       string  `field:"varchar()"`
	ContactIds []int64 `field:"many2many(fi.contact,fi_sub,list_id,contact_id)"`
}

type FiContact struct {
	TModel  `table:"name('fi_contact')"`
	Id      int64   `field:"pk autoincr"`
	Name    string  `field:"varchar()"`
	ListIds []int64 `field:"many2many(fi.list,fi_sub,contact_id,list_id)"`
}

func TestSyncModel_KeepsIndexesItDidNotName(t *testing.T) {
	ds := testDataSource(t, filepath.Join(t.TempDir(), "fi.db"))
	for round := 0; round < 3; round++ {
		o, err := New(WithDataSource(ds))
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		rec := &ddlRecorder{}
		o.AddHook(rec)
		if _, err := o.SyncModel("", new(FiSub), new(FiList), new(FiContact)); err != nil {
			t.Fatalf("第 %d 次 SyncModel: %v", round+1, err)
		}
		if round == 0 {
			// 运维手工加的索引：名字不归 ORM 管。
			if _, err := o.Exec(`CREATE INDEX fi_sub_manual_optout ON fi_sub (opt_out)`); err != nil {
				t.Fatalf("手工建索引: %v", err)
			}
		}
		if round > 0 {
			for _, d := range rec.ddl {
				if u := strings.ToUpper(d); strings.HasPrefix(u, "DROP") || strings.HasPrefix(u, "CREATE INDEX") ||
					strings.HasPrefix(u, "CREATE UNIQUE") || strings.HasPrefix(u, "ALTER") {
					t.Errorf("第 %d 次同步发了 DDL：%s", round+1, d)
				}
			}
		}
		s := o.NewSession()
		idx, err := o.dialect.GetIndexes(s.context, s, "fi_sub")
		s.Close()
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := idx["fi_sub_manual_optout"]; !ok && round > 0 {
			t.Errorf("第 %d 次同步后手工索引没了：%v", round+1, keysOf(idx))
		}
		o.Close()
	}
}
