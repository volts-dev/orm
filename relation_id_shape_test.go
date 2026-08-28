package orm

import (
	"testing"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/utils"

	_ "modernc.org/sqlite"
)

// 这一组用例锁死一件事：**一次 read 里，同一个关系列有两种形态**，而读到哪一种由
// 字段顺序决定 —— 关系字段的四个入口必须两种都认。
//
// TSession._read 把所有计算字段串成**一个循环**依次调 OnRead。TMany2OneField.OnRead
// 会**就地改写** dataset 里那一列：裸外键 → false 或 map{id,name,…}。于是排在它后面
// 的任何一个 getter，读同一条记录的这一列时拿到的是 map。
//
// 而字段顺序在不点名读时来自 TModelObject.GetFields()，那是 sync.Map.Range —— **顺序
// 随机**。所以这不是"某个模型写错了"，是同一次请求这回好下回崩。
//
// 崩的形态是 panic `hash of unhashable type: map[string]interface {}`（map 拿去做
// map 键），被 router 的 recover 兜成一条脱敏的 500，用户看到"服务器内部错误，请稍
// 后再试（错误编号 ERR-xxxxxxxx）"，日志里的 panic 既不提模型名也不提字段名。
// 真实案例：vectors 的 res.group 上 full_name 的 getter 读 category_id，读一次权限组
// 就是一次赌博。

type RelCategoryModel struct {
	TModel `table:"name('rel_category')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar() size(64)"`
}

type RelGroupModel struct {
	TModel     `table:"name('rel_group')"`
	Id         int64  `field:"pk autoincr"`
	Name       string `field:"varchar() size(64)"`
	CategoryId int64  `field:"many2one(rel_category)"`
}

func (self *RelGroupModel) OnBuildFields() error {
	if err := self.TModel.OnBuildFields(); err != nil {
		return err
	}
	self.Builder().VarcharField("full_name").Store(false).Getter(relGroupFullName)
	return nil
}

// relGroupFullName 是 res.group 上那个 getter 的等价物，而且**故意保留最朴素的写法**：
// 直接把这一列的原值喂给 ManyToOne。getter 不知道、也不该知道自己排在第几个，归一
// 是关系入口的责任。它一旦要求调用方先归一，就等于要求每个模块作者都想到这件事。
func relGroupFullName(ctx *TFieldContext) error {
	model := ctx.Model
	cats, err := model.ManyToOne(&TFieldContext{
		Session:    ctx.Session,
		Model:      model,
		Ids:        ctx.Dataset.Keys("category_id"),
		Field:      model.GetFieldByName("category_id"),
		UseNameGet: true,
	})
	if err != nil {
		return err
	}

	// 按字符串归一后匹配：两列的 Go 类型不保证一致，同 TMany2OneField.OnRead 的口径。
	byId := make(map[string]*dataset.TDataSet)
	for key, grp := range cats.GroupBy("id") {
		byId[utils.ToString(key)] = grp
	}

	fname := ctx.Field.Name()
	return ctx.Dataset.Range(func(pos int, record *dataset.TRecordSet) error {
		prefix := ""
		// 模块侧读另一个关系列的正规写法：过 RelationId，两种形态都认。
		if id, ok := RelationId(record.GetByField("category_id")); ok {
			if grp := byId[utils.ToString(id)]; grp != nil {
				prefix = grp.FieldByName("name").AsString() + " / "
			}
		}
		record.SetByField(fname, prefix+record.FieldByName("name").AsString())
		return nil
	})
}

// TestGetterReadsM2OColumnAfterItsOwnOnRead 是那次 500 的直接回归。
//
// Select 的顺序就是 OnRead 的顺序（不点名时才走随机的 GetFields），所以两个顺序各跑
// 一遍：一遍 getter 拿到裸外键，一遍拿到 many2one 归一后的 map。两遍必须给同一个答案。
func TestGetterReadsM2OColumnAfterItsOwnOnRead(t *testing.T) {
	ds := &TDataSource{DbType: "sqlite", DbName: ":memory:"}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(RelCategoryModel), new(RelGroupModel)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}

	catIds, err := o.Model("rel.category").Create(map[string]any{"name": "Cat"})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	grpIds, err := o.Model("rel.group").Create(map[string]any{"name": "G1", "category_id": catIds[0]})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	cases := []struct {
		name   string
		fields []string
	}{
		// category_id 在前 = many2one 先归一，getter 拿到的是 map。就是崩的那一半。
		{"m2o_normalized_first", []string{"id", "name", "category_id", "full_name"}},
		// full_name 在前 = getter 拿到裸外键。这一半一直是好的，一并锁住别修坏。
		{"getter_first", []string{"id", "name", "full_name", "category_id"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := o.Model("rel.group").Ids(grpIds[0]).Select(tc.fields...).Classic().Read()
			if err != nil {
				t.Fatalf("classic read: %v", err)
			}
			if res.Count() != 1 {
				t.Fatalf("count = %d, want 1", res.Count())
			}
			res.First()
			if got := res.Record().FieldByName("full_name").AsString(); got != "Cat / G1" {
				t.Fatalf("full_name = %q, want %q —— getter 读到的 category_id 形态没被认出来", got, "Cat / G1")
			}
		})
	}
}

