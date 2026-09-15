package orm

import "testing"

// domain 标签里的单引号写成两个（标签参数本身用单引号包，同 title / help）。此前不反转义，
// 双写的引号原样下发，前端与服务端都解析成一串空串 —— 模型上写着的 domain 静默不生效。
func TestTagDomain_UnescapesDoubledQuotes(t *testing.T) {
	cases := []struct {
		name  string
		param string
		want  string
	}{
		{"静态", "'[(''type'', ''='', ''consu'')]'", "[('type', '=', 'consu')]"},
		{"引用记录字段", "'[''&'', (''product_template_id'', ''='', product_tmpl_id), (''type'', ''='', ''consu'')]'",
			"['&', ('product_template_id', '=', product_tmpl_id), ('type', '=', 'consu')]"},
		{"没有引号", "[('a', '=', 1)]", "[('a', '=', 1)]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &TField{}
			if err := tag_domain(&TTagContext{Field: f, Params: []string{c.param}}); err != nil {
				t.Fatalf("意外报错: %v", err)
			}
			if got := f.Domain(); got != c.want {
				t.Errorf("domain(%s) = %q，期望 %q", c.param, got, c.want)
			}
		})
	}
}
