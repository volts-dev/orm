package orm

import (
	"strings"
	"testing"
)

// orderBySql 用 bench.model 走真实的 generate_order_by 生成 ORDER BY 子句。
// 断言在 SQL 生成层（不执行），因为这里要考察的是「排序项有没有被解析出来」，
// 跟行数据无关。
func orderBySql(t *testing.T, o *TOrm, apply func(*TStatement)) string {
	t.Helper()
	sess := o.Model("bench.model")
	apply(&sess.Statement)
	q := NewQuery(sess, []string{`"bench_model"`}, nil, nil, nil, nil)
	return sess.Statement.generate_order_by(q, nil)
}

// fieldPos 返回字段在 ORDER BY 子句里的位置，不在则 -1。
// 两种引号都认：各方言引法不同（sqlite 给普通字段加反引号，主键走 IdKey 分支用双引号），
// 断言不该被引号风格绑死。
func fieldPos(sql, field string) int {
	for _, quoted := range []string{`"` + field + `"`, "`" + field + "`"} {
		if i := strings.Index(sql, quoted); i >= 0 {
			return i
		}
	}
	return -1
}

// TestOrderBy_MultiFieldForms 锁定多字段排序的各种写法都必须**全部字段生效**。
//
// 历史 bug：generate_order_by_inner 按 "," 切分后用 Split(part, " ") 取第 0 段当
// 字段名，于是 " age"（逗号后带空格）切出 ["", "age"]，取到空字段名 → 查不到字段
// → 该排序项被静默丢弃。而 TStatement.OrderBy 链式拼接时插入的正是 ", "，
// 所以 .OrderBy("a").OrderBy("b") 永远只有 a 生效。
//
// 失败是静默的（SQL 照常执行，只是少排一个字段），调用方很难发现；vectors 那边
// 「按 sequence 选发信服务器」就因此写出过看着绿、实则 id 兜底没生效的测试。
func TestOrderBy_MultiFieldForms(t *testing.T) {
	cases := []struct {
		name  string
		apply func(*TStatement)
	}{
		{"变参", func(s *TStatement) { s.OrderBy("name", "age") }},
		{"单串无空格", func(s *TStatement) { s.OrderBy("name,age") }},
		{"单串逗号后带空格", func(s *TStatement) { s.OrderBy("name, age") }},
		{"单串多个空格", func(s *TStatement) { s.OrderBy("name,   age") }},
		{"链式调用", func(s *TStatement) { s.OrderBy("name").OrderBy("age") }},
		{"变参分两次", func(s *TStatement) { s.OrderBy("name").OrderBy("age", "id") }},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := setupIntegrationOrm(t)
			got := orderBySql(t, o, c.apply)

			iName := fieldPos(got, "name")
			iAge := fieldPos(got, "age")
			if iName < 0 {
				t.Fatalf("name 没出现在 ORDER BY 里: %q", got)
			}
			if iAge < 0 {
				t.Fatalf("age 被静默丢弃了: %q", got)
			}
			if iName > iAge {
				t.Errorf("字段次序反了，应 name 在 age 前: %q", got)
			}
		})
	}
}

// 排序方向要能跟着字段一起解析出来，且不受多余空白影响。
// 历史 bug：用 len(order_split) == 2 判断有无方向，"name  desc"（双空格）切出 3 段
// 导致方向静默退化成 ASC——排序结果整个反过来，却没有任何报错。
func TestOrderBy_Direction(t *testing.T) {
	cases := []struct {
		name    string
		clause  string
		wantDir string
	}{
		{"降序", "name desc", "DESC"},
		{"升序显式", "name asc", "ASC"},
		{"大写", "name DESC", "DESC"},
		{"双空格", "name  desc", "DESC"},
		{"前后留白", "  name desc  ", "DESC"},
		{"多字段各自带方向", "name desc,age asc", "DESC"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := setupIntegrationOrm(t)
			got := orderBySql(t, o, func(s *TStatement) { s.OrderBy(c.clause) })
			if !strings.Contains(got, c.wantDir) {
				t.Errorf("方向 %s 丢了: %q", c.wantDir, got)
			}
		})
	}
}

// 空段/空串不能让解析崩掉，也不该产生半截子句。
func TestOrderBy_EmptyAndBlankSegments(t *testing.T) {
	cases := []struct {
		name      string
		apply     func(*TStatement)
		wantEmpty bool
	}{
		{"空串", func(s *TStatement) { s.OrderBy("") }, true},
		{"纯空白", func(s *TStatement) { s.OrderBy("   ") }, true},
		{"无参调用", func(s *TStatement) { s.OrderBy() }, true},
		{"夹空段", func(s *TStatement) { s.OrderBy("name,,age") }, false},
		{"尾随逗号", func(s *TStatement) { s.OrderBy("name,") }, false},
		{"混入空串", func(s *TStatement) { s.OrderBy("name", "", "age") }, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := setupIntegrationOrm(t)
			got := orderBySql(t, o, c.apply)
			if c.wantEmpty && strings.TrimSpace(got) != "" {
				t.Errorf("空输入不该产生子句，得到 %q", got)
			}
			if !c.wantEmpty && !strings.Contains(got, "ORDER BY") {
				t.Errorf("有效字段被整段丢弃: %q", got)
			}
		})
	}
}

// 主键走 IdKey 专用分支（不查字段表），也要能和普通字段混排。
// vectors 的「按 sequence 选发信服务器、id 兜底」就依赖这个组合。
func TestOrderBy_IdMixedWithField(t *testing.T) {
	o := setupIntegrationOrm(t)
	got := orderBySql(t, o, func(s *TStatement) { s.OrderBy("age", "id") })

	iAge := fieldPos(got, "age")
	iId := fieldPos(got, "id")
	if iAge < 0 || iId < 0 {
		t.Fatalf("age 或 id 缺失: %q", got)
	}
	if iAge > iId {
		t.Errorf("次序反了，应 age 在 id 前: %q", got)
	}
}

// 非法排序方向必须整项拒绝（既有的注入防线，勿回退）：order_direction 会被原样拼进
// SQL，白名单之外的值一律丢弃该排序项而不是照拼。
func TestOrderBy_RejectsInvalidDirection(t *testing.T) {
	o := setupIntegrationOrm(t)
	got := orderBySql(t, o, func(s *TStatement) { s.OrderBy("name; DROP TABLE bench_model") })
	if strings.Contains(strings.ToUpper(got), "DROP") {
		t.Fatalf("非法方向被拼进 SQL: %q", got)
	}
}

// 未知字段名不能污染子句（拼错字段名时静默按无序处理，但不能生成半截 SQL）。
func TestOrderBy_UnknownFieldDropped(t *testing.T) {
	o := setupIntegrationOrm(t)
	got := orderBySql(t, o, func(s *TStatement) { s.OrderBy("nonexistent_col", "age") })
	if strings.Contains(got, "nonexistent_col") {
		t.Errorf("未知字段被拼进 SQL: %q", got)
	}
	if fieldPos(got, "age") < 0 {
		t.Errorf("未知字段把同批的有效字段一起带走了: %q", got)
	}
}
