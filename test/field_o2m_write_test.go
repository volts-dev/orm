package test

import (
	"path/filepath"
	"testing"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/orm"
	"github.com/volts-dev/orm/domain"
	"github.com/volts-dev/utils"

	_ "modernc.org/sqlite"
)

// one2many 的**写入**此前完全没有实现:TOne2ManyField 只重载 OnRead,写入落到
// TField.OnWrite 的默认分支,而 o2m 是 store=false 字段——_todoCompute 对非存储字段
// 只调 OnWrite 不收集返回值,于是整份命令元组被静默丢弃。表现是"表单内嵌 list 加了
// 几行、保存提示成功、重新读出来一行都没有",日志里也没有任何线索。
//
// 下面几个用例锁死 Odoo 风格命令元组 0/1/2/3/4/5/6 在 o2m 上的落地。

type (
	o2mOrderModel struct {
		orm.TModel `table:"name('o2m_order')"`
		Id         int64   `field:"pk autoincr title('ID') index"`
		Name       string  `field:"varchar() required"`
		LineIds    []int64 `field:"one2many(o2m_line,order_id) title('Lines')"`
		// 反向键可空的一侧:解绑(命令 3/5/6)应保留记录本身、只摘外键。
		NoteIds []int64 `field:"one2many(o2m_note,order_id) title('Notes')"`
	}

	// order_id required → 解绑等价于删除(Odoo 对 ondelete=cascade 的处理)。
	o2mLineModel struct {
		orm.TModel `table:"name('o2m_line')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		OrderId    int64  `field:"many2one(o2m_order) required index"`
		Product    string `field:"varchar()"`
		Qty        int    `field:"int() default(1)"`
	}

	// order_id 可空 → 解绑只置空外键,记录留在库里。
	o2mNoteModel struct {
		orm.TModel `table:"name('o2m_note')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		OrderId    int64  `field:"many2one(o2m_order) index"`
		Body       string `field:"varchar()"`
	}
)

