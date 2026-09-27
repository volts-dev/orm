package orm

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/orm/domain"
)

/*
声明了 store(false) 的 many2many 不建关联表。

此前 UpdateDb 不看 store，每个计算型 m2m 都建一张关联表，而值由 getter 现算，
那张表永远 0 行（真库上 22 个字段 × 2 个 schema）。去掉表之后，所有"默认去关联表
里找"的路都必须绕开它，否则就从"读一张空表"变成"读一张不存在的表"：

	读   —— ManyToMany 回空（无 getter）；有 getter 照旧走 getter
	写   —— 有 setter 交 setter，没有就忽略
	删   —— 删除时的关联清理不能去 DELETE 那张表（PG 上会打挂整个事务）
	搜   —— 只能走 Searcher；没有就报错点名，不再查空表得出"看着正常"的错结果
*/

type NsTag struct {
	TModel `table:"name('ns_tag')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(32) recname()"`
}

// TagIds 是普通 m2m（有表）；PlainIds 只在 tag 上声明 store(false)、没有 getter；
// calc_ids 由 builder 声明 Store(false) + Getter + Searcher。
type NsMain struct {
	TModel   `table:"name('ns_main')"`
	Id       int64  `field:"pk autoincr"`
	Name     string `field:"varchar(32) recname()"`
	TagIds   []any  `field:"many2many(ns_tag,ns_main_tag_rel,main_id,tag_id)"`
	PlainIds []any  `field:"many2many(ns_tag,ns_main_plain_rel,main_id,tag_id) store(false)"`
}

// nsCalcTagId 是 calc_ids 的 getter 要回的那个 tag。
var nsCalcTagId any

func (self NsMain) OnBuildFields() error {
	if err := self.TModel.OnBuildFields(); err != nil {
		return err
	}
	self.Builder().ManyToManyField("calc_ids", "ns.tag", "ns_main_calc_rel", "main_id", "tag_id").
		Store(false).
		Getter(func(ctx *TFieldContext) error {
			return ctx.Dataset.Range(func(_ int, rec *dataset.TRecordSet) error {
				rec.SetByField("calc_ids", []any{nsCalcTagId})
				return nil
			})
		}).
		Searcher(func(ctx *TFieldSearchContext) (*domain.TDomainNode, error) {
			return domain.New("name", "=", "M1"), nil
		})
	return nil
}

func setupNonStoredM2M(t *testing.T) (*TOrm, int64) {
	t.Helper()
	ds := &TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "ns.db")}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(NsTag), new(NsMain)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	tag, err := o.Model("ns.tag").Create(map[string]any{"name": "T1"})
	if err != nil {
		t.Fatalf("create tag: %v", err)
	}
	nsCalcTagId = tag[0]
	ids, err := o.Model("ns.main").Create(map[string]any{
		"name": "M1", "tag_ids": []any{[]any{6, 0, []any{tag[0]}}}})
	if err != nil {
		t.Fatalf("create main: %v", err)
	}
	if _, err := o.Model("ns.main").Create(map[string]any{"name": "M2"}); err != nil {
		t.Fatalf("create main 2: %v", err)
	}
	return o, firstId(t, ids)
}

func TestNonStoredM2M_NoRelationTable(t *testing.T) {
	o, _ := setupNonStoredM2M(t)

	for tb, want := range map[string]bool{
		"ns_main_tag_rel":   true,  // 普通 m2m：照建
		"ns_main_plain_rel": false, // tag 上 store(false)
		"ns_main_calc_rel":  false, // builder 上 Store(false)
	} {
		got, err := o.IsTableExist(tb)
		if err != nil {
			t.Fatalf("IsTableExist(%s): %v", tb, err)
		}
		if got != want {
			t.Errorf("表 %s 存在=%v，应为 %v", tb, got, want)
		}
	}
}

func TestNonStoredM2M_Read(t *testing.T) {
	o, mainId := setupNonStoredM2M(t)

	ds, err := o.NewSession().Model("ns.main").Classic().Ids(mainId).Read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	rec := ds.Record()
	if got, ok := rec.GetByField("tag_ids").([]any); !ok || len(got) != 1 {
		t.Errorf("普通 m2m 应读回 1 个 id，实得 %T %v", rec.GetByField("tag_ids"), rec.GetByField("tag_ids"))
	}
	if got, ok := rec.GetByField("calc_ids").([]any); !ok || len(got) != 1 || toStr(got[0]) != toStr(nsCalcTagId) {
		t.Errorf("计算型 m2m 应读回 getter 的值 [%v]，实得 %v", nsCalcTagId, rec.GetByField("calc_ids"))
	}
	// 没有 getter 的计算型 m2m：从前读的是一张恒空的表，现在直接回空列表。
	// ★ 断言键必须在：ManyToMany 报 "models are not correctable" 时错误只进日志，
	//   外面看到的是这个键从结果里消失，不是一个 error。
	v, has := rec.AsMap()["plain_ids"]
	if !has {
		t.Fatalf("plain_ids 从读结果里消失了（OnRead 出错被吞），结果：%v", rec.AsMap())
	}
	if got, ok := v.([]any); !ok || len(got) != 0 {
		t.Errorf("无 getter 的计算型 m2m 应为空列表，实得 %T %v", v, v)
	}
}

func TestNonStoredM2M_WriteAndDelete(t *testing.T) {
	o, mainId := setupNonStoredM2M(t)

	// 写一个没有 setter 的计算型 m2m：忽略，不能去 INSERT 一张不存在的表。
	if _, err := o.Model("ns.main").Ids(mainId).Write(map[string]any{
		"name":      "M1",
		"plain_ids": []any{[]any{6, 0, []any{nsCalcTagId}}},
	}); err != nil {
		t.Fatalf("write plain_ids: %v", err)
	}

	// 删除：关联清理只能碰普通 m2m 的表。
	if _, err := o.Model("ns.main").Delete(mainId); err != nil {
		t.Fatalf("delete: %v", err)
	}
	ds, err := o.NewSession().Model("ns.main").Ids(mainId).Read()
	if err != nil {
		t.Fatalf("read after delete: %v", err)
	}
	if ds.Count() != 0 {
		t.Fatalf("记录应已删除，仍读到 %d 行", ds.Count())
	}
}

func TestNonStoredM2M_Search(t *testing.T) {
	o, mainId := setupNonStoredM2M(t)

	// 有 Searcher：走钩子（钩子把它翻译成 name = 'M1'）。
	ds, err := o.Model("ns.main").Domain(domain.New("calc_ids", "in", []any{nsCalcTagId})).Limit(-1).Read()
	if err != nil {
		t.Fatalf("search calc_ids: %v", err)
	}
	if ds.Count() != 1 || toStr(ds.Record().GetByField("id")) != toStr(mainId) {
		t.Fatalf("calc_ids 应经 Searcher 命中 M1 一条，实得 %d 行", ds.Count())
	}

	// 没有 Searcher：报错点名，不许查空表得出错结果。
	_, err = o.Model("ns.main").Domain(domain.New("plain_ids", "in", []any{nsCalcTagId})).Limit(-1).Read()
	if err == nil || !strings.Contains(err.Error(), "plain_ids") {
		t.Fatalf("对无 Searcher 的计算型 m2m 搜索应报错并点名字段，实得 err=%v", err)
	}
}
