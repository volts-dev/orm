package orm

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/volts-dev/orm/domain"
	"github.com/volts-dev/utils"
)

/*
直接 x2many 叶子的下推回归。

判据全部是"筛出来的应该比不筛少"——这是本类 bug 唯一在外部可见的形状：条件被丢掉
时查询照样成功、照样返回一堆看着正常的行，只是**一条都没筛掉**。所以每个用例都显式
断言集合本身，绝不只断言 err == nil。
*/

type (
	X2mTag struct {
		TModel `table:"name('x2m_tag')"`
		Id     int64  `field:"pk autoincr title('ID')"`
		Name   string `field:"varchar() size(64)"`
	}

	X2mLine struct {
		TModel `table:"name('x2m_line')"`
		Id     int64  `field:"pk autoincr title('ID')"`
		Name   string `field:"varchar() size(64)"`
		PostId int64  `field:"many2one(x2m_post)"`
	}

	X2mPost struct {
		TModel  `table:"name('x2m_post')"`
		Id      int64  `field:"pk autoincr title('ID')"`
		Name    string `field:"varchar() size(64)"`
		TagIds  []any  `field:"many2many(x2m_tag,post_id,tag_id)"`
		LineIds []any  `field:"one2many(x2m_line,post_id)"`
	}
)

// firstId 取 Create 的返回值——它是一个 id 切片，不是标量。
func firstId(t *testing.T, v any) int64 {
	t.Helper()
	if ids, ok := v.([]any); ok {
		if len(ids) == 0 {
			t.Fatalf("Create returned an empty id list")
		}
		return utils.ToInt64(ids[0])
	}
	return utils.ToInt64(v)
}

// OnBuildFields 给 x2m.tag 加一个**非存储**的名称候选列，用来复现
// pro.tmpl.attr.value.name / res.partner.category.display_name 那种形态。
func (self X2mTag) OnBuildFields() error {
	if err := self.TModel.OnBuildFields(); err != nil {
		return err
	}
	self.Builder().VarcharField("computed_label").Store(false).Readonly(true)
	return nil
}

type x2mFixture struct {
	orm   *TOrm
	posts map[string]int64 // name -> id
	tags  map[string]int64
	lines map[string]int64
}

