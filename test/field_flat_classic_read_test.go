package test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/volts-dev/orm"

	_ "modernc.org/sqlite"
)

// 复现 form view 的实际读写路径:前端发的是**平铺** ReadRequest(Fields 为 arch 字段名、
// ClassicRead=true、无 SubFields)。本文件覆盖 many2one 的写入形态与 one2many 在该
// 路径下的读取回填形态。
type (
	FCPartner struct {
		orm.TModel `table:"name('fc_partner')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Name       string `field:"varchar() required"`
		Color      int    `field:"int()"`
	}

	FCLine struct {
		orm.TModel `table:"name('fc_line')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Name       string `field:"varchar()"`
		Qty        int    `field:"int()"`
		OrderId    int64  `field:"many2one(fc_order)"`
	}

	FCOrder struct {
		orm.TModel `table:"name('fc_order')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Name       string `field:"varchar() required"`
		PartnerId  int64  `field:"many2one(fc_partner)"`
		Lines      []any  `field:"one2many(fc_line,order_id)"`
	}
)

func newFlatClassicOrm(t *testing.T) *orm.TOrm {
	t.Helper()
	ds := &orm.TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "flat.db")}
	o, err := orm.New(orm.WithDataSource(ds), orm.WithBigNumberToString(true))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("test", new(FCPartner), new(FCLine), new(FCOrder)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	if err := o.Freeze(context.Background()); err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	return o
}

// TestM2OWriteTupleDoesNotZero 回归:前端 many2one 组件(Many2OneField.ts)提交的写
// 值是 Odoo 经典 `[id, name]` 元组,不是裸 id。此前 session_crwd.go `_todoCompute`
// 对 TYPE_M2O 字段只在写入值是**纯字符串**时才调用 field.OnWrite(TMany2OneField.
// OnWrite 会正确从元组取 v[0]);其余形态(包括元组)落到 default 分支,没有自定义
// getter/setter 的普通 many2one 字段会跳过 OnWrite,直接把整个 []any{id,name} 元组
// 扔给 value2SqlTypeValue→utils.ToInt64,对 BigInt 列解析失败退回 0——字段被写成 0
// 而不是新 id,表现为"更新提交成功(无报错)但读不到新值"。
func TestM2OWriteTupleDoesNotZero(t *testing.T) {
	o := newFlatClassicOrm(t)

	partnerModel, _ := o.GetModel("fc_partner")
	orderModel, _ := o.GetModel("fc_order")

	ss := o.NewSession()
	defer ss.Close()
	if err := ss.Begin(); err != nil {
		t.Fatal(err)
	}

	pidsA, _ := partnerModel.Tx(ss).Create(map[string]any{"name": "PartnerA"})
	pidsB, _ := partnerModel.Tx(ss).Create(map[string]any{"name": "PartnerB"})
	oids, err := orderModel.Tx(ss).Create(map[string]any{"name": "SO-M2O", "partner_id": pidsA[0]})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	orderId := oids[0]
	if err := ss.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// 前端 Many2OneField.ts 的 commit([id, name]) 写值形态:经典 [id,name] 元组,
	// 而非裸 id。
	partnerBId := pidsB[0]
	partnerBIdStr := fmt.Sprint(partnerBId)
	effect, err := orderModel.Records().Ids(orderId).Write(map[string]any{
		"partner_id": []any{partnerBIdStr, "PartnerB"},
	})
	if err != nil {
		t.Fatalf("write partner_id tuple: %v", err)
	}
	if effect == 0 {
		t.Fatalf("write 期望影响 >0 行, 实际 %d", effect)
	}

	// 断言只读裸值(非 ClassicRead),只验证本仓的写入分发修复,不依赖 dataset 仓
	// AsMap 复合值格式化的另一处独立修复(见该仓 recordset.go)。
	rds, err := orderModel.Read(&orm.ReadRequest{
		Ids:    []any{orderId},
		Fields: []string{"id", "partner_id"},
	})
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if rds.Count() != 1 {
		t.Fatalf("期望 1 条 order, 实际 %d", rds.Count())
	}
	got := rds.Record().GetByField("partner_id")
	want := fmt.Sprint(partnerBId)
	if fmt.Sprint(got) != want {
		t.Fatalf("partner_id 期望写入后为 PartnerB 的 id %s, 实际 %T = %#v (若为空串/0,说明元组被错误地当 BigInt 解析退回0)", want, got, got)
	}
}

// TestFlatClassicReadO2MIdAsString 回归:BigNumberToString 打开时,o2m 的裸 id 列表
// 必须以**字符串**回填。否则雪花 id 以 JSON number 传给前端会丢精度(> 2^53),x2many
// 的 loadSeeded 按舍入后的错 id 查询得到空结果,o2m 列表显示不出数据。
//
// (m2o 内嵌子记录被标量格式化器压坏的问题在 dataset 仓的 AsMap 测试里覆盖。)
func TestFlatClassicReadO2MIdAsString(t *testing.T) {
	o := newFlatClassicOrm(t)

	partnerModel, _ := o.GetModel("fc_partner")
	lineModel, _ := o.GetModel("fc_line")
	orderModel, _ := o.GetModel("fc_order")

	ss := o.NewSession()
	defer ss.Close()
	if err := ss.Begin(); err != nil {
		t.Fatal(err)
	}

	pids, _ := partnerModel.Tx(ss).Create(map[string]any{"name": "ACME", "color": 7})
	oids, err := orderModel.Tx(ss).Create(map[string]any{"name": "SO001", "partner_id": pids[0]})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	orderId := oids[0]
	lineModel.Tx(ss).Create(map[string]any{"name": "L1", "qty": 2, "order_id": orderId})
	lineModel.Tx(ss).Create(map[string]any{"name": "L2", "qty": 5, "order_id": orderId})
	if err := ss.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// 平铺 classic read —— 正是 form_model.ts load() 发出的请求形态。
	rds, err := orderModel.Read(&orm.ReadRequest{
		Ids:         []any{orderId},
		Fields:      []string{"id", "name", "partner_id", "lines"},
		ClassicRead: true,
	})
	if err != nil {
		t.Fatalf("flat classic Read: %v", err)
	}
	if rds.Count() != 1 {
		t.Fatalf("期望 1 条 order, 实际 %d", rds.Count())
	}
	m := rds.Record().AsMap()

	lines, ok := m["lines"].([]any)
	if !ok || len(lines) != 2 {
		t.Fatalf("lines 期望 2 个 id, 实际 %T = %#v", m["lines"], m["lines"])
	}
	for _, id := range lines {
		if _, isStr := id.(string); !isStr {
			t.Fatalf("BigNumberToString 下 o2m 的 id 期望字符串, 实际 %T = %v", id, id)
		}
	}
}

// TestFlatClassicReadEmptyO2MKeyStillPresent 回归:one2many 字段一条关联行都没有时
// (刚创建、还没加过明细行的订单),字段 key 此前会从输出里彻底消失(不是 [],是键都不
// 存在)——TOne2ManyField.OnRead 只在 grp.Count()>0 时才调用 SetByField。调用方/前端
// 把"键缺失"误判成"关系字段没有返回"。修复后无论有无关联行都调用 SetByField,空关联
// 落一个空切片。
func TestFlatClassicReadEmptyO2MKeyStillPresent(t *testing.T) {
	o := newFlatClassicOrm(t)

	orderModel, _ := o.GetModel("fc_order")

	ss := o.NewSession()
	defer ss.Close()
	if err := ss.Begin(); err != nil {
		t.Fatal(err)
	}

	// 订单没有任何 partner_id、没有任何 lines —— 关联侧完全空。
	oids, err := orderModel.Tx(ss).Create(map[string]any{"name": "SO-EMPTY"})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	orderId := oids[0]
	if err := ss.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	rds, err := orderModel.Read(&orm.ReadRequest{
		Ids:         []any{orderId},
		Fields:      []string{"id", "name", "lines"},
		ClassicRead: true,
	})
	if err != nil {
		t.Fatalf("flat classic Read: %v", err)
	}
	if rds.Count() != 1 {
		t.Fatalf("期望 1 条 order, 实际 %d", rds.Count())
	}
	m := rds.Record().AsMap()

	lines, hasKey := m["lines"]
	if !hasKey {
		t.Fatalf("lines 键完全缺失(应始终存在,即使是空数组)")
	}
	arr, ok := lines.([]any)
	if !ok {
		t.Fatalf("lines 期望 []any, 实际 %T = %#v", lines, lines)
	}
	if len(arr) != 0 {
		t.Fatalf("lines 期望空切片, 实际 len=%d", len(arr))
	}
}
