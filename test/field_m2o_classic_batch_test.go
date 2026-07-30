package test

import (
	"fmt"
	"testing"

	"github.com/volts-dev/orm"

	_ "modernc.org/sqlite"
)

// 复现列表/表单读一**批**记录时 many2one 的经典回填。真实形态就是 res.partner 的列表:
// 一页里既有 company_id/user_id 填了的记录,也有没填的(m2o 本就可空),还可能有指向已删
// 记录的悬空 FK。
//
// TMany2OneField.OnRead 用 ctx.Dataset.Range 逐条回填,而 Range 一旦回调返回 error 就
// **整个中断**(dataset.go:Range)。此前只要有一条记录的 FK 匹配不到 comodel 行
// (NULL / 0 / 悬空 / 跨租户不可见),回调就返回 "has more than 1 record" 错误,于是:
//
//   - 该记录之后的**所有**记录都拿不到内嵌子记录,字段只剩裸 id;
//   - 错误在 _read() 里只是 log.Errf,请求照常 200 返回。
//
// 前端 toM2OTuple 拿到裸 id 会退化成 [id, String(id)],界面上显示的就是一串数字 id 而
// 不是名称——这正是 "call_kw/res.partner/read 很多 many2one 只返回 id" 的现场。
func newM2OBatchOrm(t *testing.T) *orm.TOrm {
	t.Helper()
	return newFlatClassicOrm(t)
}

func TestM2OClassicReadBatchWithBlankFK(t *testing.T) {
	o := newM2OBatchOrm(t)

	partnerModel, _ := o.GetModel("fc_partner")
	orderModel, _ := o.GetModel("fc_order")

	ss := o.NewSession()
	defer ss.Close()
	if err := ss.Begin(); err != nil {
		t.Fatal(err)
	}
	pidsA, _ := partnerModel.Tx(ss).Create(map[string]any{"name": "ACME"})
	pidsB, _ := partnerModel.Tx(ss).Create(map[string]any{"name": "Globex"})

	// 四条订单: 有 FK / 无 FK(m2o 可空) / 哨兵 -1(省略 m2o 的 create 落的就是它,
	// 真栈 res_partner.company_id 约 6/7 的行是 -1) / 有 FK。空的那两条夹在中间,
	// 才能验证它们不会把后面那条的回填一起带走。
	o1, _ := orderModel.Tx(ss).Create(map[string]any{"name": "SO-1", "partner_id": pidsA[0]})
	o2, _ := orderModel.Tx(ss).Create(map[string]any{"name": "SO-2"})
	o4, _ := orderModel.Tx(ss).Create(map[string]any{"name": "SO-SENTINEL", "partner_id": -1})
	o3, _ := orderModel.Tx(ss).Create(map[string]any{"name": "SO-3", "partner_id": pidsB[0]})
	if err := ss.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	rds, err := orderModel.Read(&orm.ReadRequest{
		Ids:         []any{o1[0], o2[0], o4[0], o3[0]},
		Fields:      []string{"id", "name", "partner_id"},
		ClassicRead: true,
	})
	if err != nil {
		t.Fatalf("flat classic Read: %v", err)
	}
	if rds.Count() != 4 {
		t.Fatalf("期望 4 条 order, 实际 %d", rds.Count())
	}

	want := map[string]string{"SO-1": "ACME", "SO-3": "Globex"}
	for _, rec := range rds.Data {
		m := rec.AsMap()
		name := fmt.Sprint(m["name"])
		got := m["partner_id"]
		if wantPartner, has := want[name]; has {
			sub, ok := got.(map[string]any)
			if !ok {
				t.Errorf("%s 的 partner_id 期望内嵌子记录 map, 实际 %T = %#v "+
					"(裸 id 会让前端显示成一串数字而不是名称)", name, got, got)
				continue
			}
			if fmt.Sprint(sub["name"]) != wantPartner {
				t.Errorf("%s 的 partner_id.name = %v, want %s", name, sub["name"], wantPartner)
			}
			continue
		}
		// 空 FK: 不内嵌是对的,但键必须还在,且不能是别人的记录。
		if _, isMap := got.(map[string]any); isMap {
			t.Errorf("%s 没有 partner_id, 不该内嵌到任何子记录: %#v", name, got)
		}
	}
}

// TestM2OClassicReadDanglingFK 覆盖悬空外键:FK 有值但 comodel 里那条已被删除。
// 该记录自身读成裸 id 是可接受的降级,但绝不能连累同一批里其它记录的回填。
func TestM2OClassicReadDanglingFK(t *testing.T) {
	o := newM2OBatchOrm(t)

	partnerModel, _ := o.GetModel("fc_partner")
	orderModel, _ := o.GetModel("fc_order")

	ss := o.NewSession()
	defer ss.Close()
	if err := ss.Begin(); err != nil {
		t.Fatal(err)
	}
	pidsA, _ := partnerModel.Tx(ss).Create(map[string]any{"name": "ACME"})
	pidsGone, _ := partnerModel.Tx(ss).Create(map[string]any{"name": "Deleted"})
	o1, _ := orderModel.Tx(ss).Create(map[string]any{"name": "SO-DANGLING", "partner_id": pidsGone[0]})
	o2, _ := orderModel.Tx(ss).Create(map[string]any{"name": "SO-OK", "partner_id": pidsA[0]})
	if err := ss.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if _, err := o.NewSession().Exec(`DELETE FROM fc_partner WHERE id=?`, pidsGone[0]); err != nil {
		t.Fatalf("删除 partner 失败: %v", err)
	}

	rds, err := orderModel.Read(&orm.ReadRequest{
		Ids:         []any{o1[0], o2[0]},
		Fields:      []string{"id", "name", "partner_id"},
		ClassicRead: true,
	})
	if err != nil {
		t.Fatalf("flat classic Read: %v", err)
	}
	if rds.Count() != 2 {
		t.Fatalf("期望 2 条 order, 实际 %d", rds.Count())
	}

	for _, rec := range rds.Data {
		m := rec.AsMap()
		if fmt.Sprint(m["name"]) != "SO-OK" {
			continue
		}
		sub, ok := m["partner_id"].(map[string]any)
		if !ok {
			t.Fatalf("SO-OK 的 partner_id 期望内嵌子记录 map, 实际 %T = %#v "+
				"(同批里另一条的悬空 FK 中断了整轮回填)", m["partner_id"], m["partner_id"])
		}
		if fmt.Sprint(sub["name"]) != "ACME" {
			t.Fatalf("SO-OK 的 partner_id.name = %v, want ACME", sub["name"])
		}
	}
}
