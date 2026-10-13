package orm

import (
	"sort"
	"strings"
	"testing"

	"github.com/volts-dev/utils"
)

// any / not any：右值是对端上的子 domain。夹具同 expr_x2many_test（post1: red+blue, line1+line2；post2: red；
// post3: line3；orphan 行没有 post）。判据都是集合本身。
func TestAnyOperator(t *testing.T) {
	fx := setupX2m(t)
	posts := func(dom []any) string {
		t.Helper()
		ds, err := fx.orm.Model("x2m.post").Domain(dom).Limit(-1).Read()
		if err != nil {
			t.Fatalf("read %v: %v", dom, err)
		}
		var names []string
		for _, id := range ds.Keys() {
			for n, pid := range fx.posts {
				if pid == utils.ToInt64(id) {
					names = append(names, n)
				}
			}
		}
		sort.Strings(names)
		return strings.Join(names, ",")
	}
	lines := func(dom []any) string {
		t.Helper()
		ds, err := fx.orm.Model("x2m.line").Domain(dom).Limit(-1).Read()
		if err != nil {
			t.Fatalf("read %v: %v", dom, err)
		}
		var names []string
		for _, id := range ds.Keys() {
			for n, lid := range fx.lines {
				if lid == utils.ToInt64(id) {
					names = append(names, n)
				}
			}
		}
		sort.Strings(names)
		return strings.Join(names, ",")
	}
	leaf := func(f, op string, v any) []any { return []any{f, op, v} }

	cases := []struct {
		name, got, want string
	}{
		{"o2m any", posts([]any{leaf("line_ids", "any", []any{leaf("name", "=", "line1")})}), "post1"},
		{"o2m any 多条件落在同一条记录上", posts([]any{leaf("line_ids", "any", []any{leaf("name", "=", "line1"), leaf("name", "=", "line2")})}), ""},
		{"o2m not any", posts([]any{leaf("line_ids", "not any", []any{leaf("name", "=", "line1")})}), "post2,post3"},
		{"m2m any", posts([]any{leaf("tag_ids", "any", []any{leaf("name", "in", []any{"tag_blue", "tag_green"})})}), "post1"},
		{"m2m not any", posts([]any{leaf("tag_ids", "not any", []any{leaf("name", "=", "tag_red")})}), "post3"},
		{"m2o any", lines([]any{leaf("post_id", "any", []any{leaf("name", "=", "post3")})}), "line3"},
		{"m2o not any（含没有父记录的孤儿行）", lines([]any{leaf("post_id", "not any", []any{leaf("name", "=", "post1")})}), "line3,orphan"},
		{"点号穿过 m2o 再 any", lines([]any{leaf("post_id.tag_ids", "any", []any{leaf("name", "=", "tag_blue")})}), "line1,line2"},
		{"any 匹配不到", posts([]any{leaf("line_ids", "any", []any{leaf("name", "=", "nope")})}), ""},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %q want %q", c.name, c.got, c.want)
		}
	}
}