// TestManyToOneAcceptsClassicShapedIds 不经过 read，直接把经典形态喂给关系入口。
// 上面那个用例依赖字段顺序，这个不依赖任何顺序，坏了一定红。
func TestManyToOneAcceptsClassicShapedIds(t *testing.T) {
	ds := &TDataSource{DbType: "sqlite", DbName: ":memory:"}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(RelCategoryModel), new(RelGroupModel)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	catIds, err := o.Model("rel.category").Create(map[string]any{"name": "Cat"})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}

	model, err := o.GetModel("rel.group")
	if err != nil {
		t.Fatalf("GetModel: %v", err)
	}

	// many2one 归一后这一列的三种取值，外加裸 id：四种都得认，且都不许 panic。
	// false 与空 map 是"没有关联"，不产生 id。
	got, err := model.ManyToOne(&TFieldContext{
		Model: model,
		Field: model.GetFieldByName("category_id"),
		Ids: []any{
			map[string]any{"id": catIds[0], "name": "Cat"}, // 经典形态：有关联
			catIds[0],        // 裸外键（同一个 id，必须被去重掉）
			false,            // 经典形态：没有关联
			map[string]any{}, // 悬空/不可见时只剩壳
		},
		UseNameGet: true,
	})
	if err != nil {
		t.Fatalf("ManyToOne: %v", err)
	}
	if got.Count() != 1 {
		t.Fatalf("count = %d, want 1 —— 经典形态与裸 id 指向同一条，应当去重成一次查询", got.Count())
	}
	got.First()
	if name := got.Record().FieldByName("name").AsString(); name != "Cat" {
		t.Fatalf("name = %q, want Cat", name)
	}
}

func TestRelationIdOf(t *testing.T) {
	cases := []struct {
		name     string
		in       any
		idField  string
		wantId   any
		wantOk   bool
		resolved bool
	}{
		{"裸外键", int64(7), "id", int64(7), true, true},
		{"经典形态", map[string]any{"id": int64(7), "name": "x"}, "id", int64(7), true, true},
		{"自定义主键列名", map[string]any{"code": int64(7)}, "code", int64(7), true, true},
		{"主键列名不同但有 id 兜底", map[string]any{"id": int64(7)}, "code", int64(7), true, true},
		{"NameGet 元组", []any{int64(7), "x"}, "id", int64(7), true, true},
		{"没有关联", false, "id", nil, false, true},
		{"空 FK", int64(0), "id", nil, false, true},
		{"哨兵 -1", int64(-1), "id", nil, false, true},
		{"nil", nil, "id", nil, false, true},
		{"取不出主键的 map", map[string]any{"name": "x"}, "id", nil, false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, ok, resolved := relationIdOf(tc.in, tc.idField)
			if ok != tc.wantOk || resolved != tc.resolved {
				t.Fatalf("ok=%v resolved=%v, want ok=%v resolved=%v", ok, resolved, tc.wantOk, tc.resolved)
			}
			if ok && id != tc.wantId {
				t.Fatalf("id = %#v, want %#v", id, tc.wantId)
			}
		})
	}
}

// relationIds 的去重必须按字符串做：同一个 id 从不同来源回来可能是 int64 也可能是
// 字符串（驱动整型宽度、BigNumberToString 打开时），用 any 直接做键就悄悄漏掉。
func TestRelationIdsDedupAcrossGoTypes(t *testing.T) {
	got := relationIds([]any{
		int64(7),
		"7",
		map[string]any{"id": int64(7)},
		float64(8),
		nil,
		false,
	}, "id", "test")

	if len(got) != 2 {
		t.Fatalf("ids = %#v, want 2 个（7 与 8）", got)
	}
	if utils.ToString(got[0]) != "7" || utils.ToString(got[1]) != "8" {
		t.Fatalf("ids = %#v, want [7 8]（原顺序）", got)
	}
}
