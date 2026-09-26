package orm

import (
	"sort"
	"strings"
	"testing"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/utils"
)

/*
many2many 读值按字段 domain 过滤对端记录——**不论是不是经典读**。

上游 Odoo 19 Many2many.read：`domain = self.get_comodel_domain(records)`，
`comodel._search(domain)` 再 JOIN 关系表，读值只有 domain 之内的那部分。本仓此前只有
经典读（ClassicRead）那一支套了 domain，非经典读直接查关系表：同一个字段、同一条记录，
表单（经典读）看到 2 个标签，列表 / 导出 / 直接调 ManyToMany 的代码（非经典读）看到 3 个。
而写命令 5/6（74b69a3 起）只动 domain 之内的——读出来的「现值」与写入认的「现值」不是
同一组。

引用记录字段的 domain（不带引号的 grp_id）在 Odoo 里只能写成字符串，而
get_comodel_domain 对字符串回 Domain.TRUE：服务端读写都不套，只给前端挑候选。
*/

// idsToNames 把一组 tag id 换成排序后的名字串，便于比对。
func (f *x2mWFixture) tagNames(t *testing.T, v any) string {
	t.Helper()
	byId := make(map[string]string, len(f.tag))
	for name, id := range f.tag {
		byId[utils.ToString(id)] = name
	}
	var ids []any
	switch vv := v.(type) {
	case []any:
		ids = vv
	case nil:
	default:
		t.Fatalf("m2m 读值的形态没见过：%T = %v", v, v)
	}
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		n, ok := byId[utils.ToString(id)]
		if !ok {
			t.Fatalf("读回来的 %v 不是 fixture 里的 tag", id)
		}
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

func (f *x2mWFixture) readBox(t *testing.T, classic bool, fields ...string) map[string]any {
	t.Helper()
	sess := f.o.NewSession(classic).Model("x2m.w.box")
	ds, err := sess.Select(append([]string{"id"}, fields...)...).Ids(f.boxId).Limit(-1).Read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if ds == nil || ds.Count() != 1 {
		t.Fatalf("读不到 box")
	}
	return ds.Record().AsMap()
}

// 非经典读：带常量 domain 的 m2m 只回 domain 之内的对端。修复前回 3 个。
func TestM2MRead_PlainReadAppliesFieldDomain(t *testing.T) {
	eachX2mBackend(t, func(t *testing.T, f *x2mWFixture) {
		m := f.readBox(t, false, "in_tag_ids", "all_tag_ids")
		expect(t, "in_tag_ids（非经典）", f.tagNames(t, m["in_tag_ids"]), "t_in1,t_in2")
		expect(t, "all_tag_ids（非经典）", f.tagNames(t, m["all_tag_ids"]), "t_in1,t_in2,t_out")
	})
}

// 经典读一直是对的，钉住它，确保两支给出同一组。
func TestM2MRead_ClassicReadAppliesFieldDomain(t *testing.T) {
	eachX2mBackend(t, func(t *testing.T, f *x2mWFixture) {
		m := f.readBox(t, true, "in_tag_ids", "all_tag_ids")
		expect(t, "in_tag_ids（经典）", f.tagNames(t, m["in_tag_ids"]), "t_in1,t_in2")
		expect(t, "all_tag_ids（经典）", f.tagNames(t, m["all_tag_ids"]), "t_in1,t_in2,t_out")
	})
}

// vectors / 模块里有十几处直接调 IModel.ManyToMany（非经典），按 srcKey/relKey 读关系表行。
// 行的形状不变，只是 domain 外的那几行不在了。
func TestM2MRead_DirectManyToManyAppliesFieldDomain(t *testing.T) {
	eachX2mBackend(t, func(t *testing.T, f *x2mWFixture) {
		m, err := f.o.GetModel("x2m.w.box")
		if err != nil {
			t.Fatal(err)
		}
		rows := func(field string) string {
			ds, err := m.ManyToMany(&TFieldContext{Ids: []any{f.boxId}, Field: m.GetFieldByName(field)})
			if err != nil {
				t.Fatalf("ManyToMany(%s): %v", field, err)
			}
			var ids []any
			if ds != nil {
				ds.Range(func(_ int, rec *dataset.TRecordSet) error {
					if utils.ToInt64(rec.GetByField("box_id")) != f.boxId {
						t.Fatalf("关系表行的 box_id 不对：%v", rec.AsMap())
					}
					ids = append(ids, rec.GetByField("tag_id"))
					return nil
				})
			}
			return f.tagNames(t, ids)
		}
		expect(t, "ManyToMany(in_tag_ids)", rows("in_tag_ids"), "t_in1,t_in2")
		expect(t, "ManyToMany(all_tag_ids)", rows("all_tag_ids"), "t_in1,t_in2,t_out")
	})
}

// 引用记录字段的 domain 不在服务端套：两种读都回全部关联。修复前经典读把父记录的
// grp_id 代进去，只回 t_in1。
func TestM2MRead_VariableDomainIsClientSideOnly(t *testing.T) {
	eachX2mBackend(t, func(t *testing.T, f *x2mWFixture) {
		for _, classic := range []bool{false, true} {
			m := f.readBox(t, classic, "grp_tag_ids")
			expect(t, "grp_tag_ids", f.tagNames(t, m["grp_tag_ids"]), "t_in1,t_out")
		}
	})
}
