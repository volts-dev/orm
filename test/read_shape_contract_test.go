package test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/orm"

	_ "modernc.org/sqlite"
)

// 读出口的**形态契约**。三件事在这里钉死，它们都属于"同一个字段、同一条读取路径，
// 形态却随数据或随实现细节变"的一类，调用方每一处都得多写一个分支才能不崩：
//
//	P1  经典读的 many2one：有值 map / 没值 false，**只有这两种**
//	P2  plain 读的 o2m/m2m：点了名就一定出键，且是真值；没点名维持不出键
//	P4  BigNumberToString：GetByField 与 AsMap 必须给出同一个值
type (
	RSPartner struct {
		orm.TModel `table:"name('rs_partner')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Name       string `field:"varchar() required"`
	}

	RSLine struct {
		orm.TModel `table:"name('rs_line')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Name       string `field:"varchar()"`
		Qty        int    `field:"int()"` // 用来验证子规格没点名的列不会回来
		OrderId    int64  `field:"many2one(rs_order)"`
	}

	RSOrder struct {
		orm.TModel `table:"name('rs_order')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Name       string `field:"varchar() required"`
		Sequence   int64  `field:"bigint"`
		PartnerId  int64  `field:"many2one(rs_partner)"`
		Lines      []any  `field:"one2many(rs_line,order_id)"`
	}
)

func newReadShapeOrm(t *testing.T) *orm.TOrm {
	t.Helper()
	ds := &orm.TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "shape.db")}
	o, err := orm.New(orm.WithDataSource(ds), orm.WithBigNumberToString(true))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	t.Cleanup(func() { o.Close() })
	if _, err := o.SyncModel("test", new(RSPartner), new(RSLine), new(RSOrder)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	if err := o.Freeze(context.Background()); err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	return o
}

// ---------------------------------------------------------------- P1

// TestClassicRead_M2OShapeIsMapOrFalse 钉死经典读下 many2one 只有两种形态。
//
// 从前空外键是**原值不动**：裸 int64(0)（或导入落的 -1，或 BigNumberToString 打开
// 时的 ""），而有值时是 map。同一个字段同一条路径，形态随数据变——前端把没选值的
// m2o 渲染成字面量 "0" 并给出一个点不开的链接，就是这么来的。现在统一成 Odoo 的
// false。
func TestClassicRead_M2OShapeIsMapOrFalse(t *testing.T) {
	o := newReadShapeOrm(t)
	partnerModel, _ := o.GetModel("rs_partner")
	orderModel, _ := o.GetModel("rs_order")

	pids, err := partnerModel.Records().Create(map[string]any{"name": "P1"})
	if err != nil {
		t.Fatalf("create partner: %v", err)
	}
	withFK, err := orderModel.Records().Create(map[string]any{"name": "has-fk", "partner_id": pids[0]})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	noFK, err := orderModel.Records().Create(map[string]any{"name": "no-fk"})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}

	// 两条一起读：空外键的归一必须在**同一批**里与有值的一起生效。
	// (回归点：从前这段代码整个包在 `if ds.Count() > 0` 里，一批记录若外键全为空，
	//  对端查询回 0 行，于是连归一都不做，全批退回旧形态。)
	ds, err := orderModel.Read(&orm.ReadRequest{
		Ids:         []any{withFK[0], noFK[0]},
		Fields:      []string{"id", "name", "partner_id"},
		ClassicRead: true,
	})
	if err != nil {
		t.Fatalf("classic read: %v", err)
	}
	if ds.Count() != 2 {
		t.Fatalf("期望 2 条, 实际 %d", ds.Count())
	}

	got := map[string]any{}
	ds.Range(func(_ int, rec *dataset.TRecordSet) error {
		m := rec.AsMap()
		got[m["name"].(string)] = m["partner_id"]
		return nil
	})

	if m, ok := got["has-fk"].(map[string]any); !ok {
		t.Errorf("有值的 partner_id 期望 map{id,name}, 实际 %T = %#v", got["has-fk"], got["has-fk"])
	} else if m["name"] != "P1" {
		t.Errorf("partner_id.name 期望 P1, 实际 %#v", m["name"])
	}

	if got["no-fk"] != false {
		t.Errorf("空 partner_id 期望 false（Odoo 的空关系）, 实际 %T = %#v —— 裸标量说明形态归一回归了",
			got["no-fk"], got["no-fk"])
	}
}

// TestClassicRead_M2ODanglingKeepsMapShape 悬空/不可见外键同样不退回裸标量。
//
// 取不到对端记录是合法的降级（对方被删、被租户或记录规则挡掉），但降级只该丢**名字**，
// 不该换**形态**——否则调用方为了这一种情况就得再写一个标量分支。
func TestClassicRead_M2ODanglingKeepsMapShape(t *testing.T) {
	o := newReadShapeOrm(t)
	partnerModel, _ := o.GetModel("rs_partner")
	orderModel, _ := o.GetModel("rs_order")

	pids, _ := partnerModel.Records().Create(map[string]any{"name": "doomed"})
	oids, err := orderModel.Records().Create(map[string]any{"name": "dangling", "partner_id": pids[0]})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if _, err := partnerModel.Records().Ids(pids[0]).Delete(); err != nil {
		t.Fatalf("delete partner: %v", err)
	}

	ds, err := orderModel.Read(&orm.ReadRequest{
		Ids:         []any{oids[0]},
		Fields:      []string{"id", "partner_id"},
		ClassicRead: true,
	})
	if err != nil {
		t.Fatalf("classic read: %v", err)
	}
	v := ds.Record().AsMap()["partner_id"]
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("悬空 partner_id 期望仍是 map（只带 id）, 实际 %T = %#v", v, v)
	}
	if len(m) != 1 || m["id"] == nil {
		t.Fatalf("悬空 partner_id 期望 map{id}, 实际 %#v", m)
	}
}

// ---------------------------------------------------------------- P2

// TestPlainRead_NamedO2MAlwaysPresent plain 读里点了名的 o2m 必须出键、出真值。
//
// 从前 `Select("id","name","lines")` 这种写法回来的记录里**根本没有 lines 这个键**
// ——不是空数组，是键不存在：o2m 无物理列不进 SELECT，而 plain 读那段派发对
// IsRelated() 一律 continue。请求成功、无日志，调用方只看到"关系字段没返回"。
func TestPlainRead_NamedO2MAlwaysPresent(t *testing.T) {
	o := newReadShapeOrm(t)
	orderModel, _ := o.GetModel("rs_order")
	lineModel, _ := o.GetModel("rs_line")

	withLines, _ := orderModel.Records().Create(map[string]any{"name": "has-lines"})
	empty, _ := orderModel.Records().Create(map[string]any{"name": "no-lines"})
	if _, err := lineModel.Records().Create(map[string]any{"name": "L1", "order_id": withLines[0]}); err != nil {
		t.Fatalf("create line: %v", err)
	}
	if _, err := lineModel.Records().Create(map[string]any{"name": "L2", "order_id": withLines[0]}); err != nil {
		t.Fatalf("create line: %v", err)
	}

	ds, err := orderModel.Read(&orm.ReadRequest{
		Ids:    []any{withLines[0], empty[0]},
		Fields: []string{"id", "name", "lines"},
	})
	if err != nil {
		t.Fatalf("plain read: %v", err)
	}

	got := map[string]any{}
	present := map[string]bool{}
	ds.Range(func(_ int, rec *dataset.TRecordSet) error {
		m := rec.AsMap()
		name := m["name"].(string)
		got[name], present[name] = m["lines"], func() bool { _, ok := m["lines"]; return ok }()
		return nil
	})

	for _, name := range []string{"has-lines", "no-lines"} {
		if !present[name] {
			t.Fatalf("%s: 点了名的 lines 键必须存在（哪怕是空数组）", name)
		}
	}

	lines, ok := got["has-lines"].([]any)
	if !ok {
		t.Fatalf("有子行时 lines 期望 []any 的 id 列表, 实际 %T = %#v", got["has-lines"], got["has-lines"])
	}
	if len(lines) != 2 {
		t.Errorf("lines 期望 2 个 id, 实际 %d: %#v —— 空数组说明它是被顶替的假值而不是真读出来的",
			len(lines), lines)
	}

	emptyLines, ok := got["no-lines"].([]any)
	if !ok || len(emptyLines) != 0 {
		t.Errorf("无子行时 lines 期望空 []any, 实际 %T = %#v", got["no-lines"], got["no-lines"])
	}
}

// TestPlainRead_UnnamedO2MStaysAbsent 没点名就维持不出键——这是**刻意**留的边界。
//
// 反面选项是"总是给空数组"。它更整齐，但那是拿一个看着正常的假值（`[]` = 没有关联行）
// 去顶替"没读"，是本仓一直在拆的那类错。而"总是真读"意味着每次 Read() 都为模型上
// 每个 x2many 多发一次查询。所以：要它就点名。
func TestPlainRead_UnnamedO2MStaysAbsent(t *testing.T) {
	o := newReadShapeOrm(t)
	orderModel, _ := o.GetModel("rs_order")
	lineModel, _ := o.GetModel("rs_line")

	oids, _ := orderModel.Records().Create(map[string]any{"name": "unnamed"})
	lineModel.Records().Create(map[string]any{"name": "L1", "order_id": oids[0]})

	ds, err := orderModel.Read(&orm.ReadRequest{
		Ids:    []any{oids[0]},
		Fields: []string{"id", "name"},
	})
	if err != nil {
		t.Fatalf("plain read: %v", err)
	}
	if _, ok := ds.Record().AsMap()["lines"]; ok {
		t.Errorf("没点名的 lines 不该出现在 plain 读结果里（每个 x2many 补一次子查询 = 每次读放大成 N+1）")
	}
}

// ---------------------------------------------------------------- P4

// TestReadShape_GetByFieldMatchesAsMap BigNumberToString 必须对**所有**取值路径生效。
//
// 从前它是挂在数据集上的字段格式化器，而只有 TRecordSet.AsMap() 会去查那张表。于是
// 同一列同一行有两个值：AsMap 给字符串、GetByField 给 int64；空外键 AsMap 给 ""、
// GetByField 给 0。开关"看起来打开了"，走 GetByField 的调用方一个也没享受到——雪花
// id 照样以 int64 出去，过一次 JSON 就 >2^53 改位。
func TestReadShape_GetByFieldMatchesAsMap(t *testing.T) {
	o := newReadShapeOrm(t)
	partnerModel, _ := o.GetModel("rs_partner")
	orderModel, _ := o.GetModel("rs_order")

	pids, _ := partnerModel.Records().Create(map[string]any{"name": "P"})
	withFK, _ := orderModel.Records().Create(map[string]any{"name": "a", "sequence": int64(0), "partner_id": pids[0]})
	noFK, _ := orderModel.Records().Create(map[string]any{"name": "b", "sequence": int64(7)})

	ds, err := orderModel.Read(&orm.ReadRequest{
		Ids:    []any{withFK[0], noFK[0]},
		Fields: []string{"id", "name", "sequence", "partner_id"},
	})
	if err != nil {
		t.Fatalf("plain read: %v", err)
	}

	ds.Range(func(pos int, rec *dataset.TRecordSet) error {
		m := rec.AsMap()
		for _, name := range []string{"id", "name", "sequence", "partner_id"} {
			if got, want := rec.GetByField(name), m[name]; got != want {
				t.Errorf("行 %d 的 %s: GetByField 给 %T=%#v, AsMap 给 %T=%#v —— 两条取值路径必须一致",
					pos, name, got, got, want, want)
			}
			if _, isStr := m[name].(string); !isStr {
				t.Errorf("行 %d 的 %s: BigNumberToString 打开时期望字符串形态, 实际 %T=%#v", pos, name, m[name], m[name])
			}
		}
		return nil
	})

	// 普通 int64 数据列的 0 是合法值，必须留成 "0"；只有外键的 0 才归空串。
	ds.First()
	first := ds.Record()
	if first.GetByField("sequence") != "0" {
		t.Errorf("sequence=0 期望 \"0\"（合法值，不是「未设置」）, 实际 %#v", first.GetByField("sequence"))
	}
	ds.Next()
	if v := ds.Record().GetByField("partner_id"); v != "" {
		t.Errorf("空外键期望归成空串, 实际 %#v", v)
	}
}

// TestClassicRead_X2ManyIsIdsUnlessSubSpec x2many 的形状由**子规格**决定，不由
// ClassicRead 决定。
//
// 从前 o2m 看 ctx.Fields、m2m 看 ctx.ClassicRead，于是同一次经典读里 o2m 回 id 列表、
// m2m 回整条对端记录——同一类字段两种形状，调用方两边都得写分支。现在两者同一条规则：
//
//	没给子规格 → 对端 id 列表
//	给了子规格 → 对端记录列表，列范围由子规格限定
//
// 默认给 id 还挡掉了 payload 放大：经典列表读原本会把每行每个 x2many 的对端整条塞进
// 结果集（Odoo 的 read() 对 x2many 一律回 ids，正是这个理由）。
func TestClassicRead_X2ManyIsIdsUnlessSubSpec(t *testing.T) {
	o := newReadShapeOrm(t)
	orderModel, _ := o.GetModel("rs_order")
	lineModel, _ := o.GetModel("rs_line")

	oids, _ := orderModel.Records().Create(map[string]any{"name": "SO"})
	if _, err := lineModel.Records().Create(map[string]any{"name": "L1", "qty": 3, "order_id": oids[0]}); err != nil {
		t.Fatalf("create line: %v", err)
	}

	// 没给子规格：id 列表
	ds, err := orderModel.Read(&orm.ReadRequest{
		Ids:         []any{oids[0]},
		Fields:      []string{"id", "lines"},
		ClassicRead: true,
		Limit:       -1,
	})
	if err != nil {
		t.Fatalf("classic read: %v", err)
	}
	v := ds.Record().GetByField("lines")
	ids, ok := v.([]any)
	if !ok || len(ids) != 1 {
		t.Fatalf("经典读没给子规格时 lines 期望单元素 []any 的 id 列表, 实际 %T = %#v", v, v)
	}

	// 给了子规格：对端记录，且只含点名的列（外加主键与回指父记录的反向 FK）
	ds, err = orderModel.Read(&orm.ReadRequest{
		Ids:         []any{oids[0]},
		Fields:      []string{"id", "lines"},
		ClassicRead: true,
		Limit:       -1,
		SubFields: map[string]*orm.ReadRequest{
			"lines": {Fields: []string{"name"}},
		},
	})
	if err != nil {
		t.Fatalf("classic read with subspec: %v", err)
	}
	v = ds.Record().GetByField("lines")
	recs, ok := v.([]map[string]any)
	if !ok || len(recs) != 1 {
		t.Fatalf("给了子规格时 lines 期望单元素 []map 的对端记录, 实际 %T = %#v", v, v)
	}
	if recs[0]["name"] != "L1" {
		t.Errorf("子记录 name 期望 L1, 实际 %#v", recs[0]["name"])
	}
	if _, has := recs[0]["qty"]; has {
		t.Errorf("子规格只点了 name，却回了没点名的列: %#v", recs[0])
	}
}
