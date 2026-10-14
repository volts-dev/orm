package orm

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/volts-dev/dataset"
)

/*
many2many 写入值的 Go 侧形态回归。

前端经 JSON 进来的命令列表一律是 []any{[]any{...}}，那条路早就认得。但 Go 代码里直接写
写入值时，最自然的写法是 **[][]any**（或者 Set 的 id 用 []int64）：

	"tax_ids": [][]any{{6, 0, taxIds}}     // taxIds 是 []int64

TMany2ManyField.OnWrite 只对 []any 做类型断言，[][]any 整个落进最后的 default 分支：

	M2M field name <tax_ids> could not support this type of value [[6 0 []]]

（2026-10-11 真栈 hr.expense/create，费用一条都建不出来；调用方当时改成 []any 绕过去了，
见 hr_expense 7af4c5a。）而 []any{[]any{6, 0, []int64{...}}} 虽然认成了命令，Set 的第三
元素不是 []any，被当成**单个 id** `[]int64{...}` 整个绑进 SQL。
*/

func m2mTagNames(t *testing.T, fx *x2mFixture, post string) []string {
	t.Helper()
	model, err := fx.orm.GetModel("x2m.post")
	if err != nil {
		t.Fatal(err)
	}
	field := model.GetFieldByName("tag_ids")
	mid := strings.ReplaceAll(field.JoinModelName(), ".", "_")
	ds, err := fx.orm.Query(fmt.Sprintf(`SELECT "%s" AS tag FROM %s WHERE "%s" = ?`,
		field.RelatedKeyName(), mid, field.JoinSourceKey()), fx.posts[post])
	if err != nil {
		t.Fatalf("query rel: %v", err)
	}
	var names []string
	ds.Range(func(_ int, rec *dataset.TRecordSet) error {
		id := rec.FieldByName("tag").AsInteger()
		for n, tid := range fx.tags {
			if tid == id {
				names = append(names, n)
			}
		}
		return nil
	})
	sort.Strings(names)
	return names
}

func TestM2MWrite_GoSliceShapes(t *testing.T) {
	cases := []struct {
		name  string
		value func(fx *x2mFixture) any
		want  []string
	}{
		{"[][]any 清空 (6,0,[])", func(fx *x2mFixture) any {
			return [][]any{{6, 0, []any{}}}
		}, nil},
		{"[][]any 替换 (6,0,[]int64)", func(fx *x2mFixture) any {
			return [][]any{{6, 0, []int64{fx.tags["tag_green"]}}}
		}, []string{"tag_green"}},
		{"[]any 命令 + []int64 的 Set", func(fx *x2mFixture) any {
			return []any{[]any{6, 0, []int64{fx.tags["tag_green"], fx.tags["tag_red"]}}}
		}, []string{"tag_green", "tag_red"}},
		{"[]any 命令 + nil 切片的 Set", func(fx *x2mFixture) any {
			return []any{[]any{6, 0, []int64(nil)}}
		}, nil},
		{"[][]any 链接 (4,id)", func(fx *x2mFixture) any {
			return [][]any{{4, fx.tags["tag_green"]}}
		}, []string{"tag_blue", "tag_green", "tag_red"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fx := setupX2m(t)
			if got := m2mTagNames(t, fx, "post1"); !sameStrings(got, []string{"tag_blue", "tag_red"}) {
				t.Fatalf("夹具不对: %v", got)
			}
			if _, err := fx.orm.Model("x2m.post").Ids(fx.posts["post1"]).Write(map[string]any{"tag_ids": c.value(fx)}); err != nil {
				t.Fatalf("write: %v", err)
			}
			if got := m2mTagNames(t, fx, "post1"); !sameStrings(got, c.want) {
				t.Fatalf("得到 %v，应为 %v", got, c.want)
			}
			// 别的记录不受影响。
			if got := m2mTagNames(t, fx, "post2"); !sameStrings(got, []string{"tag_red"}) {
				t.Fatalf("post2 被连带改了: %v", got)
			}
		})
	}
}

