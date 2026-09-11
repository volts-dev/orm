package orm

import (
	"testing"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/utils"

	_ "modernc.org/sqlite"
)

// 非存储、带 getter 的 many2one（算出来的外键）必须真的被算出来。
//
// TMany2OneField.OnRead 此前没有 getter 分支（o2m 一直有、m2m 2026-08-26 补上），
// 于是这种字段在任何读法下都是空的：plain 读里没有值，经典读里是 false。真实案例是
// vectors 侧 stock 的库位「所属仓库」、包裹的「库位 / 货主」、补货规则的「产品分类」
// ——声明写得好好的，界面上恒空，没有一行日志。

type M2OGetterCatModel struct {
	TModel `table:"name('m2o_getter_cat')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar() size(64)"`
}

type M2OGetterItemModel struct {
	TModel     `table:"name('m2o_getter_item')"`
	Id         int64  `field:"pk autoincr"`
	Name       string `field:"varchar() size(64)"`
	CategoryId int64  `field:"many2one(m2o_getter_cat)"`
}

func (self *M2OGetterItemModel) OnBuildFields() error {
	if err := self.TModel.OnBuildFields(); err != nil {
		return err
	}
	// 「镜像分类」：值就是 category_id，但不落库、由 getter 算。
	self.Builder().ManyToOneField("mirror_category_id", "m2o_getter_cat").Store(false).
		Getter(m2oGetterMirror)
	return nil
}

// m2oGetterMirror 从库里回查 category_id（不假设它在这次读的字段里——plain 读只点名
// mirror_category_id 时数据集里就没有 category_id）。
func m2oGetterMirror(ctx *TFieldContext) error {
	ids := ctx.Dataset.Keys("id")
	byId := map[string]any{}
	if len(ids) > 0 {
		ds, err := ctx.Model.Orm().Model("m2o.getter.item").Ids(ids...).Select("id", "category_id").Read()
		if err != nil {
			return err
		}
		ds.Range(func(_ int, rec *dataset.TRecordSet) error {
			byId[utils.ToString(rec.GetByField("id"))] = rec.GetByField("category_id")
			return nil
		})
	}
	fname := ctx.Field.Name()
	return ctx.Dataset.Range(func(_ int, rec *dataset.TRecordSet) error {
		rec.SetByField(fname, byId[utils.ToString(rec.GetByField("id"))])
		return nil
	})
}

func TestNonStoredMany2oneGetterIsComputed(t *testing.T) {
	ds := &TDataSource{DbType: "sqlite", DbName: ":memory:"}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(M2OGetterCatModel), new(M2OGetterItemModel)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	catIds, err := o.Model("m2o.getter.cat").Create(map[string]any{"name": "Office"})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	itemIds, err := o.Model("m2o.getter.item").Create(map[string]any{"name": "Chair", "category_id": catIds[0]})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}
	wantId := utils.ToString(catIds[0])

	// plain 读：裸 id。
	res, err := o.Model("m2o.getter.item").Ids(itemIds[0]).Select("id", "mirror_category_id").Read()
	if err != nil {
		t.Fatalf("plain read: %v", err)
	}
	res.First()
	if got := utils.ToString(res.Record().GetByField("mirror_category_id")); got != wantId {
		t.Fatalf("plain read mirror_category_id = %q, want %q —— getter 没被调", got, wantId)
	}

	// 经典读：对端记录（带名字）。
	res, err = o.Model("m2o.getter.item").Ids(itemIds[0]).Select("id", "mirror_category_id").Classic().Read()
	if err != nil {
		t.Fatalf("classic read: %v", err)
	}
	res.First()
	m, ok := res.Record().GetByField("mirror_category_id").(map[string]any)
	if !ok {
		t.Fatalf("classic read mirror_category_id = %#v, want the category record", res.Record().GetByField("mirror_category_id"))
	}
	if utils.ToString(m["id"]) != wantId || m["name"] != "Office" {
		t.Fatalf("classic read mirror_category_id = %v, want {id:%s name:Office}", m, wantId)
	}
}
