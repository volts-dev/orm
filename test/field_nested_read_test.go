package test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/volts-dev/orm"
	"github.com/volts-dev/utils"

	_ "modernc.org/sqlite"
)

// 嵌套单次读取用的独立模型集合:
//
//	nr_order --(m2o partner_id)--> nr_partner
//	nr_order <--(o2m lines, 反向 FK order_id)-- nr_line
//
// 用于验证: 单次 read 通过 ReadRequest.SubFields 把 m2o 子记录(map)与 o2m 子记录列表
// 内嵌进返回数据集, 子字段范围由各自 Fields 限定 —— 即组件无需独立调用 API。
type (
	NRPartner struct {
		orm.TModel `table:"name('nr_partner')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Name       string `field:"varchar() required"`
		Color      int    `field:"int()"`
		Extra      string `field:"varchar()"`
	}

	NRLine struct {
		orm.TModel `table:"name('nr_line')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Name       string `field:"varchar()"`
		Qty        int    `field:"int()"`
		Secret     string `field:"varchar()"`
		OrderId    int64  `field:"many2one(nr_order)"`
	}

	NROrder struct {
		orm.TModel `table:"name('nr_order')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Name       string `field:"varchar() required"`
		PartnerId  int64  `field:"many2one(nr_partner)"`
		Lines      []any  `field:"one2many(nr_line,order_id)"`
	}
)

func newNestedReadOrm(t *testing.T) *orm.TOrm {
	t.Helper()
	ds := &orm.TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "nested.db")}
	o, err := orm.New(orm.WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("test", new(NRPartner), new(NRLine), new(NROrder)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	if err := o.Freeze(context.Background()); err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	return o
}

// TestNestedReadSubFields 验证单次 read 内嵌 m2o/o2m 子记录, 且子字段范围受 Fields 限定。
func TestNestedReadSubFields(t *testing.T) {
	o := newNestedReadOrm(t)

	partnerModel, _ := o.GetModel("nr_partner")
	lineModel, _ := o.GetModel("nr_line")
	orderModel, _ := o.GetModel("nr_order")

	ss := o.NewSession()
	defer ss.Close()
	if err := ss.Begin(); err != nil {
		t.Fatal(err)
	}

	pids, err := partnerModel.Tx(ss).Create(map[string]any{"name": "ACME", "color": 7, "extra": "should-not-appear"})
	if err != nil {
		t.Fatalf("create partner: %v", err)
	}
	partnerId := pids[0]

	oids, err := orderModel.Tx(ss).Create(map[string]any{"name": "SO001", "partner_id": partnerId})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	orderId := oids[0]

	if _, err := lineModel.Tx(ss).Create(map[string]any{"name": "L1", "qty": 2, "secret": "x", "order_id": orderId}); err != nil {
		t.Fatalf("create line1: %v", err)
	}
	if _, err := lineModel.Tx(ss).Create(map[string]any{"name": "L2", "qty": 5, "secret": "y", "order_id": orderId}); err != nil {
		t.Fatalf("create line2: %v", err)
	}
	if err := ss.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// 单次 read: 主字段 id/name + 嵌套规格 partner_id(m2o)/lines(o2m)。
	rds, err := orderModel.Read(&orm.ReadRequest{
		Ids:    []any{orderId},
		Fields: []string{"id", "name"},
		SubFields: map[string]*orm.ReadRequest{
			"partner_id": {Fields: []string{"name", "color"}},
			"lines":      {Fields: []string{"name", "qty"}},
		},
	})
	if err != nil {
		t.Fatalf("nested Read: %v", err)
	}
	if rds.Count() != 1 {
		t.Fatalf("期望 1 条 order, 实际 %d", rds.Count())
	}
	rec := rds.Record()

	// --- m2o: partner_id 应内嵌为含 name+color 的子记录(map) ---
	partnerVal := rec.GetByField("partner_id")
	pm, ok := partnerVal.(map[string]any)
	if !ok {
		t.Fatalf("partner_id 应内嵌为 map 子记录, 实际类型 %T = %v", partnerVal, partnerVal)
	}
	if got := utils.ToString(pm["name"]); got != "ACME" {
		t.Fatalf("内嵌 partner.name=%q, want ACME", got)
	}
	if got := utils.ToString(pm["color"]); got != "7" {
		t.Fatalf("内嵌 partner.color=%q, want 7", got)
	}
	t.Logf("m2o 内嵌 partner_id=%v ✓", pm)

	// --- o2m: lines 应内嵌为完整子记录列表(map), 而非仅 id ---
	linesVal := rec.GetByField("lines")
	lm, ok := linesVal.([]map[string]any)
	if !ok {
		t.Fatalf("lines 应内嵌为 []map 子记录列表, 实际类型 %T = %v", linesVal, linesVal)
	}
	if len(lm) != 2 {
		t.Fatalf("期望内嵌 2 条 line, 实际 %d: %v", len(lm), lm)
	}
	names := map[string]string{}
	for _, l := range lm {
		names[utils.ToString(l["name"])] = utils.ToString(l["qty"])
	}
	if names["L1"] != "2" || names["L2"] != "5" {
		t.Fatalf("内嵌 lines 的 name/qty 不符: %v", names)
	}
	t.Logf("o2m 内嵌 lines=%v ✓", lm)
}
