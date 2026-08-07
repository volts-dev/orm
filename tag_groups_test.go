package orm

import "testing"

// groups tag 此前只是处理器表里的一行注释——写了 `groups(...)` 的字段
// permissionGroups 恒为空串，字段级权限的上层消费方拿到的永远是"不限组"。
// 存量两处声明（product 的 StandardPrice）用了两种写法，都必须认。
func TestTagGroups(t *testing.T) {
	cases := []struct {
		name   string
		params []string
		want   string
	}{
		{"不带引号", []string{"registry.group_user"}, "registry.group_user"},
		{"带单引号", []string{"'registry.group_user'"}, "registry.group_user"},
		{"带双引号", []string{`"registry.group_user"`}, "registry.group_user"},
		{"多参数", []string{"'a.g1'", "'b.g2'"}, "a.g1,b.g2"},
		{"单参数内逗号分隔", []string{"a.g1,b.g2"}, "a.g1,b.g2"},
		{"逗号加空格", []string{"a.g1, b.g2"}, "a.g1,b.g2"},
		{"混合引号与逗号", []string{"'a.g1', 'b.g2'"}, "a.g1,b.g2"},
		// 空参数不能变成一个空组名：下游按"组列表非空即受限"判断，
		// 一个空串元素会让字段变成"只有名为空的组能看"，即谁都看不到。
		{"空参数", []string{""}, ""},
		{"只有引号", []string{"''"}, ""},
		{"逗号但无内容", []string{" , "}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &TField{}
			f.name = "standard_price"
			if err := tag_groups(&TTagContext{Field: f, Params: c.params}); err != nil {
				t.Fatalf("意外报错: %v", err)
			}
			if got := f.Groups(); got != c.want {
				t.Errorf("groups(%v) = %q，期望 %q", c.params, got, c.want)
			}
		})
	}
}

// 不写 tag 的字段必须保持空串：下游据此判断"该字段不限组"。
func TestTagGroups_EmptyMeansUnrestricted(t *testing.T) {
	f := &TField{}
	if got := f.Groups(); got != "" {
		t.Errorf("未声明 groups 的字段应为空串，实得 %q", got)
	}
	if err := tag_groups(&TTagContext{Field: f}); err != nil {
		t.Fatal(err)
	}
	if got := f.Groups(); got != "" {
		t.Errorf("groups() 无参数时应保持空串，实得 %q", got)
	}
}
