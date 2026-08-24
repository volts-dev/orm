package orm

import (
	stdErrors "errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ormerr "github.com/volts-dev/orm/errors"
)

/*
写入语义的回归。三个症状的共同形态都是「请求发出去、返回成功、库里没变」——
调用方拿不到任何信号：

  - 未知键静默丢弃。_separateValues 按模型字段遍历，没被认领的键根本不会被访问到。
    真栈：开户向导把 contact_address 拼成 contact_ddress，用户填的地址从此消失。
  - 显式 nil 被当成"没提供"。setted 的判据是"值不是 nil"，把「键不在」与
    「键在、值是 nil」压成同一种，于是 Write(map{"expire": nil}) 影响 0 行。
    更隐蔽的是 dataset.AppendRecord 会把全 nil 的记录**整条丢掉**，所以键在不在
    这件事根本不能回头问数据集。
  - 时间列的零时刻被原样写成 0001-01-01：既不是 NULL 也不报错，看着像清空了，
    `expire < now()` 之类的查询却照样命中——比"改不动"更难查。
  - struct 源的零值无法表达"清空"。
*/

type wsRec struct {
	TModel `table:"name('ws_rec')"`
	Id     int64     `field:"pk autoincr"`
	Name   string    `field:"varchar(64)"`
	Note   string    `field:"varchar(64)"`
	Qty    int       `field:"int"`
	Active bool      `field:"bool default(true)"`
	Expire time.Time `field:"datetime"`
}

func newWsOrm(t *testing.T) *TOrm {
	t.Helper()
	ds := &TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "ws.db")}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(wsRec)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	return o
}

