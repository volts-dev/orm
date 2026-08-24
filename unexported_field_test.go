package orm

import (
	"path/filepath"
	"strings"
	"testing"
)

/*
带 ORM 标签的未导出字段。

反射取不到未导出字段：这一列从来不会被建出来，读写它也永远是空操作，全程无错、
无日志。视图里一直引用它，表现是"这张表单打不开"或某个 modifier 条件恒为假，
排查时完全看不出根因。

vectors 为找这一类缺陷专门写了一个静态分析器(tools/viewlint)——2026-08-02 那轮
人工排查在 product / purchase / website_sale 里找出 40 余处。需要动用静态分析才
发现，说明启动日志里多一行 warn 是不够的，所以这里在模型注册期直接拒绝。
*/

type uxGood struct {
	TModel `table:"name('ux_good')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(64)"`
	// 无标签的未导出字段是正常的私有成员，必须保持静默。
	cache map[string]any
	once  bool
}

type uxBad struct {
	TModel `table:"name('ux_bad')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(64)"`
	// ★ 小写开头 + field 标签 = 这一列从来没被建出来过
	warehouseId int64 `field:"many2one(ux_good) title('Warehouse')"`
}

type uxDashed struct {
	TModel `table:"name('ux_dashed')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(64)"`
	// `field:"-"` 是"这个字段不归 ORM 管"的显式标记，不该被误伤。
	// vectors 的 core/db/config.go 就是这么写的。
	datasource *TDataSource `field:"-"`
}

func newUxOrm(t *testing.T) *TOrm {
	t.Helper()
	ds := &TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "ux.db")}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	return o
}

func TestUnexportedTaggedField_IsRejected(t *testing.T) {
	o := newUxOrm(t)

	_, err := o.SyncModel("", new(uxBad))
	if err == nil {
		t.Fatal("带 field 标签的未导出字段必须在注册期被拒绝 —— 否则那一列永远不会被建出来，且无声无息")
	}
	// 错误信息必须点名是哪个字段、并说清后果与修法，否则等于换了一种没人看得懂的失败。
	msg := err.Error()
	for _, want := range []string{"warehouse_id", "unexported", "never created", "Export it"} {
		if !strings.Contains(msg, want) {
			t.Errorf("错误信息缺少 %q：%s", want, msg)
		}
	}
}

// 无标签的未导出字段是正常的私有成员（缓存、锁、一次性标志），不能因为收紧就把它们也拦下。
func TestUnexportedUntaggedField_IsFine(t *testing.T) {
	o := newUxOrm(t)

	if _, err := o.SyncModel("", new(uxGood)); err != nil {
		t.Fatalf("无标签的未导出字段是正常私有成员，不该报错：%v", err)
	}
	if _, err := o.Model("ux.good").Create(map[string]any{"name": "x"}); err != nil {
		t.Fatalf("模型应当照常可用：%v", err)
	}
}

// `field:"-"` 明说了"不归 ORM 管"，未导出也不该报。
func TestUnexportedDashTaggedField_IsFine(t *testing.T) {
	o := newUxOrm(t)

	if _, err := o.SyncModel("", new(uxDashed)); err != nil {
		t.Fatalf(`field:"-" 是显式排除标记，不该被误伤：%v`, err)
	}
}
