package orm

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	_ "github.com/lib/pq"
)

type bfOwner struct {
	TModel `table:"name('bf_owner')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(32)"`
}

type bfTarget struct {
	TModel `table:"name('bf_target')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(32)"`
}

func bfSource(t testing.TB) *TDataSource {
	return pgDataSource(t, "test_orm_addfield")
}

// bfOrm 起一个同步好 bf_owner / bf_target 的 orm（冻结后）。reset 为真时先清场 ——
// 清场必须在另一个 TOrm 上做、早于被测的那个（New 会把库里已有的表反查进来）。
func bfOrm(t *testing.T, reset bool) (*TOrm, string, string) {
	t.Helper()
	if reset {
		cleaner, err := New(WithDataSource(bfSource(t)))
		if err != nil {
			t.Skipf("postgres unavailable: %v", err)
		}
		for _, q := range []string{
			`DROP TABLE IF EXISTS bf_owner, bf_target, bf_side, bf_side_tags, bf_owner_target_rel, bf_owner_optional_rel CASCADE`,
			`CREATE TABLE bf_side (owner_id bigint, note varchar(64))`,
			`CREATE TABLE bf_side_tags (owner_id bigint, target_id bigint)`,
		} {
			if _, err := cleaner.Exec(q); err != nil {
				cleaner.Close()
				t.Fatalf("%s: %v", q, err)
			}
		}
		cleaner.Close()
	}
	o, err := New(WithDataSource(bfSource(t)))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	t.Cleanup(func() { o.Close() })
	names, err := o.SyncModel("test", new(bfOwner), new(bfTarget))
	if err != nil || len(names) != 2 {
		t.Fatalf("SyncModel: %v %v", names, err)
	}
	if err := o.Freeze(context.Background()); err != nil {
		t.Fatal(err)
	}
	return o, names[0], names[1]
}

func bfColumnExists(t *testing.T, o *TOrm, table, col string) bool {
	t.Helper()
	ds, err := o.Query(`SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = ? AND column_name = ?`, table, col)
	if err != nil {
		t.Fatal(err)
	}
	return ds.Count() > 0
}

