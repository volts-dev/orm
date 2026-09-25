package orm

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/utils"
)

/*
x2many 的「5 = 清空」「6 = 整体替换」只作用于字段 domain 之内的那部分。

读取路径早就按字段 domain 过滤（OneToMany / ManyToMany，见 o2m_domain_test.go），
写入路径却按反向键 / 关系表把**全部**子行找出来再删 / 解绑。于是同一张子表按 domain
切成几个字段时，对其中一个发 (5,) 或 (6,0,ids)，会把别的字段的行一起带走——
account.move.invoice_line_ids（domain 只含 product/section/note 行）上发一次 (5,)，
同一张发票的税额行、应收行全部被删，不报错。

上游 Odoo 19（odoo/orm/fields_relational.py）：
  - One2many.write_real 的 CLEAR/SET 查的是
    `get_comodel_domain(model) & (inverse in recs) & (id not in lines)`；
  - Many2many.write_real 的 CLEAR/SET 作用于 old_relation = record[self.name]，
    即读出来的、已按 domain 过滤的那一组。
  - DELETE(2) / UNLINK(3) 点名 id，不看 domain。
*/

type (
	X2mWBox struct {
		TModel `table:"name('x2m_w_box')"`
		Id     int64  `field:"pk autoincr title('ID')"`
		Name   string `field:"varchar() size(64)"`
		InIds  []any  `field:"one2many(x2m_w_item,box_id) domain([('kind','=','in')])"`
		AllIds []any  `field:"one2many(x2m_w_item,box_id)"`
		// 反向键必填的一组：解绑落成删除（invoice_line_ids 的 move_id 就是这样）。
		ReqInIds  []any `field:"one2many(x2m_w_req_item,box_id) domain('[(''kind'', ''in'', [''in'', ''mid''])]')"`
		ReqAllIds []any `field:"one2many(x2m_w_req_item,box_id)"`
		// 同一张关系表，按 kind 切出一个带 domain 的 m2m 字段。
		InTagIds  []any `field:"many2many(x2m_w_tag,x2m_w_box_tag_rel,box_id,tag_id) domain([('kind','=','in')])"`
		AllTagIds []any `field:"many2many(x2m_w_tag,x2m_w_box_tag_rel,box_id,tag_id)"`
	}

	X2mWItem struct {
		TModel `table:"name('x2m_w_item')"`
		Id     int64  `field:"pk autoincr title('ID')"`
		Name   string `field:"varchar() size(64)"`
		Kind   string `field:"varchar() size(16)"`
		BoxId  int64  `field:"many2one(x2m_w_box)"`
	}

	X2mWReqItem struct {
		TModel `table:"name('x2m_w_req_item')"`
		Id     int64  `field:"pk autoincr title('ID')"`
		Name   string `field:"varchar() size(64)"`
		Kind   string `field:"varchar() size(16)"`
		BoxId  int64  `field:"many2one(x2m_w_box) required()"`
	}

	X2mWTag struct {
		TModel `table:"name('x2m_w_tag')"`
		Id     int64  `field:"pk autoincr title('ID')"`
		Name   string `field:"varchar() size(64)"`
		Kind   string `field:"varchar() size(16)"`
	}
)

type x2mWFixture struct {
	o     *TOrm
	boxId int64
	other int64          // 另一只箱子：它的子行任何时候都不许被动到
	item  map[string]any // name -> id（x2m_w_item）
	req   map[string]any // name -> id（x2m_w_req_item）
	tag   map[string]any // name -> id（x2m_w_tag）
}

// eachX2mBackend 在 sqlite 与 postgres（本机 test_orm，连不上就跳过）上各跑一遍：
// 生产跑的是 PG，关系表查询走的是手拼 SQL，两种方言都要过。
func eachX2mBackend(t *testing.T, run func(t *testing.T, f *x2mWFixture)) {
	t.Run("sqlite", func(t *testing.T) {
		run(t, setupX2mWrite(t, &TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "x2mw.db")}))
	})
	t.Run("postgres", func(t *testing.T) {
		run(t, setupX2mWrite(t, &TDataSource{DbType: "postgres", Host: "localhost", Port: "5432",
			UserName: "postgres", Password: "postgres", DbName: "test_orm", SSLMode: "disable"}))
	})
}

