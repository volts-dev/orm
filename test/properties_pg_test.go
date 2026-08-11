package test

import (
	"database/sql"
	"fmt"
	"testing"

	_ "github.com/lib/pq"
	"github.com/volts-dev/orm"
	"github.com/volts-dev/orm/domain"
)

// properties 一对字段的**真库**整链验证：建表 → 写定义 → 填值 → 读回合并 → jsonb 筛选。
//
// 为什么必须连真 Postgres：这条链上有三件事纯单测碰不到，而它们**全部**是"代码看着对、
// 到库里才炸/才不生效"的那类：
//   - `JSONB` 列的 DDL（拼错或带上长度后缀是语法错，只在建表那一刻暴露）；
//   - `USING GIN` 索引（btree 在 jsonb 上不仅用不上 `@>`，文档稍大还会撞 2704 字节
//     的索引行上限，把 INSERT 直接打挂）；
//   - `@>` / `->>` 这些算子的真实语义（NULL 列上 NOT (col @> ...) 求值是 NULL 而不是
//     TRUE —— "规格不等于 X" 会把从没填过规格的记录整批漏掉）。
//
// 库不可达时 Skip，且**说清跳过了什么**：一条静默 skip 的集成测试和没有测试是一回事。

type propContainerModel struct {
	orm.TModel `table:"name('prop_container_model')"`
	Id         int64  `field:"pk autoincr"`
	Name       string `field:"varchar()"`
	// 容器上的定义（哪些规格项）
	PropsDefinition string `field:"properties_definition() title('Specs')"`
}

type propChildModel struct {
	orm.TModel `table:"name('prop_child_model')"`
	Id         int64  `field:"pk autoincr"`
	Name       string `field:"varchar()"`
	CategId    int64  `field:"many2one(prop_container_model)"`
	// 明细上的值。index 在 jsonb 列上落的是 GIN。
	Props string `field:"properties('categ_id.props_definition') title('Specs') index"`
}

func newPropOrm(t *testing.T) *orm.TOrm {
	t.Helper()

	ds := defaultPostgresSource()
	probe, err := sql.Open("postgres",
		fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
			ds.Host, ds.Port, ds.UserName, ds.Password, ds.DbName))
	if err == nil {
		err = probe.Ping()
		probe.Close()
	}
	if err != nil {
		t.Skipf("跳过：Postgres(%s@%s:%s) 不可达 —— **jsonb 列、GIN 索引与 @> 筛选这三件事本次没有被验证**："+
			"它们只在真库上才成立。在有库的环境里重跑这条。原因: %v", ds.DbName, ds.Host, ds.Port, err)
	}

	o, err := orm.New(orm.WithDataSource(ds))
	if err != nil {
		t.Fatalf("连接数据库: %v", err)
	}
	t.Cleanup(func() { o.Close() })

	if _, err := o.SyncModel("public", new(propContainerModel), new(propChildModel)); err != nil {
		t.Fatalf("SyncModel（jsonb 列 / GIN 索引的 DDL 就发在这一步）: %v", err)
	}
	return o
}

func TestPropertiesPg_ColumnIsJsonbWithGin(t *testing.T) {
	o := newPropOrm(t)

	ds, err := o.Query(`SELECT data_type AS t FROM information_schema.columns
		WHERE table_name = 'prop_child_model' AND column_name = 'props'`)
	if err != nil {
		t.Fatalf("查列类型: %v", err)
	}
	dataType := ds.FieldByName("t").AsString()
	if dataType != "jsonb" {
		t.Errorf("props 列类型 = %q, want jsonb —— 落成 text 的话 @> 和 GIN 都无从谈起", dataType)
	}

	idx, err := o.Query(`SELECT indexdef AS d FROM pg_indexes
		WHERE tablename = 'prop_child_model' AND indexdef ILIKE '%props%'`)
	if err != nil {
		t.Fatalf("查索引（没建出来说明 GinType 没走到建索引那条路）: %v", err)
	}
	if idx.Count() == 0 {
		t.Fatal("props 上没有任何索引 —— GinType 没走到建索引那条路")
	}
	indexdef := idx.FieldByName("d").AsString()
	if !containsFold(indexdef, "using gin") {
		t.Errorf("props 的索引不是 GIN: %s", indexdef)
	}
}

