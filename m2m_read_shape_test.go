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

// 经典读**没给子规格**：回 id 列表，与 o2m 逐字相同。
//
// 从前经典读一律内嵌整条对端记录，而同一次经典读里 o2m 回的是 id 列表——同一类字段、
// 同一次请求，两种形状。默认回 id 还挡掉了 payload 放大：一次列表读原本会把每行每个
// m2m 的对端整条塞进结果。
func TestM2MRead_ClassicWithoutSubSpecReturnsIdList(t *testing.T) {
	o, mainId, tagIds := setupM2MShape(t)

	ds, err := o.NewSession().Model("ms.main").Classic().Ids(mainId).Read()
	if err != nil {
		t.Fatal(err)
	}
	v := ds.Record().GetByField("tag_ids")
	got, ok := v.([]any)
	if !ok {
		t.Fatalf("经典读没给子规格时 m2m 应回 []any 的 id 列表，实得 %T = %v", v, v)
	}
	if len(got) != len(tagIds) {
		t.Fatalf("应有 %d 个 id，实得 %v", len(tagIds), got)
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

// **给了子规格**：回对端记录，不带关系表的列，且列范围由子规格限定。
func TestM2MRead_SubSpecReturnsTargetRecords(t *testing.T) {
	o, mainId, _ := setupM2MShape(t)

	model, err := o.GetModel("ms.main")
	if err != nil {
		t.Fatal(err)
	}
	ds, err := model.Read(&ReadRequest{
		Ids:         []any{mainId},
		Fields:      []string{"id", "name", "tag_ids"},
		ClassicRead: true,
		Limit:       -1,
		SubFields: map[string]*ReadRequest{
			"tag_ids": {Fields: []string{"name"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	v := ds.Record().GetByField("tag_ids")
	got, ok := v.([]map[string]any)
	if !ok {
		t.Fatalf("给了子规格时 m2m 应回 []map 的对端记录，实得 %T = %v", v, v)
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
		// 对端主键始终在——没有它调用方拿到的记录无从回指
		if rec["id"] == nil {
			t.Errorf("对端记录缺 id：%v", rec)
		}
		if s := toStr(rec["name"]); s != "T1" && s != "T2" {
			t.Errorf("对端记录的 name 不对：%v", rec)
		}
		// 列范围由子规格限定：没点名的列不该出现（此前是整张 rel.* 全塞进来）
		for k := range rec {
			if k != "id" && k != "name" {
				t.Errorf("子规格只点了 name，却回了多余的列 %q：%v", k, rec)
			}
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

// plain 读（既不是 Classic 也不是 NameGet）里**点了名**的 m2m 同样要出键、出真值。
//
// 与 o2m 共用 _read 里那道关系字段派发门槛：从前它对 IsRelated() 一律跳过，于是
// `Select("id","name","tag_ids")` 回来的记录里根本没有 tag_ids 这个键——不是空数组，
// 是键不存在，而请求成功、无日志。对照用例见 test/read_shape_contract_test.go 的
// TestPlainRead_NamedO2MAlwaysPresent。
func TestM2MRead_NamedInPlainReadIsPresent(t *testing.T) {
	o, mainId, tagIds := setupM2MShape(t)

	ds, err := o.NewSession().Model("ms.main").Select("id", "name", "tag_ids").Ids(mainId).Read()
	if err != nil {
		t.Fatal(err)
	}

	m := ds.Record().AsMap()
	if _, has := m["tag_ids"]; !has {
		t.Fatalf("点了名的 tag_ids 键必须存在，实得 %v", m)
	}
	got, ok := m["tag_ids"].([]any)
	if !ok {
		t.Fatalf("plain 读的 m2m 应回 []any 的 id 列表，实得 %T = %v", m["tag_ids"], m["tag_ids"])
	}
	if len(got) != len(tagIds) {
		t.Fatalf("应有 %d 个 id，实得 %v —— 空数组说明它是顶替出来的假值而不是真读的", len(tagIds), got)
	}
}