// many2many 的 (0, 0, vals) / (1, id, vals) / (2, id)：此前落进 default 只记一行
// 「M2M command 0 not fully supported yet」就丢掉——任务快速创建里的 `#新标签`（上游
// Command.create）写入成功、标签却从来没链上（真栈 2026-10-13 project.task/create）。
func TestM2MWrite_CreateUpdateDeleteCommands(t *testing.T) {
	tagName := func(t *testing.T, fx *x2mFixture, id any) (string, bool) {
		t.Helper()
		ds, err := fx.orm.Query(`SELECT name FROM x2m_tag WHERE id = ?`, id)
		if err != nil {
			t.Fatalf("query tag: %v", err)
		}
		if ds.Count() == 0 {
			return "", false
		}
		return ds.Record().FieldByName("name").AsString(), true
	}
	linkedIds := func(t *testing.T, fx *x2mFixture, post string) []int64 {
		t.Helper()
		model, _ := fx.orm.GetModel("x2m.post")
		field := model.GetFieldByName("tag_ids")
		mid := strings.ReplaceAll(field.JoinModelName(), ".", "_")
		ds, err := fx.orm.Query(fmt.Sprintf(`SELECT "%s" AS tag FROM %s WHERE "%s" = ?`,
			field.RelatedKeyName(), mid, field.JoinSourceKey()), fx.posts[post])
		if err != nil {
			t.Fatalf("query rel: %v", err)
		}
		var out []int64
		ds.Range(func(_ int, rec *dataset.TRecordSet) error {
			out = append(out, rec.FieldByName("tag").AsInteger())
			return nil
		})
		return out
	}

	t.Run("(0,0,vals) 建出对端记录并链上", func(t *testing.T) {
		fx := setupX2m(t)
		if _, err := fx.orm.Model("x2m.post").Ids(fx.posts["post1"]).Write(map[string]any{
			"tag_ids": []any{[]any{0, 0, map[string]any{"name": "tag_new"}}},
		}); err != nil {
			t.Fatalf("write: %v", err)
		}
		ids := linkedIds(t, fx, "post1")
		if len(ids) != 3 {
			t.Fatalf("原有两个 + 新建一个，应当链着 3 个标签，得到 %v", ids)
		}
		found := false
		for _, id := range ids {
			if n, ok := tagName(t, fx, id); ok && n == "tag_new" {
				found = true
			}
		}
		if !found {
			t.Fatalf("新建的 tag_new 没有链到 post1 上：%v", ids)
		}
		if got := m2mTagNames(t, fx, "post2"); !sameStrings(got, []string{"tag_red"}) {
			t.Fatalf("post2 被连带改了: %v", got)
		}
	})

	t.Run("(1,id,vals) 改对端那一行、关联不动", func(t *testing.T) {
		fx := setupX2m(t)
		if _, err := fx.orm.Model("x2m.post").Ids(fx.posts["post1"]).Write(map[string]any{
			"tag_ids": []any{[]any{1, fx.tags["tag_blue"], map[string]any{"name": "tag_navy"}}},
		}); err != nil {
			t.Fatalf("write: %v", err)
		}
		if n, _ := tagName(t, fx, fx.tags["tag_blue"]); n != "tag_navy" {
			t.Fatalf("对端记录应改名为 tag_navy，得到 %q", n)
		}
		if ids := linkedIds(t, fx, "post1"); len(ids) != 2 {
			t.Fatalf("改名不该动关联，得到 %v", ids)
		}
	})

	t.Run("(2,id) 删掉对端记录与关联", func(t *testing.T) {
		fx := setupX2m(t)
		if _, err := fx.orm.Model("x2m.post").Ids(fx.posts["post1"]).Write(map[string]any{
			"tag_ids": []any{[]any{2, fx.tags["tag_blue"]}},
		}); err != nil {
			t.Fatalf("write: %v", err)
		}
		if _, ok := tagName(t, fx, fx.tags["tag_blue"]); ok {
			t.Fatal("(2,id) 是删除对端记录，tag_blue 应当不在了")
		}
		if got := m2mTagNames(t, fx, "post1"); !sameStrings(got, []string{"tag_red"}) {
			t.Fatalf("post1 应只剩 tag_red，得到 %v", got)
		}
	})

	t.Run("(1/2) 不带 id 拒绝", func(t *testing.T) {
		fx := setupX2m(t)
		for _, cmd := range [][]any{{1, nil, map[string]any{"name": "x"}}, {2, nil}, {2, 0}} {
			if _, err := fx.orm.Model("x2m.post").Ids(fx.posts["post1"]).Write(map[string]any{
				"tag_ids": []any{cmd},
			}); err == nil {
				t.Fatalf("命令 %v 不带 id 应当报错", cmd)
			}
		}
		for _, n := range []string{"tag_red", "tag_blue", "tag_green"} {
			if got, ok := tagName(t, fx, fx.tags[n]); !ok || got != n {
				t.Fatalf("不带 id 的命令不许碰任何标签：%s → %q %v", n, got, ok)
			}
		}
	})
}