// SyncModel 必须是幂等的：GIN 索引反查不出 GinType 的话，每次启动都会 DROP 再 CREATE
// 一遍整个索引（大表上是分钟级），而且没有任何报错。
func TestPropertiesPg_SyncIsIdempotent(t *testing.T) {
	o := newPropOrm(t)

	first, err := o.Query(`SELECT indexdef AS d FROM pg_indexes
		WHERE tablename = 'prop_child_model' AND indexdef ILIKE '%props%'`)
	if err != nil || first.Count() == 0 {
		t.Fatalf("查索引: %v", err)
	}
	before := first.FieldByName("d").AsString()

	if _, err := o.SyncModel("public", new(propContainerModel), new(propChildModel)); err != nil {
		t.Fatalf("第二次 SyncModel: %v", err)
	}

	second, err := o.Query(`SELECT indexdef AS d FROM pg_indexes
		WHERE tablename = 'prop_child_model' AND indexdef ILIKE '%props%'`)
	if err != nil || second.Count() == 0 {
		t.Fatalf("第二次同步后索引没了: %v", err)
	}
	after := second.FieldByName("d").AsString()
	if before != after {
		t.Errorf("索引被重建了:\n前 %s\n后 %s", before, after)
	}
}

func TestPropertiesPg_RoundTripAndFilter(t *testing.T) {
	o := newPropOrm(t)

	container, err := o.GetModel("prop_container_model")
	if err != nil {
		t.Fatal(err)
	}
	child, err := o.GetModel("prop_child_model")
	if err != nil {
		t.Fatal(err)
	}

	// 规格项名按本次运行生成。写死 "mat"/"pwr" 的话，第二次跑就会把上一次留下的
	// 记录一起查出来 —— 一条只在头一次跑得过的测试比没有测试更糟。
	suffix := uniqueSuffix()
	matName := fmt.Sprintf("mat%d", suffix)
	pwrName := fmt.Sprintf("pwr%d", suffix)
	catIds, err := container.Records().Create(map[string]any{
		"name": fmt.Sprintf("cat-%d", suffix),
		"props_definition": []any{
			map[string]any{"name": matName, "string": "材质", "type": "char", "default": "棉"},
			map[string]any{"name": pwrName, "string": "功率", "type": "integer"},
		},
	})
	if err != nil {
		t.Fatalf("建容器（定义写不进 jsonb 列就在这一步炸）: %v", err)
	}
	catId := catIds[0]

	// 建两条明细：一条填「丝」+功率 0，一条只填默认值。
	silkIds, err := child.Records().Create(map[string]any{
		"name":     fmt.Sprintf("silk-%d", suffix),
		"categ_id": catId,
		"props": []any{
			map[string]any{"name": matName, "type": "char", "value": "丝"},
			map[string]any{"name": pwrName, "type": "integer", "value": 0},
		},
	})
	if err != nil {
		t.Fatalf("建明细: %v", err)
	}
	if _, err = child.Records().Create(map[string]any{
		"name":     fmt.Sprintf("plain-%d", suffix),
		"categ_id": catId,
	}); err != nil {
		t.Fatalf("建第二条明细: %v", err)
	}

	// ── 读：库里存的是瘦字典，读出来必须是"定义 ∪ 值"的完整列表 ──
	ds, err := child.Records().Ids(silkIds[0]).Read()
	if err != nil {
		t.Fatalf("读明细: %v", err)
	}
	if ds.Count() != 1 {
		t.Fatalf("want 1 record, got %d", ds.Count())
	}
	items, ok := ds.FieldByName("props").AsInterface().([]map[string]any)
	if !ok {
		t.Fatalf("props 读出来不是列表(%T)——前端拿到瘦字典就渲染不出任何东西", ds.FieldByName("props").AsInterface())
	}
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d: %v", len(items), items)
	}
	byName := map[string]map[string]any{}
	for _, it := range items {
		byName[fmt.Sprint(it["name"])] = it
	}
	if got := byName[matName]["string"]; got != "材质" {
		t.Errorf("定义没合并进来（string=%v）", got)
	}
	if got := fmt.Sprint(byName[matName]["value"]); got != "丝" {
		t.Errorf("值没读回来: %v", byName[matName])
	}
	if _, has := byName[pwrName]["value"]; !has {
		t.Errorf("数值 0 被当成空丢了: %v", byName[pwrName])
	}

	// ── 筛选：等值必须走 @>，且真库上要能查出行 ──
	hit, err := child.Records().Domain(domain.New("props."+matName, "=", "丝")).Read()
	if err != nil {
		t.Fatalf("按规格筛选（@> 语法错就在这一步）: %v", err)
	}
	if hit.Count() != 1 {
		t.Fatalf("('props.%s','=','丝') 命中 %d 条, want 1", matName, hit.Count())
	}

	// 数值 0 是合法值，不能被当成"没填"。
	hit, err = child.Records().Domain(domain.New("props."+pwrName, "=", 0)).Read()
	if err != nil {
		t.Fatalf("按数值规格筛选: %v", err)
	}
	if hit.Count() != 1 {
		t.Errorf("('props.%s','=',0) 命中 %d 条, want 1 —— 0 被当成空值了", pwrName, hit.Count())
	}

	// != 必须把「从没填过规格」的那条也算进去：NULL 列上 NOT (col @> ...) 求值是 NULL。
	miss, err := child.Records().Domain(domain.New("props."+matName, "!=", "丝")).Read()
	if err != nil {
		t.Fatalf("反向筛选: %v", err)
	}
	if miss.Count() < 1 {
		t.Errorf("('props.%s','!=','丝') 一条都没命中 —— 没填过规格的记录被 NULL 语义整批漏掉了", matName)
	}

	// ── 改定义：明细上带 definition_changed 的写入要落到**容器**上 ──
	if _, err = child.Records().Ids(silkIds[0]).Write(map[string]any{
		"props": []any{
			map[string]any{"name": matName, "string": "面料", "type": "char", "value": "丝", "definition_changed": true},
			map[string]any{"name": pwrName, "string": "功率", "type": "integer", "value": 0},
		},
	}); err != nil {
		t.Fatalf("改定义: %v", err)
	}
	catDs, err := container.Records().Select("id", "props_definition").Ids(catId).Read()
	if err != nil {
		t.Fatal(err)
	}
	defItems, _ := catDs.FieldByName("props_definition").AsInterface().([]any)
	var renamed bool
	for _, it := range defItems {
		m, _ := it.(map[string]any)
		if m["name"] == matName && m["string"] == "面料" {
			renamed = true
		}
		if _, hasValue := m["value"]; hasValue {
			t.Errorf("定义里混进了 value —— 每条明细各存一份定义，正是这套设计要避免的: %v", m)
		}
	}
	if !renamed {
		t.Errorf("改名没写回容器: %v", defItems)
	}
}