func wsSeed(t *testing.T, o *TOrm) int64 {
	t.Helper()
	ids, err := o.Model("ws.rec").Create(map[string]any{
		"name": "n0", "note": "keep", "qty": 7, "active": true,
		"expire": time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	switch v := ids[0].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	}
	t.Fatalf("unexpected id type %T", ids[0])
	return 0
}

// isNull 直接问库，不经 ORM 的读取转换——NULL 与零时刻读出来都是 time.Time{}，
// 走 ORM 分辨不出来，而这两者的区别正是本组用例要钉的东西。
func wsIsNull(t *testing.T, o *TOrm, id int64, col string) bool {
	t.Helper()
	ds, err := o.Query("SELECT "+col+" IS NULL AS null_flag FROM ws_rec WHERE id=?", id)
	if err != nil {
		t.Fatal(err)
	}
	v := ds.Record().FieldByName("null_flag").AsInterface()
	switch x := v.(type) {
	case bool:
		return x
	case int64:
		return x == 1
	}
	t.Fatalf("unexpected isnull type %T (%v)", v, v)
	return false
}

// ---------- 未知键 ----------

func TestWrite_UnknownKey_IsRejected(t *testing.T) {
	o := newWsOrm(t)
	id := wsSeed(t, o)

	_, err := o.Model("ws.rec").Ids(id).Write(map[string]any{"nnote": "typo", "qty": 42})
	if !stdErrors.Is(err, ormerr.ErrValidation) {
		t.Fatalf("模型上不存在的键必须报 ErrValidation，得 %v", err)
	}
	if !strings.Contains(err.Error(), "nnote") {
		t.Fatalf("错误信息必须点名是哪个键，得 %v", err)
	}

	// 拼错的键既然报了错，同一次写入的合法键也不该落库——不能改一半。
	ds, _ := o.Model("ws.rec").Ids(id).Read()
	if got := ds.Record().FieldByName("qty").AsInteger(); got != 7 {
		t.Fatalf("整次写入应当被拒，qty 不该变，得 %d", got)
	}
}

func TestCreate_UnknownKey_IsRejected(t *testing.T) {
	o := newWsOrm(t)
	_, err := o.Model("ws.rec").Create(map[string]any{"name": "x", "contact_ddress": "北京"})
	if !stdErrors.Is(err, ormerr.ErrValidation) {
		t.Fatalf("新建时的未知键同样必须报错，得 %v", err)
	}
}

func TestWrite_UnknownKey_AllowedAtBoundary(t *testing.T) {
	o := newWsOrm(t)
	id := wsSeed(t, o)

	n, err := o.Model("ws.rec").Ids(id).AllowUnknownFields().Write(map[string]any{"nnote": "typo", "qty": 42})
	if err != nil {
		t.Fatalf("AllowUnknownFields() 之后应当照旧容忍，得 %v", err)
	}
	if n != 1 {
		t.Fatalf("期望影响 1 行，得 %d", n)
	}
	ds, _ := o.Model("ws.rec").Ids(id).Read()
	if got := ds.Record().FieldByName("qty").AsInteger(); got != 42 {
		t.Fatalf("合法键仍应落库，qty 得 %d", got)
	}
}

// Omit() 过的键是调用方明说"不要写"，不再计较它是不是模型字段。
func TestWrite_UnknownKey_OmitIsTolerated(t *testing.T) {
	o := newWsOrm(t)
	id := wsSeed(t, o)

	if _, err := o.Model("ws.rec").Ids(id).Omit("nnote").Write(map[string]any{"nnote": "x", "qty": 42}); err != nil {
		t.Fatalf("Omit 过的键不该报未知：%v", err)
	}
}

// struct 源经 StructToMap 产出，只含模型字段，不参与未知键检查。
func TestWrite_UnknownKey_StructSourceUnaffected(t *testing.T) {
	o := newWsOrm(t)
	id := wsSeed(t, o)

	if _, err := o.Model("ws.rec").Ids(id).Write(&wsRec{Id: id, Qty: 3}); err != nil {
		t.Fatalf("struct 源不该触发未知键检查：%v", err)
	}
}

// ---------- 显式空值 ----------

// 单键清空：这一条踩的是 dataset.AppendRecord 丢弃全 nil 记录的坑，
// 所以显式键集合必须在 ORM 入口就截下来，不能回头问数据集。
func TestWrite_ExplicitNil_ClearsColumn(t *testing.T) {
	o := newWsOrm(t)
	id := wsSeed(t, o)

	n, err := o.Model("ws.rec").Ids(id).Write(map[string]any{"expire": nil})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 1 {
		t.Fatalf("期望影响 1 行，得 %d —— 单键 nil 的记录被 dataset 整条丢掉了", n)
	}
	if !wsIsNull(t, o, id, "expire") {
		t.Fatal("expire 应当被清成 NULL")
	}
}

func TestWrite_ExplicitNil_MixedKeys(t *testing.T) {
	o := newWsOrm(t)
	id := wsSeed(t, o)

	if _, err := o.Model("ws.rec").Ids(id).Write(map[string]any{"note": "x", "expire": nil}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !wsIsNull(t, o, id, "expire") {
		t.Fatal("expire 应当被清成 NULL")
	}
	ds, _ := o.Model("ws.rec").Ids(id).Read()
	if got := ds.Record().FieldByName("note").AsString(); got != "x" {
		t.Fatalf("同一次写入里的普通值应照常落库，note 得 %q", got)
	}
}

// 零时刻不是一个合法日期，是"清空"的另一种写法。原先它被原样写成 0001-01-01：
// 不报错、看着像清空了，`expire < now()` 却照样命中。
func TestWrite_ZeroTime_ClearsColumn(t *testing.T) {
	o := newWsOrm(t)
	id := wsSeed(t, o)

	if _, err := o.Model("ws.rec").Ids(id).Write(map[string]any{"expire": time.Time{}}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !wsIsNull(t, o, id, "expire") {
		t.Fatal("零时刻应当落成 NULL，而不是 0001-01-01")
	}
}

// 新建时"没值"与 NULL 是同一个意思，该由默认值来填——显式 nil 不该越过默认值。
func TestCreate_ExplicitNil_StillUsesDefault(t *testing.T) {
	o := newWsOrm(t)

	ids, err := o.Model("ws.rec").Create(map[string]any{"name": "x", "active": nil})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	var id int64
	switch v := ids[0].(type) {
	case int64:
		id = v
	case int:
		id = int64(v)
	}
	ds, _ := o.Model("ws.rec").Ids(id).Read()
	if got := ds.Record().FieldByName("active").AsBoolean(); !got {
		t.Fatal("新建时的显式 nil 应当走默认值 true，而不是写 NULL")
	}
}

// map 源里的显式零值照常落库（已有语义，防回归）。
func TestWrite_ExplicitZero_StillLands(t *testing.T) {
	o := newWsOrm(t)
	id := wsSeed(t, o)

	if _, err := o.Model("ws.rec").Ids(id).Write(map[string]any{"note": "", "qty": 0, "active": false}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	ds, _ := o.Model("ws.rec").Ids(id).Read()
	rec := ds.Record()
	if rec.FieldByName("note").AsString() != "" || rec.FieldByName("qty").AsInteger() != 0 || rec.FieldByName("active").AsBoolean() {
		t.Fatalf("显式零值必须落库，得 note=%q qty=%d active=%v",
			rec.FieldByName("note").AsString(), rec.FieldByName("qty").AsInteger(), rec.FieldByName("active").AsBoolean())
	}
}

// ---------- struct 源 + Select() 写入范围 ----------

// struct 的零值与"没赋值"分不开，所以默认必须保持旧语义：零值 = 没提供。
func TestWrite_StructZero_UnchangedByDefault(t *testing.T) {
	o := newWsOrm(t)
	id := wsSeed(t, o)

	if _, err := o.Model("ws.rec").Ids(id).Write(&wsRec{Id: id, Name: "n0"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	ds, _ := o.Model("ws.rec").Ids(id).Read()
	rec := ds.Record()
	if rec.FieldByName("note").AsString() != "keep" || rec.FieldByName("qty").AsInteger() != 7 {
		t.Fatalf("struct 源的零值不该被当成清空，得 note=%q qty=%d",
			rec.FieldByName("note").AsString(), rec.FieldByName("qty").AsInteger())
	}
}

// Select() 点名之后，范围内的零值照落、范围外的一概不碰。
// 这是 struct 源精确表达"清空"的唯一办法。
func TestWrite_SelectScopesUpdate(t *testing.T) {
	o := newWsOrm(t)
	id := wsSeed(t, o)

	if _, err := o.Model("ws.rec").Ids(id).Select("note", "qty").Write(&wsRec{Id: id}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	ds, _ := o.Model("ws.rec").Ids(id).Read()
	rec := ds.Record()
	if rec.FieldByName("note").AsString() != "" {
		t.Fatalf("范围内的 note 应被清空，得 %q", rec.FieldByName("note").AsString())
	}
	if rec.FieldByName("qty").AsInteger() != 0 {
		t.Fatalf("范围内的 qty 应归零，得 %d", rec.FieldByName("qty").AsInteger())
	}
	if !rec.FieldByName("active").AsBoolean() {
		t.Fatal("范围外的 active 不该被碰")
	}
	if rec.FieldByName("name").AsString() != "n0" {
		t.Fatalf("范围外的 name 不该被碰，得 %q", rec.FieldByName("name").AsString())
	}
}

// ---------- 嵌套 x2many：宽严必须整棵树一致 ----------

type wsOrder struct {
	TModel  `table:"name('ws_order')"`
	Id      int64  `field:"pk autoincr"`
	Name    string `field:"varchar(64)"`
	LineIds []any  `field:"one2many(ws_line,order_id)"`
}

type wsLine struct {
	TModel  `table:"name('ws_line')"`
	Id      int64  `field:"pk autoincr"`
	OrderId int64  `field:"many2one(ws_order)"`
	Label   string `field:"varchar(64)"`
}

func newWsTreeOrm(t *testing.T) *TOrm {
	t.Helper()
	ds := &TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "wstree.db")}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(wsOrder), new(wsLine)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	return o
}

// 明细行里的拼写错误同样要被拦下——x2many 命令里的键是最容易写错的地方
// （行上全是 m2o），而它们此前和父层一样静默消失。
func TestCreate_UnknownKey_InNestedCommand(t *testing.T) {
	o := newWsTreeOrm(t)

	model, err := o.GetModel("ws.order")
	if err != nil {
		t.Fatal(err)
	}
	_, err = model.Create(&CreateRequest{Data: []any{map[string]any{
		"name":     "o1",
		"line_ids": []any{[]any{0, 0, map[string]any{"lable": "typo"}}},
	}}})
	if !stdErrors.Is(err, ormerr.ErrValidation) {
		t.Fatalf("明细行上的未知键必须报错，得 %v", err)
	}
	if !strings.Contains(err.Error(), "lable") {
		t.Fatalf("错误信息应点名明细行上的那个键，得 %v", err)
	}
}

// 而边界层一旦放宽，整棵树都要跟着放宽：父层放行、子层报错会让一次表单保存
// 在明细行上莫名其妙地失败。
func TestCreate_UnknownKey_NestedInheritsAllowance(t *testing.T) {
	o := newWsTreeOrm(t)

	model, err := o.GetModel("ws.order")
	if err != nil {
		t.Fatal(err)
	}
	ids, err := model.Create(&CreateRequest{
		AllowUnknownFields: true,
		Data: []any{map[string]any{
			"name":     "o1",
			"line_ids": []any{[]any{0, 0, map[string]any{"label": "L1", "lable": "typo"}}},
		}},
	})
	if err != nil {
		t.Fatalf("AllowUnknownFields 应当整棵树生效，得 %v", err)
	}
	if len(ids) == 0 {
		t.Fatal("没有建出记录")
	}
	ds, err := o.Model("ws.line").Domain(`[('label','=','L1')]`).Read()
	if err != nil {
		t.Fatal(err)
	}
	if ds.Count() != 1 {
		t.Fatalf("明细行应当照常落库，得 %d 条", ds.Count())
	}
}
