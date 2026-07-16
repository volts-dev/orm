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
// ClassicRead=true、无 SubFields)。本文件聚焦 many2one 在该路径下的写入形态。
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