// 新建记录时要按容器的定义把默认值算出来存下（Odoo 的 Properties 是 compute 字段，
// `_depends=(容器,)` + `precompute=True`）。
//
// 少了这一步的表现极难归因：同一个类别下，"改过一次规格"的产品有默认值、
// 刚建出来的产品是空的，两条记录看起来完全一样却显示不同，且没有任何报错。
func TestPropertiesPg_DefaultsFilledOnCreate(t *testing.T) {
	o := newPropOrm(t)

	container, err := o.GetModel("prop_container_model")
	if err != nil {
		t.Fatal(err)
	}
	child, err := o.GetModel("prop_child_model")
	if err != nil {
		t.Fatal(err)
	}

	suffix := uniqueSuffix()
	matName := fmt.Sprintf("mat%d", suffix)
	pwrName := fmt.Sprintf("pwr%d", suffix)
	catIds, err := container.Records().Create(map[string]any{
		"name": fmt.Sprintf("defcat-%d", suffix),
		"props_definition": []any{
			map[string]any{"name": matName, "string": "材质", "type": "char", "default": "棉"},
			map[string]any{"name": pwrName, "string": "功率", "type": "integer"},
		},
	})
	if err != nil {
		t.Fatalf("建容器: %v", err)
	}

	// 只给类别，一个规格值都不填 —— 走的正是"新建产品"那条路。
	ids, err := child.Records().Create(map[string]any{
		"name":     fmt.Sprintf("plain-%d", suffix),
		"categ_id": catIds[0],
	})
	if err != nil {
		t.Fatalf("建明细: %v", err)
	}

	ds, err := child.Records().Ids(ids[0]).Read()
	if err != nil {
		t.Fatalf("读明细: %v", err)
	}
	items, _ := ds.FieldByName("props").AsInterface().([]map[string]any)
	byName := map[string]map[string]any{}
	for _, it := range items {
		byName[fmt.Sprint(it["name"])] = it
	}
	if got := fmt.Sprint(byName[matName]["value"]); got != "棉" {
		t.Errorf("新建记录没落上默认值: %v", byName[matName])
	}
	if _, has := byName[pwrName]["value"]; has {
		t.Errorf("没有默认值的项不该凭空多出一个 value: %v", byName[pwrName])
	}

	// 存下来了才算数：只在读出口补默认值的话，按默认值筛选查不到这条记录。
	hit, err := child.Records().Domain(domain.New("props."+matName, "=", "棉")).Read()
	if err != nil {
		t.Fatalf("按默认值筛选: %v", err)
	}
	if hit.Count() != 1 {
		t.Errorf("按默认值筛选一条都没命中 —— 默认值只是读出来好看，并没有真的落库")
	}
}