func setupX2mWrite(t *testing.T, ds *TDataSource) *x2mWFixture {
	t.Helper()
	o, err := New(WithDataSource(ds))
	if err != nil {
		if ds.DbType == "postgres" {
			t.Skipf("postgres unavailable: %v", err)
		}
		t.Fatalf("orm.New: %v", err)
	}
	// 每个子测试一个 orm，不关就一直占着连接池：PG 是整机共用的，跑满 max_connections
	// 连别的进程一起拒（53300）。Cleanup 后进先出，这句先登记、最后执行（在 drop 之后）。
	t.Cleanup(func() { o.Close() })
	if ds.DbType == "postgres" {
		drop := func() {
			for _, tb := range []string{"x2m_w_box_tag_rel", "x2m_w_item", "x2m_w_req_item", "x2m_w_tag", "x2m_w_box"} {
				o.Exec("DROP TABLE IF EXISTS " + tb + " CASCADE")
			}
		}
		drop()
		t.Cleanup(drop)
	}
	if _, err := o.SyncModel("", new(X2mWItem), new(X2mWReqItem), new(X2mWTag), new(X2mWBox)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	f := &x2mWFixture{o: o, item: map[string]any{}, req: map[string]any{}, tag: map[string]any{}}

	mk := func(model string, vals map[string]any) any {
		ids, err := o.Model(model).Create(vals)
		if err != nil {
			t.Fatalf("create %s %v: %v", model, vals, err)
		}
		return ids[0]
	}
	f.boxId = firstId(t, []any{mk("x2m.w.box", map[string]any{"name": "box"})})
	f.other = firstId(t, []any{mk("x2m.w.box", map[string]any{"name": "other"})})

	for _, it := range []struct{ name, kind string }{{"i1", "in"}, {"i2", "in"}, {"o1", "out"}} {
		f.item[it.name] = mk("x2m.w.item", map[string]any{"name": it.name, "kind": it.kind, "box_id": f.boxId})
	}
	f.item["x1"] = mk("x2m.w.item", map[string]any{"name": "x1", "kind": "in", "box_id": f.other})

	// 发票的形状：product / section 在 domain 内，tax / payment_term 不在。
	for _, it := range []struct{ name, kind string }{{"r_in", "in"}, {"r_mid", "mid"}, {"r_tax", "tax"}, {"r_term", "term"}} {
		f.req[it.name] = mk("x2m.w.req.item", map[string]any{"name": it.name, "kind": it.kind, "box_id": f.boxId})
	}
	f.req["r_other"] = mk("x2m.w.req.item", map[string]any{"name": "r_other", "kind": "in", "box_id": f.other})

	for _, it := range []struct{ name, kind string }{{"t_in1", "in"}, {"t_in2", "in"}, {"t_in3", "in"}, {"t_out", "out"}} {
		f.tag[it.name] = mk("x2m.w.tag", map[string]any{"name": it.name, "kind": it.kind})
	}
	// box 挂 t_in1/t_in2/t_out；other 挂 t_in1。t_in3 先不挂，给 (6) 用。
	for _, pair := range [][2]any{
		{f.boxId, f.tag["t_in1"]}, {f.boxId, f.tag["t_in2"]}, {f.boxId, f.tag["t_out"]},
		{f.other, f.tag["t_in1"]},
	} {
		if _, err := o.Exec("INSERT INTO x2m_w_box_tag_rel (box_id, tag_id) VALUES (?, ?)", pair[0], pair[1]); err != nil {
			t.Fatalf("seed rel: %v", err)
		}
	}
	return f
}

func (f *x2mWFixture) write(t *testing.T, field string, cmds ...[]any) {
	t.Helper()
	val := make([]any, 0, len(cmds))
	for _, c := range cmds {
		val = append(val, c)
	}
	if _, err := f.o.Model("x2m.w.box").Ids(f.boxId).Write(map[string]any{field: val}); err != nil {
		t.Fatalf("write %s %v: %v", field, cmds, err)
	}
}

// children 返回挂在 parent 下的子行名（排序后以逗号连接）。
func (f *x2mWFixture) children(t *testing.T, table string, parent int64) string {
	t.Helper()
	ds, err := f.o.Query("SELECT name, box_id FROM " + table + " ORDER BY name")
	if err != nil {
		t.Fatalf("query %s: %v", table, err)
	}
	var names []string
	ds.Range(func(_ int, rec *dataset.TRecordSet) error {
		if utils.ToInt64(rec.GetByField("box_id")) == parent {
			names = append(names, utils.ToString(rec.GetByField("name")))
		}
		return nil
	})
	sort.Strings(names)
	return strings.Join(names, ",")
}

// allRows 返回表里全部行名，用来确认「删除」没有多删。
func (f *x2mWFixture) allRows(t *testing.T, table string) string {
	t.Helper()
	ds, err := f.o.Query("SELECT name FROM " + table + " ORDER BY name")
	if err != nil {
		t.Fatalf("query %s: %v", table, err)
	}
	var names []string
	ds.Range(func(_ int, rec *dataset.TRecordSet) error {
		names = append(names, utils.ToString(rec.GetByField("name")))
		return nil
	})
	sort.Strings(names)
	return strings.Join(names, ",")
}

// tagsOf 返回关系表里 parent 挂着的 tag 名。
func (f *x2mWFixture) tagsOf(t *testing.T, parent int64) string {
	t.Helper()
	ds, err := f.o.Query("SELECT t.name AS name FROM x2m_w_box_tag_rel r JOIN x2m_w_tag t ON t.id = r.tag_id WHERE r.box_id = ? ORDER BY t.name", parent)
	if err != nil {
		t.Fatalf("query rel: %v", err)
	}
	var names []string
	ds.Range(func(_ int, rec *dataset.TRecordSet) error {
		names = append(names, utils.ToString(rec.GetByField("name")))
		return nil
	})
	sort.Strings(names)
	return strings.Join(names, ",")
}

func expect(t *testing.T, what, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = [%s]，期望 [%s]", what, got, want)
	}
}

