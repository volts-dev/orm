package test

import (
	"testing"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/orm"
	"github.com/volts-dev/orm/domain"

	_ "modernc.org/sqlite"
)

// lineIds 取某个父记录名下子行的 id（lineRows 只回 product→qty，这里要 id）。
func lineIds(t *testing.T, o *orm.TOrm, model string, orderId any) []any {
	t.Helper()
	m, err := o.GetModel(model)
	if err != nil {
		t.Fatal(err)
	}
	ds, err := m.Records().Domain(domain.New("order_id", "=", orderId)).Read()
	if err != nil {
		t.Fatalf("read %s: %v", model, err)
	}
	out := []any{}
	if ds == nil {
		return out
	}
	ds.Range(func(_ int, rec *dataset.TRecordSet) error {
		out = append(out, rec.GetByField("id"))
		return nil
	})
	return out
}

// o2m 的解绑/替换命令只许动**本父记录**的子行。
//
// 真机 2026-08-04（kylin，产品模板 Prices 页）：删掉列表里的一行，四行全没了。日志：
//
//	SELECT "pro_pricelist_item"."id" FROM system.pro_pricelist_item
//	  WHERE tenant_id = $1 AND (公司可见性规则)          ← 没有父记录条件、没有 id 条件
//	DELETE FROM "system"."pro_pricelist_item" WHERE "id" in ($1,$2,$3,$4)
//
// 那次只是恰好四行都挂在同一个产品下，所以看着像"这张列表被清空了"；换成别的产品
// 也有价格规则，一样会被删掉——这是**跨记录的数据销毁**，不是显示问题。
//
// 命令 5/6 都会先 o2mChildIds 取"当前子行"再删差集，父记录条件一旦丢掉，"当前子行"
// 就变成整张表。下面用两个父记录钉死：动 A 不许碰 B。
func TestOne2ManyClearOnlyTouchesItsOwnParent(t *testing.T) {
	o := newO2MOrm(t)
	order, err := o.GetModel("o2m_order")
	if err != nil {
		t.Fatal(err)
	}

	aIds, err := order.Records().Create(map[string]any{
		"name": "A",
		"line_ids": []any{
			[]any{0, 0, map[string]any{"product": "A-1", "qty": 1}},
			[]any{0, 0, map[string]any{"product": "A-2", "qty": 2}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	bIds, err := order.Records().Create(map[string]any{
		"name": "B",
		"line_ids": []any{
			[]any{0, 0, map[string]any{"product": "B-1", "qty": 1}},
			[]any{0, 0, map[string]any{"product": "B-2", "qty": 2}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := lineRows(t, o, "o2m_line", bIds[0]); len(got) != 2 {
		t.Fatalf("前置条件不成立，B 应有 2 行，实际 %d", len(got))
	}

	// 清空 A
	if _, err := order.Records().Ids(aIds[0]).Write(map[string]any{
		"line_ids": []any{[]any{5}},
	}); err != nil {
		t.Fatalf("clear A: %v", err)
	}

	if got := lineRows(t, o, "o2m_line", aIds[0]); len(got) != 0 {
		t.Fatalf("A 的子行应被清空，实际还有 %d 行", len(got))
	}
	if got := lineRows(t, o, "o2m_line", bIds[0]); len(got) != 2 {
		t.Fatalf("**B 的子行被连坐删了**：应有 2 行，实际 %d 行——父记录条件丢了", len(got))
	}
}

// 命令 6（设置为这批）同样只许在本父记录的范围内算差集。
func TestOne2ManySetOnlyTouchesItsOwnParent(t *testing.T) {
	o := newO2MOrm(t)
	order, err := o.GetModel("o2m_order")
	if err != nil {
		t.Fatal(err)
	}

	aIds, err := order.Records().Create(map[string]any{
		"name": "A",
		"line_ids": []any{
			[]any{0, 0, map[string]any{"product": "A-1", "qty": 1}},
			[]any{0, 0, map[string]any{"product": "A-2", "qty": 2}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	bIds, err := order.Records().Create(map[string]any{
		"name": "B",
		"line_ids": []any{
			[]any{0, 0, map[string]any{"product": "B-1", "qty": 1}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	aLines := lineIds(t, o, "o2m_line", aIds[0])
	if len(aLines) != 2 {
		t.Fatalf("前置条件不成立，A 应有 2 行，实际 %d", len(aLines))
	}
	keep := aLines[0]

	// A 只保留第一行
	if _, err := order.Records().Ids(aIds[0]).Write(map[string]any{
		"line_ids": []any{[]any{6, 0, []any{keep}}},
	}); err != nil {
		t.Fatalf("set A: %v", err)
	}

	if got := lineRows(t, o, "o2m_line", aIds[0]); len(got) != 1 {
		t.Fatalf("A 应只剩 1 行，实际 %d 行", len(got))
	}
	if got := lineRows(t, o, "o2m_line", bIds[0]); len(got) != 1 {
		t.Fatalf("**B 的子行被连坐删了**：应有 1 行，实际 %d 行——差集算到别的父记录上去了", len(got))
	}
}

// 命令 2（删除某一行）只删点名的那一行。真机上"删一行、其它全没"就是这条的反面。
func TestOne2ManyDeleteRemovesOnlyTheNamedRow(t *testing.T) {
	o := newO2MOrm(t)
	order, err := o.GetModel("o2m_order")
	if err != nil {
		t.Fatal(err)
	}

	aIds, err := order.Records().Create(map[string]any{
		"name": "A",
		"line_ids": []any{
			[]any{0, 0, map[string]any{"product": "A-1", "qty": 1}},
			[]any{0, 0, map[string]any{"product": "A-2", "qty": 2}},
			[]any{0, 0, map[string]any{"product": "A-3", "qty": 3}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	bIds, err := order.Records().Create(map[string]any{
		"name":     "B",
		"line_ids": []any{[]any{0, 0, map[string]any{"product": "B-1", "qty": 1}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	aLines := lineIds(t, o, "o2m_line", aIds[0])
	if len(aLines) != 3 {
		t.Fatalf("前置条件不成立，A 应有 3 行，实际 %d", len(aLines))
	}

	if _, err := order.Records().Ids(aIds[0]).Write(map[string]any{
		"line_ids": []any{[]any{2, aLines[1]}},
	}); err != nil {
		t.Fatalf("delete one line: %v", err)
	}

	got := lineRows(t, o, "o2m_line", aIds[0])
	if len(got) != 2 {
		t.Fatalf("A 应剩 2 行，实际 %d 行", len(got))
	}
	if got := lineRows(t, o, "o2m_line", bIds[0]); len(got) != 1 {
		t.Fatalf("**B 的子行被连坐删了**：应有 1 行，实际 %d 行", len(got))
	}
}
