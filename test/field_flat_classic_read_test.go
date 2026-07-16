package test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/volts-dev/orm"

	_ "modernc.org/sqlite"
)

// 复现 form view 的实际读取路径:前端发的是**平铺** ReadRequest(Fields 为 arch 字段名、
// ClassicRead=true、无 SubFields)。本测试聚焦 one2many 在该路径下的回填形态。
//
// 回归点(orm 侧):BigNumberToString 打开时,o2m 的裸 id 列表必须以**字符串**回填。
// 否则雪花 id 以 JSON number 传给前端会丢精度(> 2^53),x2many 的 loadSeeded 按舍入后的
// 错 id 查询得到空结果,o2m 列表显示不出数据。
//
// (m2o 内嵌子记录被标量格式化器压坏的问题在 dataset 仓的 AsMap 测试里覆盖。)
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
