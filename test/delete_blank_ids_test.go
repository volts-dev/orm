package test

import (
	"testing"

	_ "modernc.org/sqlite"
)

// 「点了名的删除」绝不能退化成「按当前域删」。
//
// TSession.Delete 里原本是：没有 id → _search 出当前域下的全部行 → 删。那条
// "无条件删除"的保险用 hasCondition() 判定，而多租户上层(vectors withSession)会在
// BeforeSession 里追加 tenant_id/company_id 记录规则，于是"有条件"恒成立，保险形同虚设。
//
// 真机 2026-08-04：前端把一条**没有 id** 的幽灵行交给删除，发来 [2, null]，到这里
// id 列表是空的，于是：
//
//	SELECT id FROM system.pro_pricelist_item WHERE tenant_id=$1 AND (公司规则)
//	DELETE FROM system.pro_pricelist_item WHERE id in ($1,$2)
//
// 该租户可见的每一行都没了。记录规则是**可见性**，不是调用方给的删除范围。
func TestDeleteWithBlankIdsRefusesInsteadOfWipingByDomain(t *testing.T) {
	o := newO2MOrm(t)
	line, err := o.GetModel("o2m_line")
	if err != nil {
		t.Fatal(err)
	}
	order, err := o.GetModel("o2m_order")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := order.Records().Create(map[string]any{
		"name": "A",
		"line_ids": []any{
			[]any{0, 0, map[string]any{"product": "A-1", "qty": 1}},
			[]any{0, 0, map[string]any{"product": "A-2", "qty": 2}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	countAll := func() int {
		ds, err := line.Records().Read()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if ds == nil {
			return 0
		}
		return ds.Count()
	}
	if n := countAll(); n != 2 {
		t.Fatalf("前置条件不成立，应有 2 行，实际 %d", n)
	}

	// 点了名、但名字是空的——正是前端幽灵行发来的形状。
	for _, blank := range []any{nil, "", int64(0)} {
		if _, err := line.Records().Delete(blank); err == nil {
			t.Fatalf("Delete(%#v) 应当报错而不是按域删", blank)
		}
		if n := countAll(); n != 2 {
			t.Fatalf("Delete(%#v) 之后行数变了：应有 2 行，实际 %d —— 退化成按域删了", blank, n)
		}
	}
}

// o2m 的命令 1/2/3/4 必须真的带 id，带不动就报错，不许把空 id 往下传。
func TestOne2ManyCommandWithoutIdIsRefused(t *testing.T) {
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
		"name": "A",
		"line_ids": []any{
			[]any{0, 0, map[string]any{"product": "A-1", "qty": 1}},
			[]any{0, 0, map[string]any{"product": "A-2", "qty": 2}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	countAll := func() int {
		ds, err := line.Records().Read()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if ds == nil {
			return 0
		}
		return ds.Count()
	}

	for _, cmd := range [][]any{
		{2, nil},  // 删除，没有 id —— 幽灵行发来的就是这个
		{3, nil},  // 解绑，没有 id
		{1, nil, map[string]any{"qty": 9}}, // 更新，没有 id
	} {
		_, err := order.Records().Ids(ids[0]).Write(map[string]any{
			"line_ids": []any{cmd},
		})
		if err == nil {
			t.Fatalf("命令 %v 没有 id，应当报错", cmd)
		}
		if n := countAll(); n != 2 {
			t.Fatalf("命令 %v 之后行数变了：应有 2 行，实际 %d", cmd, n)
		}
	}
}