// setupX2m 造一个最小但足够分辨对错的数据集：
//
//	post1 → tag_red, tag_blue   两行明细
//	post2 → tag_red             无明细
//	post3 → 无标签              一行明细
//
// 任何一条过滤器只要被丢掉，返回的就是全部 3 条 post——与正确答案（1 或 2 条）不同。
func setupX2m(t *testing.T) *x2mFixture {
	t.Helper()
	// 不能用 :memory: —— sqlite 的内存库是**每连接一个**，而 SyncModel 在事务里建表、
	// m2m 关联表却走 orm.Exec 另开连接，两边落在不同的库上，表看着建了却查不到。
	ds := &TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "x2m.db")}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(X2mTag), new(X2mLine), new(X2mPost)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}

	fx := &x2mFixture{orm: o, posts: map[string]int64{}, tags: map[string]int64{}, lines: map[string]int64{}}

	for _, n := range []string{"tag_red", "tag_blue", "tag_green"} {
		id, err := o.Model("x2m.tag").Create(map[string]any{"name": n})
		if err != nil {
			t.Fatalf("create tag %s: %v", n, err)
		}
		fx.tags[n] = firstId(t, id)
	}
	for _, n := range []string{"post1", "post2", "post3"} {
		id, err := o.Model("x2m.post").Create(map[string]any{"name": n})
		if err != nil {
			t.Fatalf("create post %s: %v", n, err)
		}
		fx.posts[n] = firstId(t, id)
	}
	for _, l := range []struct{ name, post string }{
		{"line1", "post1"}, {"line2", "post1"}, {"line3", "post3"},
	} {
		id, err := o.Model("x2m.line").Create(map[string]any{"name": l.name, "post_id": fx.posts[l.post]})
		if err != nil {
			t.Fatalf("create line %s: %v", l.name, err)
		}
		fx.lines[l.name] = firstId(t, id)
	}

	// m2m 关联行直接写中间表：本文件测的是**读/筛**，用 m2m 写入路径当夹具会把两件
	// 事的失败混在一起。表名与列名从字段元数据取，不硬编码。
	model, err := o.GetModel("x2m.post")
	if err != nil {
		t.Fatalf("GetModel: %v", err)
	}
	field := model.GetFieldByName("tag_ids")
	if field == nil {
		t.Fatal("x2m.post has no tag_ids field")
	}
	mid := strings.ReplaceAll(field.JoinModelName(), ".", "_")
	// 关联表在 sqlite 上建不出来：TMany2ManyField.UpdateDb 走 orm.Exec 另开连接，
	// 而 SyncModel 的事务正握着写锁 → SQLITE_BUSY。postgres 上没有这个问题（真栈
	// 里这些表是存在的），所以这里补建一份夹具，不去改生产代码的 DDL 路径。
	if _, err := o.Exec(fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s ("%s" INTEGER NOT NULL, "%s" INTEGER NOT NULL, UNIQUE("%s","%s"))`,
		mid, field.JoinSourceKey(), field.RelatedKeyName(), field.JoinSourceKey(), field.RelatedKeyName(),
	)); err != nil {
		t.Fatalf("create m2m rel table %s: %v", mid, err)
	}
	for _, link := range []struct{ post, tag string }{
		{"post1", "tag_red"}, {"post1", "tag_blue"}, {"post2", "tag_red"},
	} {
		q := fmt.Sprintf(`INSERT INTO %s ("%s","%s") VALUES (?,?)`, mid, field.JoinSourceKey(), field.RelatedKeyName())
		if _, err := o.Exec(q, fx.posts[link.post], fx.tags[link.tag]); err != nil {
			t.Fatalf("link %s-%s (%s): %v", link.post, link.tag, q, err)
		}
	}
	return fx
}

// search 跑一次带 domain 的读，返回命中的 post 名字（排序后便于比较）。
func (fx *x2mFixture) search(t *testing.T, node *domain.TDomainNode) []string {
	t.Helper()
	ds, err := fx.orm.Model("x2m.post").Domain(node).Limit(-1).Read()
	if err != nil {
		t.Fatalf("read %s: %v", node.String(), err)
	}
	var names []string
	if ds != nil {
		for _, id := range ds.Keys() {
			for name, pid := range fx.posts {
				if pid == utils.ToInt64(id) {
					names = append(names, name)
				}
			}
		}
	}
	sort.Strings(names)
	return names
}

func (fx *x2mFixture) assertSearch(t *testing.T, node *domain.TDomainNode, want ...string) {
	t.Helper()
	sort.Strings(want)
	got := fx.search(t, node)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("domain %s\n got: %v\nwant: %v", node.String(), got, want)
	}
}

// TestX2many_M2M_Filters 是本次修复的主判据。
//
// 修复前每一个 case 返回的都是 post1,post2,post3 —— 条件被静默丢掉，整表返回。
func TestX2many_M2M_Filters(t *testing.T) {
	fx := setupX2m(t)
	red, blue, green := fx.tags["tag_red"], fx.tags["tag_blue"], fx.tags["tag_green"]

	t.Run("in 单个 id", func(t *testing.T) {
		fx.assertSearch(t, domain.New("tag_ids", "in", red), "post1", "post2")
	})
	t.Run("in 多个 id 取并集", func(t *testing.T) {
		fx.assertSearch(t, domain.New("tag_ids", "in", blue, green), "post1")
	})
	t.Run("= 单个 id", func(t *testing.T) {
		fx.assertSearch(t, domain.New("tag_ids", "=", blue), "post1")
	})
	t.Run("not in 排除", func(t *testing.T) {
		fx.assertSearch(t, domain.New("tag_ids", "not in", red), "post3")
	})
	// 右值是字符串时按对端记录名解析。这里用 = 而不是 ilike：ilike 走的是
	// `col::text ilike ?` 的 postgres 语法，sqlite 直接报 unrecognized token ":"，
	// 与本次改动无关。走的是同一条"字符串 → 查对端 → 拿 id"的路径。
	t.Run("按标签名匹配", func(t *testing.T) {
		fx.assertSearch(t, domain.New("tag_ids", "=", "tag_blue"), "post1")
	})
	// 负向操作符必须**先取反再查对端**：'!=' 要找的是"叫 tag_red 的标签"，然后排除
	// 拥有它的父记录。若直接拿 '!=' 去查对端，得到的是"不叫 tag_red 的标签"，排除
	// 拥有它们的父记录，结论正好相反（会返回 post2 而不是 post3）。
	t.Run("按标签名取反", func(t *testing.T) {
		fx.assertSearch(t, domain.New("tag_ids", "!=", "tag_red"), "post3")
	})
	t.Run("点号路径", func(t *testing.T) {
		fx.assertSearch(t, domain.New("tag_ids.name", "=", "tag_red"), "post1", "post2")
	})
	t.Run("= False 即没有任何标签", func(t *testing.T) {
		fx.assertSearch(t, domain.New("tag_ids", "=", false), "post3")
	})
	t.Run("!= False 即至少有一个标签", func(t *testing.T) {
		fx.assertSearch(t, domain.New("tag_ids", "!=", false), "post1", "post2")
	})
}

// TestX2many_O2M_Filters 同上，one2many 侧。
func TestX2many_O2M_Filters(t *testing.T) {
	fx := setupX2m(t)

	t.Run("in 单个明细 id", func(t *testing.T) {
		fx.assertSearch(t, domain.New("line_ids", "in", fx.lines["line3"]), "post3")
	})
	t.Run("in 同一父记录的两条明细去重", func(t *testing.T) {
		fx.assertSearch(t, domain.New("line_ids", "in", fx.lines["line1"], fx.lines["line2"]), "post1")
	})
	t.Run("点号路径", func(t *testing.T) {
		fx.assertSearch(t, domain.New("line_ids.name", "=", "line2"), "post1")
	})
	t.Run("= False 即没有任何明细", func(t *testing.T) {
		fx.assertSearch(t, domain.New("line_ids", "=", false), "post2")
	})
}

// TestX2many_NonexistentIdMatchesNothing 直接钉死 2026-08-21 真栈上量到的现象：
// 按一个根本不存在的关联 id 去筛，结果数**等于**不筛。那时三个模型都是
// 54→54 / 56→56 / 9→9。这里必须是 3→0。
func TestX2many_NonexistentIdMatchesNothing(t *testing.T) {
	fx := setupX2m(t)

	all := fx.search(t, domain.New("id", "!=", 0))
	if len(all) != 3 {
		t.Fatalf("夹具不对：不加过滤应有 3 条 post，实得 %v", all)
	}
	fx.assertSearch(t, domain.New("tag_ids", "in", int64(999999)))
	fx.assertSearch(t, domain.New("line_ids", "in", int64(999999)))
}

// TestX2many_CombinedWithScalarIntersects 防的是"条件被丢掉后连累同一棵树里的其他
// 条件"这一类：x2many 叶子必须与相邻条件求交，而不是把结果放大。
func TestX2many_CombinedWithScalarIntersects(t *testing.T) {
	fx := setupX2m(t)

	node := domain.New("tag_ids", "in", fx.tags["tag_red"])
	node.AND(domain.New("name", "=", "post2"))
	fx.assertSearch(t, node, "post2")

	// 交集为空时同样必须是空，而不是回落成其中一边。
	node2 := domain.New("tag_ids", "in", fx.tags["tag_blue"])
	node2.AND(domain.New("name", "=", "post2"))
	fx.assertSearch(t, node2)
}

// TestIdListNode_AlwaysList 锁 idListNode 的哨兵行为：单元素列表会退化成 VALUE_NODE，
// 而 leaf_to_sql 的 in/not in 分支只认 LIST_NODE，退化后走的是"右值是布尔"的错误分支。
func TestIdListNode_AlwaysList(t *testing.T) {
	cases := [][]any{{}, {int64(11)}, {int64(11), int64(12)}}
	for _, ids := range cases {
		node := idListNode(ids)
		if !node.IsListNode() {
			t.Fatalf("idListNode(%v) 不是 LIST_NODE: %s", ids, node.String())
		}
		if node.Count() < 2 {
			t.Fatalf("idListNode(%v) 只有 %d 个元素，会退化", ids, node.Count())
		}
	}
}

// TestX2many_HierarchyOperatorIsLoud 钉住"不实现就报错"的取舍：child_of 在 x2many 上
// 没有实现，老代码是空函数体（条件静默消失）。宁可报错也不要再发一份错数据。
func TestX2many_HierarchyOperatorIsLoud(t *testing.T) {
	fx := setupX2m(t)
	_, err := fx.orm.Model("x2m.post").Domain(domain.New("tag_ids", "child_of", fx.tags["tag_red"])).Read()
	if err == nil {
		t.Fatal("child_of on a many2many should report an error, not silently drop the leaf")
	}
	if !strings.Contains(err.Error(), "child_of") {
		t.Fatalf("错误信息该点名操作符，实得: %v", err)
	}
}

// TestX2many_IdsArriveAsStrings 钉住雪花 id 的现实形态：BigNumberToString 打开后
// 前端拿到的 id 是**字符串**，原样发回来就是 ('tag_ids','in',["2079..."])。
// 修复前这种右值被当成"标签名"去查对端，一条也匹配不上，于是整条过滤器废掉。
func TestX2many_IdsArriveAsStrings(t *testing.T) {
	fx := setupX2m(t)
	red := fmt.Sprintf("%d", fx.tags["tag_red"])
	blue := fmt.Sprintf("%d", fx.tags["tag_blue"])

	fx.assertSearch(t, domain.New("tag_ids", "in", red), "post1", "post2")
	fx.assertSearch(t, domain.New("tag_ids", "in", red, blue), "post1", "post2")
	fx.assertSearch(t, domain.New("tag_ids", "=", blue), "post1")

	// like 一族永远按名字，不能把数字串认成 id——否则按名称搜就没法搜数字开头的标签。
	fx.assertSearch(t, domain.New("tag_ids", "=", "tag_red"), "post1", "post2")
}

// TestX2manyOwnerIds_EmptyIsNotAll 是真栈上抓到的那个反转：
//
//	targetIds 为空（对端一条都没匹配上）→ 没有任何 owner
//	anyRelated=true（右值是 False）      → 所有有关联的 owner
//
// 修复前两者都用 targetIds == nil 表达，撞在一起：按一个不存在的标签去筛，54 条联系人
// 回了 5 条——正好是"所有有标签的联系人"，看着像筛过了，答案却完全相反。
// 沿 sqlite 的空结果集返回 []（而非 nil）这条路，上面那些集合断言恰好测不到它。
func TestX2manyOwnerIds_EmptyIsNotAll(t *testing.T) {
	fx := setupX2m(t)
	model, err := fx.orm.GetModel("x2m.post")
	if err != nil {
		t.Fatal(err)
	}
	comodel, err := fx.orm.GetModel("x2m.tag")
	if err != nil {
		t.Fatal(err)
	}
	field := model.GetFieldByName("tag_ids")
	exp := &TExpression{orm: fx.orm, session: fx.orm.Model("x2m.post")}

	for _, empty := range [][]any{nil, {}} {
		ids, err := exp.x2manyOwnerIds(comodel, field, empty, false)
		if err != nil {
			t.Fatalf("targetIds=%v: %v", empty, err)
		}
		if len(ids) != 0 {
			t.Fatalf("targetIds=%v 应该一个 owner 都没有，实得 %v", empty, ids)
		}
	}

	all, err := exp.x2manyOwnerIds(comodel, field, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 { // post1 与 post2 有标签，post3 没有
		t.Fatalf("anyRelated 应返回 2 个 owner（post1/post2），实得 %v", all)
	}
}

// TestIdValueOf_AcceptsJsonNumber 钉住 json.Number：JSON 解码出来的数字在这条链路上
// 是 `type Number string` 的具名类型，`Value.(string)` 断言不到、IsNumeric() 也不认。
// 漏了它，`('tag_ids','in',[123])` 会被当成"按名字搜 123"，而这个误判在对端名称字段是
// 存储列时表现为"返回 0 条"（看着完全正常），非存储列时表现为"整表返回"。
func TestIdValueOf_AcceptsJsonNumber(t *testing.T) {
	cases := []struct {
		name string
		val  any
		want bool
	}{
		{"json.Number", json.Number("2084955429107929088"), true},
		{"裸字符串 id", "2084955429107929088", true},
		{"int64", int64(123), true},
		{"名字", "tag_red", false},
		{"带前缀的名字", "12ab", false},
		{"空串", "", false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			node := domain.NewDomainNode()
			node.Push(c.val)
			_, ok := idValueOf(node)
			if ok != c.want {
				t.Fatalf("idValueOf(%#v) = %v, want %v", c.val, ok, c.want)
			}
		})
	}
}

// TestX2many_UnsearchableComodelNameIsLoud 钉住"下钻到对端之前先拦一道"。
//
// 对端的名称字段是非存储计算列时，拿它去查对端会掉进同一个坑：叶子被丢掉、对端整表
// 返回、于是本模型凡有关联的记录全部命中——按名字筛反而筛出更多。这种情况必须报错。
func TestX2many_UnsearchableComodelNameIsLoud(t *testing.T) {
	fx := setupX2m(t)

	tag, err := fx.orm.GetModel("x2m.tag")
	if err != nil {
		t.Fatal(err)
	}
	// 先确认这道闸本身认得出可搜 / 不可搜。
	if err := assertSearchable(tag, "name"); err != nil {
		t.Fatalf("存储列 name 应当可搜: %v", err)
	}
	if err := assertSearchable(tag, "computed_label"); err == nil {
		t.Fatal("非存储列 computed_label 应当被拦下")
	}

	// 再把记录名指过去，走完整条读路径。
	tag.SetRecordName("computed_label")
	if got := tag.GetRecordName(); got != "computed_label" {
		t.Fatalf("夹具没生效，GetRecordName()=%q", got)
	}

	// 走真正的解析入口。这里直接传 comodel 实例，因为 orm.GetModel 每次给的是克隆，
	// 夹具改不到表达式内部拿到的那一个。
	exp := &TExpression{orm: fx.orm, session: fx.orm.Model("x2m.post")}
	right := domain.NewDomainNode()
	right.Push("tag_red")
	_, _, err = exp.x2manyTargetIds(tag, []string{"tag_ids"}, "=", right)
	if err == nil {
		t.Fatal("对端名称字段不可搜时必须报错，而不是回一份\"看着正常\"的全表")
	}
	if !strings.Contains(err.Error(), "computed_label") {
		t.Fatalf("错误信息该点名那个字段，实得: %v", err)
	}

	// 按 id 筛不受影响：那条路根本不碰名称字段。
	fx.assertSearch(t, domain.New("tag_ids", "in", fx.tags["tag_red"]), "post1", "post2")
}
