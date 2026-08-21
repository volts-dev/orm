package orm

import (
	"path/filepath"
	"sort"

	"github.com/volts-dev/dataset"
	"testing"
)

/*
many2one 上的 `IS NULL` / `IS NOT NULL` 回归。

TStatement.Where 收的是 SQL 串，交给 domain 解析器后 `site_id IS NULL` 变成
('site_id','=','NULL')。m2o 分支曾把 "NULL" 当**对端的名字**去 rec_name 里搜，
搜不到就把整条叶子换成 `(id in (0,0))` —— 一条恒假条件。

判据只能是**集合本身**：这类错法不报错、不留日志，查询照常成功，只是外键为空的
行一条都不回来。真栈上的样子是"前台导航栏只剩首页"。
*/

type (
	NullSite struct {
		TModel `table:"name('isnull_site')"`
		Id     int64  `field:"pk autoincr title('ID')"`
		Name   string `field:"varchar() size(64)"`
	}

	NullMenu struct {
		TModel `table:"name('isnull_menu')"`
		Id     int64  `field:"pk autoincr title('ID')"`
		Name   string `field:"varchar() size(64)"`
		SiteId int64  `field:"many2one(isnull_site)"`
	}
)

// setupIsNull 造出真栈同款的三种外键形态：指向本站、指向别的站、以及 **SQL NULL**。
// NULL 那条必须走裸 INSERT —— 经 ORM 建记录时省略 m2o 落的是哨兵值(0/-1)而不是
// NULL，形态不对就测不到这个 bug。
func setupIsNull(t *testing.T) (*TOrm, int64) {
	t.Helper()
	ds := &TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "isnull.db")}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(NullSite), new(NullMenu)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}

	site1 := firstId(t, mustCreate(t, o, "isnull.site", map[string]any{"name": "site1"}))
	site2 := firstId(t, mustCreate(t, o, "isnull.site", map[string]any{"name": "site2"}))

	mustCreate(t, o, "isnull.menu", map[string]any{"name": "home", "site_id": site1})
	mustCreate(t, o, "isnull.menu", map[string]any{"name": "other", "site_id": site2})
	if _, err := o.Exec(`INSERT INTO isnull_menu ("name") VALUES (?)`, "shop"); err != nil {
		t.Fatalf("insert null-fk row: %v", err)
	}
	return o, site1
}

func mustCreate(t *testing.T, o *TOrm, model string, vals map[string]any) any {
	t.Helper()
	id, err := o.Model(model).Create(vals)
	if err != nil {
		t.Fatalf("create %s %v: %v", model, vals, err)
	}
	return id
}

// menuNames 跑一次 Where 串查询，返回命中的菜单名（排序后便于比较）。
func menuNames(t *testing.T, o *TOrm, clause string, args ...any) []string {
	t.Helper()
	ds, err := o.Model("isnull.menu").Where(clause, args...).Limit(-1).Read()
	if err != nil {
		t.Fatalf("read %q: %v", clause, err)
	}
	var out []string
	if ds != nil {
		ds.Range(func(_ int, rec *dataset.TRecordSet) error {
			out = append(out, rec.FieldByName("name").AsString())
			return nil
		})
	}
	sort.Strings(out)
	return out
}

// 站点作用域的标准写法：本站的 + 不挂站点的。NULL 那支被吞掉时只剩 ["home"]。
func TestM2OIsNullInWhereString(t *testing.T) {
	o, site1 := setupIsNull(t)

	got := menuNames(t, o, "site_id=? OR site_id IS NULL", site1)
	want := []string{"home", "shop"}
	if !sameStrings(got, want) {
		t.Errorf("site_id=? OR site_id IS NULL 得到 %v，应为 %v"+
			"（少了 shop 就是 m2o 把 \"NULL\" 当名字搜了）", got, want)
	}
}

func TestM2OIsNotNullInWhereString(t *testing.T) {
	o, _ := setupIsNull(t)

	got := menuNames(t, o, "site_id IS NOT NULL")
	want := []string{"home", "other"}
	if !sameStrings(got, want) {
		t.Errorf("site_id IS NOT NULL 得到 %v，应为 %v", got, want)
	}
}

// 按**名字**过滤 m2o 的老路不能被这条修复顺手改坏：改动只认等值操作符 + 字面量
// NULL，右值是普通名字时仍应下钻到对端 rec_name 查 id。
//
// 用 Domain 而不是 Where("site_id=?", "site2")：占位符的值要到 leaf_to_sql 才拿得到，
// isNameOperand 那时查不了名字，本来就退回按 id 文本比较（见该函数注释）。
func TestM2ONameFilterStillResolvesByName(t *testing.T) {
	o, _ := setupIsNull(t)

	ds, err := o.Model("isnull.menu").Domain([]any{"site_id", "=", "site2"}).Limit(-1).Read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got []string
	if ds != nil {
		ds.Range(func(_ int, rec *dataset.TRecordSet) error {
			got = append(got, rec.FieldByName("name").AsString())
			return nil
		})
	}
	sort.Strings(got)
	if !sameStrings(got, []string{"other"}) {
		t.Errorf("site_id='site2' 得到 %v，应为 [other]（按名字搜对端的路不该被改坏）", got)
	}
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
