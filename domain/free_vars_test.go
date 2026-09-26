package domain

import (
	"reflect"
	"testing"
)

func TestFreeVariables(t *testing.T) {
	cases := []struct {
		expr string
		want []string
	}{
		{"", nil},
		{"[]", nil},
		{"[('kind','=','in')]", nil},
		{"[('active','=',True)]", nil},
		{"[('x','=',False), ('y','=',None)]", nil},
		{"[('display_type', 'in', ['product', 'line_section'])]", nil},
		{"[('n','=',-1.5), ('m','in',[1,2]), ('p','=',?)]", nil},
		{`[("a", "=", "b")]`, nil},
		{"[('attribute_id', '=', attribute_id)]", []string{"attribute_id"}},
		{"[('product_template_id', '=', product_template_id), ('ptav_active', '=', True)]", []string{"product_template_id"}},
		{"[('user_id','=',uid), ('company_id','=',parent.company_id), ('x','=',uid)]", []string{"uid", "parent"}},
		{"[('grp_id', 'in', [grp_id, 0])]", []string{"grp_id"}},
	}
	for _, c := range cases {
		if got := FreeVariables(c.expr); !reflect.DeepEqual(got, c.want) {
			t.Errorf("FreeVariables(%q) = %#v，期望 %#v", c.expr, got, c.want)
		}
	}
}