// 容器没有定义时不该凭空写点什么进去（那会让"这条记录填过规格"与"没填过"无从区分）。
func TestPropertiesPg_NoDefinitionKeepsColumnNull(t *testing.T) {
	o := newPropOrm(t)

	container, err := o.GetModel("prop_container_model")
	if err != nil {
		t.Fatal(err)
	}
	child, err := o.GetModel("prop_child_model")
	if err != nil {
		t.Fatal(err)
	}

	suffix := uniqueSuffix()
	catIds, err := container.Records().Create(map[string]any{"name": fmt.Sprintf("emptycat-%d", suffix)})
	if err != nil {
		t.Fatal(err)
	}
	ids, err := child.Records().Create(map[string]any{
		"name":     fmt.Sprintf("nodef-%d", suffix),
		"categ_id": catIds[0],
	})
	if err != nil {
		t.Fatal(err)
	}

	ds, err := o.Query(`SELECT coalesce(props::text, '<null>') AS v FROM prop_child_model WHERE id = ?`, ids[0])
	if err != nil {
		t.Fatalf("查列: %v", err)
	}
	if got := ds.FieldByName("v").AsString(); got != "<null>" {
		t.Errorf("容器没有定义，列却被写了 %q", got)
	}
}

func containsFold(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		ls, lsub := []rune(s), []rune(sub)
		lower := func(r rune) rune {
			if r >= 'A' && r <= 'Z' {
				return r + 32
			}
			return r
		}
		for i := 0; i+len(lsub) <= len(ls); i++ {
			ok := true
			for j := range lsub {
				if lower(ls[i+j]) != lower(lsub[j]) {
					ok = false
					break
				}
			}
			if ok {
				return true
			}
		}
		return false
	})()
}