// ---- one2many，反向键可空：解绑 = 置空 ----

func TestO2MWrite_ClearOnlyTouchesDomain(t *testing.T) {
	eachX2mBackend(t, func(t *testing.T, f *x2mWFixture) {
		f.write(t, "in_ids", []any{5})
		expect(t, "box 的子行", f.children(t, "x2m_w_item", f.boxId), "o1")
		expect(t, "other 的子行", f.children(t, "x2m_w_item", f.other), "x1")
		expect(t, "全表（置空不删行）", f.allRows(t, "x2m_w_item"), "i1,i2,o1,x1")
	})
}

func TestO2MWrite_SetOnlyTouchesDomain(t *testing.T) {
	eachX2mBackend(t, func(t *testing.T, f *x2mWFixture) {
		f.write(t, "in_ids", []any{6, 0, []any{f.item["i1"]}})
		expect(t, "box 的子行", f.children(t, "x2m_w_item", f.boxId), "i1,o1")
		expect(t, "other 的子行", f.children(t, "x2m_w_item", f.other), "x1")
	})
}

// 不带 domain 的字段行为不变：5 清掉全部，6 只留点名的。
func TestO2MWrite_NoDomainUnchanged(t *testing.T) {
	eachX2mBackend(t, func(t *testing.T, f *x2mWFixture) {
		f.write(t, "all_ids", []any{6, 0, []any{f.item["i1"]}})
		expect(t, "(6) 后 box 的子行", f.children(t, "x2m_w_item", f.boxId), "i1")
		f.write(t, "all_ids", []any{5})
		expect(t, "(5) 后 box 的子行", f.children(t, "x2m_w_item", f.boxId), "")
		expect(t, "other 的子行", f.children(t, "x2m_w_item", f.other), "x1")
	})
}

// 2 / 3 点名 id，与上游一致不看 domain：点到 domain 外的行照样生效。
func TestO2MWrite_DeleteAndUnlinkIgnoreDomain(t *testing.T) {
	eachX2mBackend(t, func(t *testing.T, f *x2mWFixture) {
		f.write(t, "in_ids", []any{3, f.item["o1"]})
		expect(t, "(3,o1) 后 box 的子行", f.children(t, "x2m_w_item", f.boxId), "i1,i2")
		f.write(t, "in_ids", []any{2, f.item["i2"]})
		expect(t, "(2,i2) 后全表", f.allRows(t, "x2m_w_item"), "i1,o1,x1")
	})
}

// ---- one2many，反向键必填：解绑 = 删除（发票行的形状）----

func TestO2MWrite_RequiredInverse_ClearDeletesOnlyDomain(t *testing.T) {
	eachX2mBackend(t, func(t *testing.T, f *x2mWFixture) {
		f.write(t, "req_in_ids", []any{5, 0, 0})
		expect(t, "全表", f.allRows(t, "x2m_w_req_item"), "r_other,r_tax,r_term")
	})
}

func TestO2MWrite_RequiredInverse_SetDeletesOnlyDomain(t *testing.T) {
	eachX2mBackend(t, func(t *testing.T, f *x2mWFixture) {
		f.write(t, "req_in_ids", []any{6, 0, []any{f.req["r_mid"]}})
		expect(t, "全表", f.allRows(t, "x2m_w_req_item"), "r_mid,r_other,r_tax,r_term")
		expect(t, "box 的子行", f.children(t, "x2m_w_req_item", f.boxId), "r_mid,r_tax,r_term")
	})
}

