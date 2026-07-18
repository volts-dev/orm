package orm

import (
	"strings"
	"testing"

	"github.com/volts-dev/orm/domain"
)

// TestLike_WildcardWrapping 锁定 name_search 模糊匹配的核心保证：like/ilike 生成的
// SQL 必须把查询值包成 %值%。此前 leaf_to_sql 计算了 need_wildcard 却没包通配符，
// 导致 ilike 退化成整串精确匹配——前端 many2one 输入片段搜不到数据。
//
// 用 SQL 生成层断言(而非执行)，因为 name 字段走 "col::text ilike ?" 是 postgres
// 语法，在测试用的内存 sqlite 上跑不了；这里只验证参数被正确包上通配符。
func TestLike_WildcardWrapping(t *testing.T) {
	o := setupIntegrationOrm(t)
	model := o.Model("bench.model").Statement.Model.GetBase()

	cases := []struct {
		op       string
		wrapped  bool // 期望值被包成 %lph%
	}{
		{"ilike", true},
		{"like", true},
		{"not ilike", true},
		{"=ilike", false}, // "原样"变体：不包通配符，由调用方自带
		{"=", false},
	}

	for _, c := range cases {
		t.Run(c.op, func(t *testing.T) {
			dom := domain.New("name", c.op, "lph")
			exp, err := NewExpression(o, model, dom, nil)
			if err != nil {
				t.Fatalf("NewExpression(%s): %v", c.op, err)
			}
			_, params := exp.toSql()

			var found string
			for _, p := range params {
				if s, ok := p.(string); ok && strings.Contains(s, "lph") {
					found = s
					break
				}
			}
			if found == "" {
				t.Fatalf("op %q: no param carrying the search term, params=%v", c.op, params)
			}
			if c.wrapped && found != "%lph%" {
				t.Fatalf("op %q: expected wildcard-wrapped %%lph%%, got %q", c.op, found)
			}
			if !c.wrapped && found != "lph" {
				t.Fatalf("op %q: expected raw value lph (no auto wildcard), got %q", c.op, found)
			}
		})
	}
}
