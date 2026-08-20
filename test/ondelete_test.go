package test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/orm"
	"github.com/volts-dev/utils"

	_ "modernc.org/sqlite"
)

// many2one 上的 `ondelete` 从前是写三处读零处的死配置：删掉一张销售单，它的订单行
// 原样留在库里（2026-08-20 真栈清点出 sale_order_line 30 行、account_move_line 5 行
// 这样的孤儿）。这组用例锁住三条语义与两条边界：
//
//	cascade   → 子行跟着走，并递归到孙行
//	restrict  → 拒绝删除，父行与子行都原样还在
//	set null  → 子行留着，那一列被置空
//	未声明     → 一律不动（Odoo 的默认是 set null，本仓 900 多个 m2o 绝大多数没声明，
//	             套默认等于每次删除都扫全库）
//	自引用成环 → 不死循环，两行都删掉，且主删除的目标不被级联抢先删掉
type (
	odParentModel struct {
		orm.TModel `table:"name('od_parent')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Name       string `field:"varchar()"`
	}

	odChildModel struct {
		orm.TModel `table:"name('od_child')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Name       string `field:"varchar()"`
		ParentId   int64  `field:"many2one(od_parent) index ondelete('cascade')"`
	}

	// 孙行：级联必须递归下去，否则删父之后 od_grand 变成指向已消失的 od_child 的孤儿。
	odGrandModel struct {
		orm.TModel `table:"name('od_grand')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Name       string `field:"varchar()"`
		ChildId    int64  `field:"many2one(od_child) index ondelete('cascade')"`
	}

	odLooseModel struct {
		orm.TModel `table:"name('od_loose')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Name       string `field:"varchar()"`
		ParentId   int64  `field:"many2one(od_parent) index ondelete('set null')"`
	}

	// 没有 ondelete 声明：删父之后它必须原样不动。
	odPlainModel struct {
		orm.TModel `table:"name('od_plain')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Name       string `field:"varchar()"`
		ParentId   int64  `field:"many2one(od_parent) index"`
	}

	odLockedModel struct {
		orm.TModel `table:"name('od_locked')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Name       string `field:"varchar()"`
	}

	odGuardModel struct {
		orm.TModel `table:"name('od_guard')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Name       string `field:"varchar()"`
		LockedId   int64  `field:"many2one(od_locked) index ondelete('restrict')"`
	}

	// 挂在子行上的 restrict：删父时级联会经过 od_child，而 od_pin 挡着它——
	// 整棵树的 restrict 都要在动手之前查完。
	odPinModel struct {
		orm.TModel `table:"name('od_pin')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Name       string `field:"varchar()"`
		ChildId    int64  `field:"many2one(od_child) index ondelete('restrict')"`
	}

	// 自引用：分类树 / 菜单树 / 移动链都是这个形状，数据成环时递归必须能停。
	odNodeModel struct {
		orm.TModel `table:"name('od_node')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Name       string `field:"varchar()"`
		ParentId   int64  `field:"many2one(od_node) index ondelete('cascade')"`
	}
)

func newOnDeleteOrm(t *testing.T) *orm.TOrm {
	t.Helper()
	ds := &orm.TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "ondelete.db")}
	o, err := orm.New(orm.WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("test",
		new(odParentModel), new(odChildModel), new(odGrandModel), new(odLooseModel),
		new(odPlainModel), new(odLockedModel), new(odGuardModel), new(odPinModel),
		new(odNodeModel)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	return o
}

// rowCount 直接数表里的行——判据必须落在子表上，从父模型侧看什么都看不见。
func rowCount(t *testing.T, o *orm.TOrm, table string, where ...string) int {
	t.Helper()
	sql := "SELECT COUNT(*) AS c FROM " + table
	if len(where) > 0 && where[0] != "" {
		sql += " WHERE " + where[0]
	}
	ds, err := o.Query(sql)
	if err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if ds == nil || ds.Count() == 0 {
		return 0
	}
	ds.First()
	return utils.ToInt(ds.Record().GetByField("c"))
}

func column(t *testing.T, o *orm.TOrm, table, col string) []string {
	t.Helper()
	ds, err := o.Query("SELECT " + col + " FROM " + table)
	if err != nil {
		t.Fatalf("query %s: %v", table, err)
	}
	out := make([]string, 0)
	if ds == nil {
		return out
	}
	ds.Range(func(_ int, rec *dataset.TRecordSet) error {
		out = append(out, utils.ToString(rec.GetByField(col)))
		return nil
	})
	return out
}

func create(t *testing.T, o *orm.TOrm, model string, vals map[string]any) any {
	t.Helper()
	m, err := o.GetModel(model)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := m.Records().Create(vals)
	if err != nil || len(ids) == 0 {
		t.Fatalf("create %s: %v", model, err)
	}
	return ids[0]
}

func del(t *testing.T, o *orm.TOrm, model string, id any) error {
	t.Helper()
	m, err := o.GetModel(model)
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Records().Delete(id)
	return err
}

// cascade：子行、孙行一起走；未声明 ondelete 的那张表一行不动；set null 的只被置空。
func TestOnDeleteCascadeRemovesChildrenAndGrandChildren(t *testing.T) {
	o := newOnDeleteOrm(t)

	parentId := create(t, o, "od_parent", map[string]any{"name": "P"})
	childId := create(t, o, "od_child", map[string]any{"name": "C", "parent_id": parentId})
	create(t, o, "od_grand", map[string]any{"name": "G", "child_id": childId})
	create(t, o, "od_loose", map[string]any{"name": "L", "parent_id": parentId})
	create(t, o, "od_plain", map[string]any{"name": "X", "parent_id": parentId})

	if err := del(t, o, "od_parent", parentId); err != nil {
		t.Fatalf("delete parent: %v", err)
	}

	if n := rowCount(t, o, "od_child"); n != 0 {
		t.Fatalf("ondelete('cascade') 没生效：od_child 还剩 %d 行", n)
	}
	if n := rowCount(t, o, "od_grand"); n != 0 {
		t.Fatalf("级联没有递归到孙行：od_grand 还剩 %d 行（它现在指向一个已经不存在的 od_child）", n)
	}
	if n := rowCount(t, o, "od_plain"); n != 1 {
		t.Fatalf("没声明 ondelete 的表被动了：od_plain 剩 %d 行，应为 1", n)
	}
	if n := rowCount(t, o, "od_loose"); n != 1 {
		t.Fatalf("ondelete('set null') 不该删行：od_loose 剩 %d 行，应为 1", n)
	}
	for _, v := range column(t, o, "od_loose", "parent_id") {
		if v != "" && v != "0" && v != "<nil>" {
			t.Fatalf("ondelete('set null') 没把列置空：parent_id=%q", v)
		}
	}
}

// restrict：拒绝删除，且父行、子行都原样还在（不能删了一半才发现拦路的）。
func TestOnDeleteRestrictBlocksDeletion(t *testing.T) {
	o := newOnDeleteOrm(t)

	lockedId := create(t, o, "od_locked", map[string]any{"name": "L"})
	guardId := create(t, o, "od_guard", map[string]any{"name": "G", "locked_id": lockedId})

	err := del(t, o, "od_locked", lockedId)
	if err == nil {
		t.Fatal("ondelete('restrict') 没拦住删除")
	}
	if !strings.Contains(err.Error(), "restrict") {
		t.Fatalf("错误里没点明是 restrict 拦的，排障时没线索: %v", err)
	}
	if n := rowCount(t, o, "od_locked"); n != 1 {
		t.Fatalf("被 restrict 拒绝之后父行不该消失，实际剩 %d 行", n)
	}
	if n := rowCount(t, o, "od_guard"); n != 1 {
		t.Fatalf("被 restrict 拒绝之后子行不该消失，实际剩 %d 行", n)
	}

	// 拦路的行去掉之后就该删得动了
	if err := del(t, o, "od_guard", guardId); err != nil {
		t.Fatalf("delete guard: %v", err)
	}
	if err := del(t, o, "od_locked", lockedId); err != nil {
		t.Fatalf("拦路行已删，父行仍删不掉: %v", err)
	}
	if n := rowCount(t, o, "od_locked"); n != 0 {
		t.Fatalf("父行没删掉，剩 %d 行", n)
	}
}

// 整棵级联树上的 restrict 都要认：父 →(cascade) 子 ←(restrict) 别人，删父必须被拒绝，
// 而且**一行都不能少**——边删边查的实现会先把子行删掉，才在下一层发现拦不住。
func TestOnDeleteRestrictOnDescendantBlocksWholeTree(t *testing.T) {
	o := newOnDeleteOrm(t)

	parentId := create(t, o, "od_parent", map[string]any{"name": "P"})
	childId := create(t, o, "od_child", map[string]any{"name": "C", "parent_id": parentId})
	create(t, o, "od_grand", map[string]any{"name": "G", "child_id": childId})
	create(t, o, "od_pin", map[string]any{"name": "PIN", "child_id": childId})

	err := del(t, o, "od_parent", parentId)
	if err == nil {
		t.Fatal("子行被 restrict 钉住时，删父必须被拒绝")
	}
	if n := rowCount(t, o, "od_parent"); n != 1 {
		t.Fatalf("被拒绝之后父行不该消失，实际剩 %d 行", n)
	}
	if n := rowCount(t, o, "od_child"); n != 1 {
		t.Fatalf("被拒绝之后子行不该消失（说明是边删边查），实际剩 %d 行", n)
	}
	if n := rowCount(t, o, "od_grand"); n != 1 {
		t.Fatalf("被拒绝之后孙行不该消失，实际剩 %d 行", n)
	}
}

// 自引用成环（A.parent=B、B.parent=A）：不能死循环，两行都要走，
// 而且主删除的目标不能被级联抢先删掉（那会让主 DELETE 影响 0 行）。
func TestOnDeleteCascadeSurvivesSelfReferenceCycle(t *testing.T) {
	o := newOnDeleteOrm(t)

	m, err := o.GetModel("od_node")
	if err != nil {
		t.Fatal(err)
	}
	aId := create(t, o, "od_node", map[string]any{"name": "A"})
	bId := create(t, o, "od_node", map[string]any{"name": "B", "parent_id": aId})
	if _, err := m.Records().Ids(aId).Write(map[string]any{"parent_id": bId}); err != nil {
		t.Fatalf("成环失败: %v", err)
	}

	// 带超时：环没停下来时这里会挂满整个 test timeout，那样什么结果都报不出来。
	done := make(chan error, 1)
	go func() { done <- del(t, o, "od_node", aId) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("delete node: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("级联在自引用环上没有停下来（死循环）")
	}

	if n := rowCount(t, o, "od_node"); n != 0 {
		t.Fatalf("环上的行没删干净，还剩 %d 行", n)
	}
}