func TestO2MWrite_RequiredInverse_NoDomainUnchanged(t *testing.T) {
	eachX2mBackend(t, func(t *testing.T, f *x2mWFixture) {
		f.write(t, "req_all_ids", []any{5})
		expect(t, "全表", f.allRows(t, "x2m_w_req_item"), "r_other")
	})
}

// ---- many2many ----

func TestM2MWrite_ClearOnlyTouchesDomain(t *testing.T) {
	eachX2mBackend(t, func(t *testing.T, f *x2mWFixture) {
		f.write(t, "in_tag_ids", []any{5, 0, 0})
		expect(t, "box 的 tag", f.tagsOf(t, f.boxId), "t_out")
		expect(t, "other 的 tag", f.tagsOf(t, f.other), "t_in1")
	})
}

func TestM2MWrite_SetOnlyTouchesDomain(t *testing.T) {
	eachX2mBackend(t, func(t *testing.T, f *x2mWFixture) {
		f.write(t, "in_tag_ids", []any{6, 0, []any{f.tag["t_in2"], f.tag["t_in3"]}})
		expect(t, "box 的 tag", f.tagsOf(t, f.boxId), "t_in2,t_in3,t_out")
		expect(t, "other 的 tag", f.tagsOf(t, f.other), "t_in1")
	})
}

// (6,0,[]) 在带 domain 的字段上 = 只清 domain 内的。
func TestM2MWrite_SetEmptyOnlyTouchesDomain(t *testing.T) {
	eachX2mBackend(t, func(t *testing.T, f *x2mWFixture) {
		f.write(t, "in_tag_ids", []any{6, 0, []any{}})
		expect(t, "box 的 tag", f.tagsOf(t, f.boxId), "t_out")
	})
}

func TestM2MWrite_NoDomainUnchanged(t *testing.T) {
	eachX2mBackend(t, func(t *testing.T, f *x2mWFixture) {
		f.write(t, "all_tag_ids", []any{6, 0, []any{f.tag["t_in3"]}})
		expect(t, "(6) 后 box 的 tag", f.tagsOf(t, f.boxId), "t_in3")
		f.write(t, "all_tag_ids", []any{5, 0, 0})
		expect(t, "(5) 后 box 的 tag", f.tagsOf(t, f.boxId), "")
		expect(t, "other 的 tag", f.tagsOf(t, f.other), "t_in1")
	})
}

// 3 点名 id，不看 domain。
func TestM2MWrite_UnlinkIgnoresDomain(t *testing.T) {
	eachX2mBackend(t, func(t *testing.T, f *x2mWFixture) {
		f.write(t, "in_tag_ids", []any{3, f.tag["t_out"]})
		expect(t, "box 的 tag", f.tagsOf(t, f.boxId), "t_in1,t_in2")
	})
}

// 父记录只挂着一条 domain 内的关联：`id in` 只有一个值时的那条路。
func TestM2MWrite_ClearSingleLinked(t *testing.T) {
	eachX2mBackend(t, func(t *testing.T, f *x2mWFixture) {
		if _, err := f.o.Model("x2m.w.box").Ids(f.other).Write(map[string]any{"in_tag_ids": []any{[]any{5, 0, 0}}}); err != nil {
			t.Fatal(err)
		}
		expect(t, "other 的 tag", f.tagsOf(t, f.other), "")
		expect(t, "box 的 tag", f.tagsOf(t, f.boxId), "t_in1,t_in2,t_out")
	})
}

// 单元素的 (5,) 是合法的 Odoo 写法（上游 CLEAR 只看 command[0]），documents 的「替换标签」
// 就这么发：[(5,), (4, id)]。m2m 的命令解析此前只认二/三元组，[5] 让整份列表被当成
// 「裸 id 列表」，把命令元组本身当 id 绑进 SQL。
func TestM2MWrite_SingleElementClear(t *testing.T) {
	eachX2mBackend(t, func(t *testing.T, f *x2mWFixture) {
		f.write(t, "all_tag_ids", []any{5}, []any{4, f.tag["t_in3"]})
		expect(t, "box 的 tag", f.tagsOf(t, f.boxId), "t_in3")
		f.write(t, "in_tag_ids", []any{5}, []any{4, f.tag["t_in1"]})
		expect(t, "box 的 tag（带 domain）", f.tagsOf(t, f.boxId), "t_in1")
	})
}
