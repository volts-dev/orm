package test

import (
	"path/filepath"
	"testing"

	"github.com/volts-dev/orm"

	_ "modernc.org/sqlite"
)

// 复现真机形态：comodel 上挂着"记录规则"——上层(vectors core/model 的 withSession)
// 在 BeforeSession 里按操作类型往会话追加 tenant_id/company_id 条件。
//
// 真机 2026-08-04 观察到的 SQL 是：
//
//	SELECT id FROM system.pro_pricelist_item WHERE (tenant_id = $1) AND (公司可见性)
//
// ——**只有记录规则，没有父记录条件**，紧接着一条 DELETE 把选出来的全删了。
// 纯 orm 模型上限定是好的（field_o2m_scope_test.go 三条都过），所以嫌疑落在
// "BeforeSession 追加条件"与 o2mChildIds 传进去的 Domain 谁覆盖谁。
type o2mRuledLineModel struct {
	orm.TModel `table:"name('o2m_ruled_line')"`
	Id         int64  `field:"pk autoincr title('ID') index"`
	OrderId    int64  `field:"many2one(o2m_ruled_order) required index"`
	Product    string `field:"varchar()"`
	Tenant     int    `field:"int() default(1)"`
}

// BeforeSession 模拟 vectors 的 withSession：读取时按租户过滤。
func (self *o2mRuledLineModel) BeforeSession(s *orm.TSession) (*orm.TSession, error) {
	if s.Op == orm.OpRead || s.Op == orm.OpCount {
		s.Where("tenant=?", 1)
	}
	return s, nil
}

type o2mRuledOrderModel struct {
	orm.TModel `table:"name('o2m_ruled_order')"`
	Id         int64   `field:"pk autoincr title('ID') index"`
	Name       string  `field:"varchar() required"`
	LineIds    []int64 `field:"one2many(o2m_ruled_line,order_id) title('Lines')"`
}

func newRuledOrm(t *testing.T) *orm.TOrm {
	t.Helper()
	ds := &orm.TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "o2m_ruled.db")}
	o, err := orm.New(orm.WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("test", new(o2mRuledOrderModel), new(o2mRuledLineModel)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	return o
}

func ruledLineCount(t *testing.T, o *orm.TOrm, orderId any) int {
	t.Helper()
	m, err := o.GetModel("o2m_ruled_line")
	if err != nil {
		t.Fatal(err)
	}
	ds, err := m.Records().Where("order_id=?", orderId).Read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if ds == nil {
		return 0
	}
	return ds.Count()
}

// 子模型带记录规则时，清空 A 不许波及 B。
func TestOne2ManyClearWithRecordRuleStaysScoped(t *testing.T) {
	o := newRuledOrm(t)
	order, err := o.GetModel("o2m_ruled_order")
	if err != nil {
		t.Fatal(err)
	}

	aIds, err := order.Records().Create(map[string]any{
		"name": "A",
		"line_ids": []any{
			[]any{0, 0, map[string]any{"product": "A-1"}},
			[]any{0, 0, map[string]any{"product": "A-2"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	bIds, err := order.Records().Create(map[string]any{
		"name": "B",
		"line_ids": []any{
			[]any{0, 0, map[string]any{"product": "B-1"}},
			[]any{0, 0, map[string]any{"product": "B-2"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := ruledLineCount(t, o, bIds[0]); n != 2 {
		t.Fatalf("前置条件不成立，B 应有 2 行，实际 %d", n)
	}

	if _, err := order.Records().Ids(aIds[0]).Write(map[string]any{
		"line_ids": []any{[]any{5}},
	}); err != nil {
		t.Fatalf("clear A: %v", err)
	}

	if n := ruledLineCount(t, o, aIds[0]); n != 0 {
		t.Fatalf("A 的子行应被清空，实际还有 %d 行", n)
	}
	if n := ruledLineCount(t, o, bIds[0]); n != 2 {
		t.Fatalf("**B 的子行被连坐删了**：应有 2 行，实际 %d 行——父记录条件被记录规则挤掉了", n)
	}
}
