package orm

import (
	"testing"

	_ "github.com/lib/pq"
)

/*
m2m 的读出口形态。

ManyToMany 回的是**关系表**的查询结果，不是对端记录。修复前：

	非经典读 → [{main_id:1, tag_id:2}]      连对端的名字都没有
	经典读   → [{id:2, name:"T1", main_id:1, tag_id:2}]  两张表的列混在一个 map 里

而同属关系字段的 o2m 回的是对端 id 列表、m2o 回的是 {id,name}。三种里只有 m2m 是
这个形状，vectors 因此在 portal / mail_followers / languages 三处绕开这个字段，
自己去查关系表。
*/

type msTag struct {
	TModel `table:"name('ms_tag')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(32) recname()"`
}

type msMain struct {
	TModel `table:"name('ms_main')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(32) recname()"`
	TagIds []any  `field:"many2many(ms_tag,ms_main_tag_rel,main_id,tag_id)"`
}

func setupM2MShape(t *testing.T) (*TOrm, any, []any) {
	t.Helper()
	ds := &TDataSource{DbType: "postgres", Host: "localhost", Port: "5432", UserName: "postgres", Password: "postgres", DbName: "test_orm", SSLMode: "disable"}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	tbs := []string{"ms_main_tag_rel", "ms_main", "ms_tag"}
	drop := func() {
		for _, tb := range tbs {
			o.Exec("DROP TABLE IF EXISTS " + tb + " CASCADE")
		}
	}
	drop()
	t.Cleanup(drop)
	if _, err := o.SyncModel("", new(msTag), new(msMain)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	t1, _ := o.Model("ms.tag").Create(map[string]any{"name": "T1"})
	t2, _ := o.Model("ms.tag").Create(map[string]any{"name": "T2"})
	ids, err := o.Model("ms.main").Create(map[string]any{
		"name": "M1", "tag_ids": []any{[]any{6, 0, []any{t1[0], t2[0]}}}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return o, ids[0], []any{t1[0], t2[0]}
}

// 非经典读：回**对端 id 列表**，与 o2m 一致。修复前是 [{main_id,tag_id}]，
// 里面一点对端数据都没有。
func TestM2MRead_NonClassicReturnsIdList(t *testing.T) {
	o, mainId, tagIds := setupM2MShape(t)

	sess := o.NewSession().Model("ms.main")
	sess.UseNameGet = true // 触发关系字段派发，但不是 ClassicRead
	ds, err := sess.Ids(mainId).Read()
	if err != nil {
		t.Fatal(err)
	}

	v := ds.Record().GetByField("tag_ids")
	got, ok := v.([]any)
	if !ok {
		t.Fatalf("m2m 非经典读应回 []any 的 id 列表，实得 %T = %v", v, v)
	}
	if len(got) != 2 {
		t.Fatalf("应有 2 个 id，实得 %v", got)
	}
	want := map[string]bool{}
	for _, id := range tagIds {
		want[toStr(id)] = true
	}
	for _, g := range got {
		if !want[toStr(g)] {
			t.Fatalf("回来的不是对端 tag 的 id：%v（期望其中之一 %v）", g, tagIds)
		}
	}
}

// 经典读：回**对端记录**，不带关系表的列。
func TestM2MRead_ClassicReturnsTargetRecords(t *testing.T) {
	o, mainId, _ := setupM2MShape(t)

	ds, err := o.NewSession().Model("ms.main").Classic().Ids(mainId).Read()
	if err != nil {
		t.Fatal(err)
	}
	v := ds.Record().GetByField("tag_ids")
	got, ok := v.([]map[string]any)
	if !ok {
		t.Fatalf("m2m 经典读应回 []map 的对端记录，实得 %T", v)
	}
	if len(got) != 2 {
		t.Fatalf("应有 2 条对端记录，实得 %d", len(got))
	}
	for _, rec := range got {
		// 关系表的列不属于对端记录，必须摘掉
		if _, has := rec["main_id"]; has {
			t.Errorf("对端记录里混进了关系表的列 main_id：%v", rec)
		}
		if _, has := rec["tag_id"]; has {
			t.Errorf("对端记录里混进了关系表的列 tag_id：%v", rec)
		}
		// 对端自己的列必须在
		if rec["id"] == nil {
			t.Errorf("对端记录缺 id：%v", rec)
		}
		if s := toStr(rec["name"]); s != "T1" && s != "T2" {
			t.Errorf("对端记录的 name 不对：%v", rec)
		}
	}
}

// 没有任何关联时给空切片而非 nil —— 调用方要能分清"没有关联"和"没读这个字段"。
func TestM2MRead_EmptyIsSliceNotNil(t *testing.T) {
	o, _, _ := setupM2MShape(t)

	ids, err := o.Model("ms.main").Create(map[string]any{"name": "M2"})
	if err != nil {
		t.Fatal(err)
	}
	sess := o.NewSession().Model("ms.main")
	sess.UseNameGet = true
	ds, err := sess.Ids(ids[0]).Read()
	if err != nil {
		t.Fatal(err)
	}
	v := ds.Record().GetByField("tag_ids")
	got, ok := v.([]any)
	if !ok {
		t.Fatalf("无关联时也应是 []any，实得 %T = %v", v, v)
	}
	if len(got) != 0 {
		t.Fatalf("无关联时应为空，实得 %v", got)
	}
}

func toStr(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case int64:
		return string0(x)
	case int:
		return string0(int64(x))
	}
	return ""
}

func string0(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [24]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