func newO2MOrm(t *testing.T) *orm.TOrm {
	t.Helper()
	ds := &orm.TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "o2m.db")}
	o, err := orm.New(orm.WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("test", new(o2mOrderModel), new(o2mLineModel), new(o2mNoteModel)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	return o
}

// lineRows 读出某订单名下的行,按 product 排序返回 product→qty。
func lineRows(t *testing.T, o *orm.TOrm, model string, orderId any) map[string]int {
	t.Helper()
	m, err := o.GetModel(model)
	if err != nil {
		t.Fatal(err)
	}
	ds, err := m.Records().Domain(domain.New("order_id", "=", orderId)).Read()
	if err != nil {
		t.Fatalf("read %s: %v", model, err)
	}
	out := map[string]int{}
	if ds == nil {
		return out
	}
	ds.Range(func(_ int, rec *dataset.TRecordSet) error {
		out[utils.ToString(rec.GetByField("product"))] = utils.ToInt(rec.GetByField("qty"))
		return nil
	})
	return out
}

// TestOne2ManyCreateCommandOnCreate 覆盖最常撞的一条链路:新建父记录时随手带上
// (0, 0, vals) 的子行。这正是前端表单「新建产品 → 属性页加两行 → 保存」发的形状。
func TestOne2ManyCreateCommandOnCreate(t *testing.T) {
	o := newO2MOrm(t)
	order, err := o.GetModel("o2m_order")
	if err != nil {
		t.Fatal(err)
	}

	ids, err := order.Records().Create(map[string]any{
		"name": "SO001",
		"line_ids": []any{
			[]any{0, 0, map[string]any{"product": "Apple", "qty": 2}},
			[]any{0, 0, map[string]any{"product": "Pear", "qty": 5}},
		},
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if len(ids) == 0 || ids[0] == nil {
		t.Fatal("create 未返回 id")
	}

	got := lineRows(t, o, "o2m_line", ids[0])
	if len(got) != 2 || got["Apple"] != 2 || got["Pear"] != 5 {
		t.Fatalf("随父记录创建的子行没落库: got %v, want {Apple:2 Pear:5}", got)
	}
}

// TestOne2ManyCommandsOnWrite 覆盖 update 侧的 0/1/2:加一行、改一行、删一行。
func TestOne2ManyCommandsOnWrite(t *testing.T) {
	o := newO2MOrm(t)
	order, err := o.GetModel("o2m_order")
	if err != nil {
		t.Fatal(err)
	}
	line, err := o.GetModel("o2m_line")
	if err != nil {
		t.Fatal(err)
	}

	ids, err := order.Records().Create(map[string]any{
		"name": "SO002",
		"line_ids": []any{
			[]any{0, 0, map[string]any{"product": "Apple", "qty": 1}},
			[]any{0, 0, map[string]any{"product": "Pear", "qty": 1}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	orderId := ids[0]

	lineIdOf := func(product string) any {
		ds, err := line.Records().
			Domain(domain.New("order_id", "=", orderId).AND(domain.New("product", "=", product))).
			Read()
		if err != nil {
			t.Fatal(err)
		}
		if ds == nil || ds.Count() != 1 {
			t.Fatalf("按 product=%s 期望 1 条行, 实际 %v", product, ds.Count())
		}
		return ds.Record().GetByField("id")
	}

	appleId := lineIdOf("Apple")
	pearId := lineIdOf("Pear")

	if _, err := order.Records().Ids(orderId).Write(map[string]any{
		"line_ids": []any{
			[]any{0, 0, map[string]any{"product": "Plum", "qty": 3}}, // 新增
			[]any{1, appleId, map[string]any{"qty": 9}},              // 改量
			[]any{2, pearId},                                         // 删除
		},
	}); err != nil {
		t.Fatalf("write order lines: %v", err)
	}

	got := lineRows(t, o, "o2m_line", orderId)
	if _, has := got["Pear"]; has {
		t.Errorf("命令 2 应删除 Pear 行, 实际仍在: %v", got)
	}
	if got["Apple"] != 9 {
		t.Errorf("命令 1 应把 Apple 改成 9, 实际 %v", got)
	}
	if got["Plum"] != 3 {
		t.Errorf("命令 0 应新增 Plum, 实际 %v", got)
	}
}

// TestOne2ManyUnlinkRequiredInverseDeletes 反向键必填时,解绑(命令 5)只能是删除:
// 置空一个 NOT NULL 的外键要么写不进去、要么留下谁也认领不了的孤儿行。
func TestOne2ManyUnlinkRequiredInverseDeletes(t *testing.T) {
	o := newO2MOrm(t)
	order, err := o.GetModel("o2m_order")
	if err != nil {
		t.Fatal(err)
	}

	ids, err := order.Records().Create(map[string]any{
		"name":     "SO003",
		"line_ids": []any{[]any{0, 0, map[string]any{"product": "Apple", "qty": 1}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := order.Records().Ids(ids[0]).Write(map[string]any{
		"line_ids": []any{[]any{5}},
	}); err != nil {
		t.Fatalf("clear lines: %v", err)
	}

	if got := lineRows(t, o, "o2m_line", ids[0]); len(got) != 0 {
		t.Fatalf("命令 5 后不应还有子行: %v", got)
	}
}

// TestOne2ManyUnlinkNullableInverseKeepsRecord 反向键可空时,解绑保留记录本身。
//
// 这一支必须用会话的 Nullable():_separateValues 判定"调用方碰过这个字段"看的是值
// 非 nil,普通 Update 写 {order_id: nil} 会被整条丢掉,解绑就成了静默无操作。
func TestOne2ManyUnlinkNullableInverseKeepsRecord(t *testing.T) {
	o := newO2MOrm(t)
	order, err := o.GetModel("o2m_order")
	if err != nil {
		t.Fatal(err)
	}
	note, err := o.GetModel("o2m_note")
	if err != nil {
		t.Fatal(err)
	}

	ids, err := order.Records().Create(map[string]any{
		"name":     "SO004",
		"note_ids": []any{[]any{0, 0, map[string]any{"body": "keep me"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	orderId := ids[0]

	attached, err := note.Records().Domain(domain.New("order_id", "=", orderId)).Read()
	if err != nil {
		t.Fatal(err)
	}
	if attached.Count() != 1 {
		t.Fatalf("前提不成立: 期望 1 条 note 挂在订单上, 实际 %d", attached.Count())
	}
	noteId := attached.Record().GetByField("id")

	if _, err := order.Records().Ids(orderId).Write(map[string]any{
		"note_ids": []any{[]any{3, noteId}},
	}); err != nil {
		t.Fatalf("unlink note: %v", err)
	}

	// 已从订单上摘下……
	still, err := note.Records().Domain(domain.New("order_id", "=", orderId)).Read()
	if err != nil {
		t.Fatal(err)
	}
	if still.Count() != 0 {
		t.Fatalf("命令 3 后 note 不该还挂在订单上, 实际 %d 条", still.Count())
	}
	// ……但记录本身还在。
	kept, err := note.Records().Ids(noteId).Read()
	if err != nil {
		t.Fatal(err)
	}
	if kept.Count() != 1 {
		t.Fatalf("反向键可空时命令 3 应保留记录本身, 实际被删了")
	}
}

// TestOne2ManySetCommandReplaces 命令 6 是"设置成这批":不在列表里的解绑,不在库里的挂上。
func TestOne2ManySetCommandReplaces(t *testing.T) {
	o := newO2MOrm(t)
	order, err := o.GetModel("o2m_order")
	if err != nil {
		t.Fatal(err)
	}
	note, err := o.GetModel("o2m_note")
	if err != nil {
		t.Fatal(err)
	}

	ids, err := order.Records().Create(map[string]any{"name": "SO005"})
	if err != nil {
		t.Fatal(err)
	}
	orderId := ids[0]

	// 两条游离的 note(还没挂到任何订单上)。
	keepIds, err := note.Records().Create(map[string]any{"body": "keep"})
	if err != nil {
		t.Fatal(err)
	}
	dropIds, err := note.Records().Create(map[string]any{"body": "drop"})
	if err != nil {
		t.Fatal(err)
	}

	// 先都挂上。
	if _, err := order.Records().Ids(orderId).Write(map[string]any{
		"note_ids": []any{[]any{4, keepIds[0]}, []any{4, dropIds[0]}},
	}); err != nil {
		t.Fatalf("link notes: %v", err)
	}
	linked, err := note.Records().Domain(domain.New("order_id", "=", orderId)).Read()
	if err != nil {
		t.Fatal(err)
	}
	if linked.Count() != 2 {
		t.Fatalf("命令 4 应挂上 2 条, 实际 %d", linked.Count())
	}

	// 再"设置成只剩 keep"。
	if _, err := order.Records().Ids(orderId).Write(map[string]any{
		"note_ids": []any{[]any{6, 0, []any{keepIds[0]}}},
	}); err != nil {
		t.Fatalf("set notes: %v", err)
	}

	after, err := note.Records().Domain(domain.New("order_id", "=", orderId)).Read()
	if err != nil {
		t.Fatal(err)
	}
	if after.Count() != 1 {
		t.Fatalf("命令 6 后应只剩 1 条, 实际 %d", after.Count())
	}
	if got := utils.ToString(after.Record().GetByField("body")); got != "keep" {
		t.Fatalf("命令 6 留下的应是 keep, 实际 %q", got)
	}
}

// TestOne2ManyNonCommandValueIsIgnored 裸 id 列表没有明确语义(当"设置成这批"会把不在
// 列表里的子行全删掉,当"追加"又与 Odoo 不一致),历史行为是无操作。这里锁死"不动数据",
// 免得日后有人顺手给它加上破坏性语义。
func TestOne2ManyNonCommandValueIsIgnored(t *testing.T) {
	o := newO2MOrm(t)
	order, err := o.GetModel("o2m_order")
	if err != nil {
		t.Fatal(err)
	}

	ids, err := order.Records().Create(map[string]any{
		"name":     "SO006",
		"line_ids": []any{[]any{0, 0, map[string]any{"product": "Apple", "qty": 1}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := order.Records().Ids(ids[0]).Write(map[string]any{
		"line_ids": []any{1, 2, 3},
	}); err != nil {
		t.Fatalf("write bare id list: %v", err)
	}

	if got := lineRows(t, o, "o2m_line", ids[0]); len(got) != 1 || got["Apple"] != 1 {
		t.Fatalf("裸 id 列表不应改动子行: got %v", got)
	}
}
