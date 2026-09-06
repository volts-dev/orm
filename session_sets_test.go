package orm

import (
	"path/filepath"
	"testing"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/utils"
)

/*
会话级 Queryable Set（多租户的 tenant_id 就是这么挂的）在 Statement.Init() 里被重新
挂成条件。原来走 Where(name+"=?") 的字符串解析，现在直建叶子——这份用例锁定两条路
语义相同：读被过滤、写被盖戳、多次执行不重复叠加。
*/

type ssItem struct {
	TModel   `table:"name('ss_item')"`
	Id       int64  `field:"pk autoincr"`
	TenantId int64  `field:"int()"`
	Name     string `field:"varchar(64)"`
}

func TestSessionSets_QueryableFiltersAndStamps(t *testing.T) {
	ds := &TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "ss.db")}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.SyncModel("", new(ssItem)); err != nil {
		t.Fatal(err)
	}
	// 两个租户各两行（裸会话写入）
	for _, row := range []map[string]any{
		{"tenant_id": 1, "name": "a1"}, {"tenant_id": 1, "name": "a2"},
		{"tenant_id": 2, "name": "b1"}, {"tenant_id": 2, "name": "b2"},
	} {
		if _, err := o.Model("ss.item").Create(row); err != nil {
			t.Fatal(err)
		}
	}

	sess := o.NewSession()
	defer sess.Close()
	sess.SetMustFieldValue("tenant_id", int64(1), true)

	// 读：只看见租户 1
	rs, err := sess.Model("ss.item").Limit(-1).Read()
	if err != nil {
		t.Fatal(err)
	}
	if rs.Count() != 2 {
		t.Fatalf("租户 1 应读回 2 行，得到 %d", rs.Count())
	}
	rs.Range(func(_ int, rec *dataset.TRecordSet) error {
		if utils.ToInt64(rec.GetByField("tenant_id")) != 1 {
			t.Fatalf("读到了别的租户的行：%v", rec.GetByField("tenant_id"))
		}
		return nil
	})

	// 同一会话再执行一次：条件不重复叠加、结果一致
	n, err := sess.Model("ss.item").Count()
	if err != nil || n != 2 {
		t.Fatalf("第二次 Count 应为 2，得到 %d / %v", n, err)
	}

	// 与显式 Where 叠加
	rs, err = sess.Model("ss.item").Where("name=?", "a2").Read()
	if err != nil || rs.Count() != 1 {
		t.Fatalf("tenant_id=1 AND name=a2 应为 1 行，得到 %d / %v", rs.Count(), err)
	}
	// 别的租户的名字在本会话看不到
	rs, err = sess.Model("ss.item").Where("name=?", "b1").Read()
	if err != nil || rs.Count() != 0 {
		t.Fatalf("租户 1 的会话不该看见 b1，得到 %d / %v", rs.Count(), err)
	}

	// 写：Create 自动盖戳 tenant_id
	ids, err := sess.Model("ss.item").Create(map[string]any{"name": "a3"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := o.Model("ss.item").Ids(ids[0]).Read()
	if err != nil || got.Count() != 1 {
		t.Fatalf("读回新建行失败：%v", err)
	}
	if utils.ToInt64(got.Record().GetByField("tenant_id")) != 1 {
		t.Fatalf("Create 应自动盖上 tenant_id=1，得到 %v", got.Record().GetByField("tenant_id"))
	}

	// 写：按 id 改别的租户的行，被可见范围收窄成 0 行
	all, _ := o.Model("ss.item").Where("name=?", "b1").Read()
	b1 := all.Record().GetByField("id")
	eff, err := sess.Model("ss.item").Ids(b1).Write(map[string]any{"name": "hacked"})
	if err != nil || eff != 0 {
		t.Fatalf("跨租户按 id 写应为 (0, nil)，得到 (%d, %v)", eff, err)
	}
}