// 列建出来的那个事务里回填一次；重启后列已在，不再回填（否则会拿侧表的旧值覆盖掉之后写的新值）。
func TestAddField_Postgres_BackfillRunsOnceWithTheColumn(t *testing.T) {
	o, owner, _ := bfOrm(t, true)
	a, err := o.Model(owner).Create(map[string]any{"name": "a"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := o.Model(owner).Create(map[string]any{"name": "b"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.Exec(`INSERT INTO bf_side (owner_id, note) VALUES (?, 'from side'), (?, NULL)`, a[0], b[0]); err != nil {
		t.Fatal(err)
	}

	calls := 0
	backfill := WithBackfill(func(s *TSession) error {
		calls++
		_, err := s.Exec(`UPDATE bf_owner o SET note = s.note FROM bf_side s WHERE s.owner_id = o.id AND s.note IS NOT NULL`)
		return err
	})
	if added, err := o.AddField(owner, "note", "varchar(64) title('Note')", backfill); err != nil || !added {
		t.Fatalf("AddField: added=%v err=%v", added, err)
	}
	if calls != 1 {
		t.Fatalf("回填应执行一次，实际 %d 次", calls)
	}
	note := func(o *TOrm, id any) string {
		t.Helper()
		ds, err := o.Model(owner).Ids(id).Read()
		if err != nil || ds.Count() != 1 {
			t.Fatalf("read %v: %v", id, err)
		}
		return ds.FieldByName("note").AsString()
	}
	if got := note(o, a[0]); got != "from side" {
		t.Fatalf("a 应回填成侧表的值，实得 %q", got)
	}
	if got := note(o, b[0]); got != "" {
		t.Fatalf("b 侧表是空值，应保持空，实得 %q", got)
	}

	// 之后用户改了值；侧表还留着旧值。重启（新的 orm 反查到这一列）再补一次：不许再回填。
	if _, err := o.Model(owner).Ids(a[0]).Write(map[string]any{"note": "edited"}); err != nil {
		t.Fatal(err)
	}
	o2, _, _ := bfOrm(t, false)
	if _, err := o2.AddField(owner, "note", "varchar(64) title('Note')", backfill); err != nil {
		t.Fatalf("AddField after restart: %v", err)
	}
	if calls != 1 {
		t.Fatalf("列已存在时不该再回填，回填被调了 %d 次", calls)
	}
	if got := note(o2, a[0]); got != "edited" {
		t.Fatalf("重启后的回填覆盖了用户改过的值：%q", got)
	}
}

// 回填失败：列不建、字段不挂，报错；修好之后再补一次就成。
func TestAddField_Postgres_FailedBackfillLeavesNoColumn(t *testing.T) {
	o, owner, _ := bfOrm(t, true)
	boom := errors.New("boom")
	if _, err := o.AddField(owner, "note", "varchar(64)", WithBackfill(func(s *TSession) error { return boom })); !errors.Is(err, boom) {
		t.Fatalf("回填失败应把错误交回来，实得 %v", err)
	}
	if bfColumnExists(t, o, "bf_owner", "note") {
		t.Fatal("回填失败，列却建出来了：以后再也不会回填")
	}
	m, _ := o.GetModel(owner)
	if m.GetFieldByName("note") != nil {
		t.Fatal("回填失败，字段却挂上了模型：读这个模型会报列不存在")
	}
	if added, err := o.AddField(owner, "note", "varchar(64)", WithBackfill(func(s *TSession) error { return nil })); err != nil || !added {
		t.Fatalf("修好之后再补：added=%v err=%v", added, err)
	}
	if !bfColumnExists(t, o, "bf_owner", "note") {
		t.Fatal("第二次补列没建出来")
	}
}

// many2many：关联表建出来的那个事务里回填。
func TestAddField_Postgres_BackfillM2MRelation(t *testing.T) {
	o, owner, target := bfOrm(t, true)
	a, _ := o.Model(owner).Create(map[string]any{"name": "a"})
	t1, _ := o.Model(target).Create(map[string]any{"name": "t1"})
	t2, _ := o.Model(target).Create(map[string]any{"name": "t2"})
	if _, err := o.Exec(`INSERT INTO bf_side_tags (owner_id, target_id) VALUES (?, ?), (?, ?)`, a[0], t1[0], a[0], t2[0]); err != nil {
		t.Fatal(err)
	}
	tag := "many2many(" + target + ",bf_owner_target_rel,owner_id,target_id) title('Targets')"
	if added, err := o.AddField(owner, "target_ids", tag, WithBackfill(func(s *TSession) error {
		_, err := s.Exec(`INSERT INTO bf_owner_target_rel (owner_id, target_id) SELECT owner_id, target_id FROM bf_side_tags ON CONFLICT DO NOTHING`)
		return err
	})); err != nil || !added {
		t.Fatalf("AddField m2m: added=%v err=%v", added, err)
	}
	ds, err := o.Query(`SELECT count(*) AS n FROM bf_owner_target_rel WHERE owner_id = ?`, a[0])
	if err != nil {
		t.Fatal(err)
	}
	if n := ds.FieldByName("n").AsInteger(); n != 2 {
		t.Fatalf("关联表应回填 2 行，实得 %d", n)
	}
}

// 冻结之后补上来的 many2many 要读得出来：关联表对应的关系模型得注册上。
//
// 真栈 2026-09-29 撞到：sale 的 pro.tmpl.optional_product_ids（扩展字段，属主 kylin 在模型冻结
// 之后补）写得进、按它过滤也对，读出来这个键整个不在 —— 每次读都在日志里报
// "the models are not correctable for ManyToMany"，请求照样成功。RegisterModel 冻结后一律拒绝
// 新模型，关联表建了、关系模型没注册。重启之后反查会把那张表当模型带进来，于是只在补字段的
// 那一次运行里坏。自关联、两列显式给名字，同 Odoo 的 product_optional_rel(src_id, dest_id)。
func TestAddField_Postgres_FrozenM2MIsReadable(t *testing.T) {
	o, owner, _ := bfOrm(t, true)
	tag := "many2many(" + owner + ",bf_owner_optional_rel,src_id,dest_id) title('Optional')"
	if added, err := o.AddField(owner, "optional_ids", tag); err != nil || !added {
		t.Fatalf("AddField m2m: added=%v err=%v", added, err)
	}
	a, _ := o.Model(owner).Create(map[string]any{"name": "a"})
	b, _ := o.Model(owner).Create(map[string]any{"name": "b"})
	c, _ := o.Model(owner).Create(map[string]any{"name": "c"})
	if _, err := o.Model(owner).Ids(a[0]).Write(map[string]any{"optional_ids": []any{[]any{6, 0, []any{b[0], c[0]}}}}); err != nil {
		t.Fatal(err)
	}
	ds, err := o.Model(owner).Ids(a[0]).Select("id", "optional_ids").Read()
	if err != nil || ds.Count() != 1 {
		t.Fatalf("read: %v (%d rows)", err, ds.Count())
	}
	raw, has := ds.Record().AsMap()["optional_ids"]
	if !has {
		t.Fatal("读回来的记录里没有 optional_ids 这个键：关系模型没注册，OnRead 报错被吞")
	}
	got := map[string]bool{}
	list, _ := raw.([]any)
	for _, v := range list {
		got[fmt.Sprint(v)] = true
	}
	want := map[string]bool{fmt.Sprint(b[0]): true, fmt.Sprint(c[0]): true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optional_ids 读回 %v，应为 %v", raw, want)
	}
}

// 运行期补上来的 many2one 带 ondelete：目标删了要执行（索引是缓存，补字段必须作废它）。
func TestAddField_Postgres_OnDeleteSeesRuntimeField(t *testing.T) {
	o, owner, target := bfOrm(t, true)
	// 先删一次，让「谁指向 bf.target」的索引建出来并缓存住。
	warm, _ := o.Model(target).Create(map[string]any{"name": "warm"})
	if _, err := o.Model(target).Delete(warm...); err != nil {
		t.Fatal(err)
	}
	if _, err := o.AddField(owner, "target_id", "many2one("+target+") ondelete('set null')"); err != nil {
		t.Fatal(err)
	}
	t1, _ := o.Model(target).Create(map[string]any{"name": "t1"})
	a, _ := o.Model(owner).Create(map[string]any{"name": "a", "target_id": t1[0]})
	if _, err := o.Model(target).Delete(t1...); err != nil {
		t.Fatal(err)
	}
	ds, err := o.Query(`SELECT COALESCE(target_id, 0) AS v FROM bf_owner WHERE id = ?`, a[0])
	if err != nil {
		t.Fatal(err)
	}
	if v := ds.FieldByName("v").AsInteger(); v != 0 {
		t.Fatalf("目标删了，set null 没执行：target_id 仍是 %d", v)
	}
}

// 按公司区分的 many2one：set null 只删掉指向它的那几格，别的公司的值不动；restrict 挡删除。
func TestAddField_Postgres_OnDeleteCompanyDependent(t *testing.T) {
	o, owner, target := bfOrm(t, true)
	if _, err := o.AddField(owner, "pref_id", "many2one("+target+") company_dependent ondelete('set null')"); err != nil {
		t.Fatal(err)
	}
	if _, err := o.AddField(owner, "lock_id", "many2one("+target+") company_dependent ondelete('restrict')"); err != nil {
		t.Fatal(err)
	}
	in := func(cid int64) *TSession { return o.Model(owner).WithContext(WithCompanyId(context.Background(), cid)) }
	t1, _ := o.Model(target).Create(map[string]any{"name": "t1"})
	t2, _ := o.Model(target).Create(map[string]any{"name": "t2"})
	a, err := in(1).Create(map[string]any{"name": "a", "pref_id": t1[0]})
	if err != nil {
		t.Fatal(err)
	}
	// 公司 2 那一格写成字符串形态（前端雪花号走字符串）。
	if _, err := in(2).Ids(a...).Write(map[string]any{"pref_id": toString(t2[0])}); err != nil {
		t.Fatal(err)
	}
	if _, err := in(3).Ids(a...).Write(map[string]any{"pref_id": toString(t1[0])}); err != nil {
		t.Fatal(err)
	}

	if _, err := o.Model(target).Delete(t1...); err != nil {
		t.Fatalf("删 t1: %v", err)
	}
	read := func(cid int64) int64 {
		ds, err := in(cid).Ids(a...).Read()
		if err != nil || ds.Count() != 1 {
			t.Fatalf("read: %v", err)
		}
		return ds.FieldByName("pref_id").AsInteger()
	}
	if v := read(1); v != 0 {
		t.Fatalf("公司 1 指向被删的 t1，应清空，实得 %d", v)
	}
	if v := read(3); v != 0 {
		t.Fatalf("公司 3（字符串形态）指向被删的 t1，应清空，实得 %d", v)
	}
	if v := read(2); v != toInt64(t2[0]) {
		t.Fatalf("公司 2 指向 t2，不该动，实得 %d", v)
	}

	// restrict：还有一格指着它就不许删。
	if _, err := in(1).Ids(a...).Write(map[string]any{"lock_id": t2[0]}); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Model(target).Delete(t2...); err == nil {
		t.Fatal("lock_id 在公司 1 里指着 t2（restrict），删除应被拒绝")
	}
}

// 别的进程的模型上、指向本进程模型的列（ForeignRef）：本进程删目标记录时照样执行 ondelete。
// 真实形态：sale 给 res.partner（kylin）加的列指向 pro.pricelist（salesvc）；salesvc 删价格表，
// 本进程的索引里没有 res.partner，只能靠宿主告诉它。表在同一个库里，SQL 直接能跑。
func TestOnDelete_Postgres_ForeignRefs(t *testing.T) {
	o, _, target := bfOrm(t, true)
	if _, err := o.Exec(`DROP TABLE IF EXISTS bf_foreign, bf_foreign_rel`); err != nil {
		t.Fatal(err)
	}
	o.SetForeignRefs(func(to string) []ForeignRef {
		if to != target {
			return nil
		}
		return []ForeignRef{
			{Model: "bf.foreign", Column: "pref_id", OnDelete: "set null"},
			{Model: "bf.foreign", Column: "lock_id", OnDelete: "restrict"},
			{Model: "bf.foreign", Column: "cpref_id", OnDelete: "set null", CompanyDependent: true},
			{Model: "bf.foreign", Column: "target_id", RelationTable: "bf_foreign_rel"},
		}
	})
	// 先删一次，让本 orm 的库结构快照建出来（此刻 bf_foreign 还不存在）。
	warm, _ := o.Model(target).Create(map[string]any{"name": "warm"})
	if _, err := o.Model(target).Delete(warm...); err != nil {
		t.Fatal(err)
	}
	// bf_foreign 不是本 orm 注册的模型：模拟另一个进程的属主表。由**另一个** orm 建 ——
	// 另一个进程运行期补的列，本进程的库结构快照里没有，列存不存在要直接问库。
	other, err := New(WithDataSource(bfSource(t)))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	for _, q := range []string{
		`DROP TABLE IF EXISTS bf_foreign, bf_foreign_rel`,
		`CREATE TABLE bf_foreign (id bigint primary key, pref_id bigint, lock_id bigint, cpref_id jsonb)`,
		`CREATE TABLE bf_foreign_rel (foreign_id bigint, target_id bigint)`,
	} {
		if _, err := other.Exec(q); err != nil {
			other.Close()
			t.Fatal(err)
		}
	}
	other.Close()
	t1, _ := o.Model(target).Create(map[string]any{"name": "t1"})
	t2, _ := o.Model(target).Create(map[string]any{"name": "t2"})
	if _, err := o.Exec(`INSERT INTO bf_foreign (id, pref_id, cpref_id) VALUES (1, ?, jsonb_build_object('1', ?::bigint, '2', ?::bigint))`, t1[0], t1[0], t2[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Exec(`INSERT INTO bf_foreign_rel VALUES (1, ?), (1, ?)`, t1[0], t2[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Model(target).Delete(t1...); err != nil {
		t.Fatalf("删 t1: %v", err)
	}
	ds, _ := o.Query(`SELECT COALESCE(pref_id, 0) AS p, cpref_id::text AS c FROM bf_foreign WHERE id = 1`)
	if p := ds.FieldByName("p").AsInteger(); p != 0 {
		t.Fatalf("别的进程的列指向被删的 t1，set null 没执行：%d", p)
	}
	if c := ds.FieldByName("c").AsString(); strings.Contains(c, `"1"`) || !strings.Contains(c, `"2"`) {
		t.Fatalf("按公司区分的列应只删掉指向 t1 的那一格：%s", c)
	}
	rel, _ := o.Query(`SELECT count(*) AS n FROM bf_foreign_rel WHERE target_id = ?`, t1[0])
	if n := rel.FieldByName("n").AsInteger(); n != 0 {
		t.Fatalf("别的进程的多对多关联表里指向 t1 的行没删：%d", n)
	}
	if _, err := o.Exec(`UPDATE bf_foreign SET lock_id = ? WHERE id = 1`, t2[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Model(target).Delete(t2...); err == nil {
		t.Fatal("别的进程的列 restrict 指着 t2，删除应被拒绝")
	}
}
