package orm

import (
	"reflect"
	"strings"
	"testing"
)

// digits 是**显示精度**，对齐 Odoo 的 `digits=`。两件事钉在这里：
//
//   - 两种写法都认：`digits(16,3)` 固定位数，`digits('Product Unit of Measure')`
//     指向 decimal.precision 的一条用途（位数由业务层解析，orm 只带名字出去）。
//   - **写坏了当没声明**。这个值最终决定界面上一列数字显示几位，一个笔误若被当成
//     0 位，整列金额会静默变成整数——没有报错，只有错的数。
func TestTagDigits(t *testing.T) {
	cases := []struct {
		name      string
		params    []string
		wantVal   []int
		wantUsage string
	}{
		{"固定位数", []string{"16", "3"}, []int{16, 3}, ""},
		{"单个数字当小数位", []string{"3"}, []int{16, 3}, ""},
		{"零位小数是合法声明", []string{"16", "0"}, []int{16, 0}, ""},
		{"用途名（带引号）", []string{"'Product Unit of Measure'"}, nil, "Product Unit of Measure"},
		{"用途名（无引号）", []string{"Account"}, nil, "Account"},
		{"负数当没写", []string{"16", "-1"}, nil, ""},
		{"非数字的第二参当没写", []string{"16", "x"}, nil, ""},
		{"空参数当没写", nil, nil, ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			field := newBaseField("qty")
			ctx := &TTagContext{Field: field, Params: c.params}
			if err := tag_digits(ctx); err != nil {
				t.Fatalf("tag_digits err: %v", err)
			}
			if !reflect.DeepEqual(field.Digits(), c.wantVal) {
				t.Errorf("Digits() = %v, want %v", field.Digits(), c.wantVal)
			}
			if field.DigitsUsage() != c.wantUsage {
				t.Errorf("DigitsUsage() = %q, want %q", field.DigitsUsage(), c.wantUsage)
			}
		})
	}
}

// digits 绝不能渗进 SqlType：`DOUBLE(16,3)` 在 PG 里不是合法 DDL，一旦写进去
// 建表就直接失败。它只是给界面看的。
func TestTagDigits_DoesNotTouchSqlType(t *testing.T) {
	field := newBaseField("amount")
	field.SqlType = SQLType{Double, 0, 0}

	if err := tag_digits(&TTagContext{Field: field, Params: []string{"16", "3"}}); err != nil {
		t.Fatalf("tag_digits err: %v", err)
	}

	if field.SqlType.DefaultLength != 0 || field.SqlType.DefaultLength2 != 0 {
		t.Fatalf("digits 渗进了 SqlType: %+v", field.SqlType)
	}
}

// Attributes 是前端拿到字段元数据的唯一出口：没声明 digits 的字段必须给 nil，
// 前端才知道该按类型回落缺省位数，而不是被一个 [0 0] 逼成整数显示。
func TestFieldAttributes_DigitsAbsentWhenUndeclared(t *testing.T) {
	plain := newBaseField("amount")
	if got := plain.Attributes(&TTagContext{Field: plain})["digits"]; got != nil {
		t.Errorf("未声明的 digits 应为 nil，实得 %v", got)
	}

	declared := newBaseField("qty")
	if err := tag_digits(&TTagContext{Field: declared, Params: []string{"16", "3"}}); err != nil {
		t.Fatalf("tag_digits err: %v", err)
	}
	attrs := declared.Attributes(&TTagContext{Field: declared})
	if !reflect.DeepEqual(attrs["digits"], []int{16, 3}) {
		t.Errorf(`attrs["digits"] = %v, want [16 3]`, attrs["digits"])
	}

	byUsage := newBaseField("price")
	if err := tag_digits(&TTagContext{Field: byUsage, Params: []string{"'Product Price'"}}); err != nil {
		t.Fatalf("tag_digits err: %v", err)
	}
	attrs = byUsage.Attributes(&TTagContext{Field: byUsage})
	if attrs["digits"] != nil {
		t.Errorf(`用途形式不该自带位数，实得 %v`, attrs["digits"])
	}
	if attrs["digits_usage"] != "Product Price" {
		t.Errorf(`attrs["digits_usage"] = %v, want "Product Price"`, attrs["digits_usage"])
	}
}

// min_display_digits 是**下限**，不是定值（Odoo 19 的单价字段全用它）：写 2 位时
// `3.1` 显示成 `3.10`，而 `3.1234` 原样显示成 `3.1234`——不四舍五入掉用户填的位数。
// 与 digits 共两点差异钉在这里：只收一个参数；0 是合法声明（未声明必须是 nil，
// 否则「没写」和「显示成整数」在下游分不开）。
func TestTagMinDisplayDigits(t *testing.T) {
	cases := []struct {
		name      string
		params    []string
		wantVal   *int
		wantUsage string
	}{
		{"固定位数", []string{"2"}, intp(2), ""},
		{"零位是合法声明", []string{"0"}, intp(0), ""},
		{"用途名（带引号）", []string{"'Product Price'"}, nil, "Product Price"},
		{"用途名（无引号）", []string{"Account"}, nil, "Account"},
		{"负数当没写", []string{"-1"}, nil, ""},
		{"二元组写法不认——它没有 precision 这一维", []string{"16", "2"}, nil, ""},
		{"空参数当没写", nil, nil, ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			field := newBaseField("price_unit")
			ctx := &TTagContext{Field: field, Params: c.params}
			if err := tag_min_display_digits(ctx); err != nil {
				t.Fatalf("tag_min_display_digits err: %v", err)
			}
			got := field.MinDisplayDigits()
			switch {
			case c.wantVal == nil && got != nil:
				t.Errorf("MinDisplayDigits() = %d, want nil", *got)
			case c.wantVal != nil && got == nil:
				t.Errorf("MinDisplayDigits() = nil, want %d", *c.wantVal)
			case c.wantVal != nil && *got != *c.wantVal:
				t.Errorf("MinDisplayDigits() = %d, want %d", *got, *c.wantVal)
			}
			if field.MinDisplayDigitsUsage() != c.wantUsage {
				t.Errorf("MinDisplayDigitsUsage() = %q, want %q", field.MinDisplayDigitsUsage(), c.wantUsage)
			}
		})
	}
}

// 同 digits：没声明的字段元数据里不能有这两个键，前端才知道该按类型回落。
// `min_display_digits: 0` 与「没写」是两回事——前者把一列价格显示成整数。
func TestFieldAttributes_MinDisplayDigits(t *testing.T) {
	plain := newBaseField("price")
	attrs := plain.Attributes(&TTagContext{Field: plain})
	if _, ok := attrs["min_display_digits"]; ok {
		t.Errorf("未声明时不该有 min_display_digits 键，实得 %v", attrs["min_display_digits"])
	}
	if _, ok := attrs["min_display_digits_usage"]; ok {
		t.Errorf("未声明时不该有 min_display_digits_usage 键")
	}

	zero := newBaseField("price")
	if err := tag_min_display_digits(&TTagContext{Field: zero, Params: []string{"0"}}); err != nil {
		t.Fatalf("tag err: %v", err)
	}
	if got := zero.Attributes(&TTagContext{Field: zero})["min_display_digits"]; got != 0 {
		t.Errorf(`attrs["min_display_digits"] = %v, want 0`, got)
	}

	byUsage := newBaseField("price_unit")
	if err := tag_min_display_digits(&TTagContext{Field: byUsage, Params: []string{"'Product Price'"}}); err != nil {
		t.Fatalf("tag err: %v", err)
	}
	attrs = byUsage.Attributes(&TTagContext{Field: byUsage})
	if _, ok := attrs["min_display_digits"]; ok {
		t.Errorf("用途形式不该自带位数，实得 %v", attrs["min_display_digits"])
	}
	if attrs["min_display_digits_usage"] != "Product Price" {
		t.Errorf(`attrs["min_display_digits_usage"] = %v, want "Product Price"`, attrs["min_display_digits_usage"])
	}
}

func intp(n int) *int { return &n }

// 走**真实的**标签切分链（splitTag → parseTag），而不是手喂 Params：
// `digits('Product Unit')` 的用途名里带空格，而 splitTag 正是按空格切标签的——
// 引号处理但凡有差池，这里拿到的就是半截名字，之后到 decimal.precision 里必然查不到，
// 表现是「精度配了却不生效」，一路上没有任何报错。
func TestTagDigits_ThroughRealTagSplitter(t *testing.T) {
	cases := []struct {
		tag          string
		wantVal      []int
		wantUsage    string
		wantMinUsage string
	}{
		{"double() digits('Product Unit') title('Quantity')", nil, "Product Unit", ""},
		{"double() digits(16,3) title('Qty')", []int{16, 3}, "", ""},
		{"double() title('Qty')", nil, "", ""},
		// min_display_digits 的用途名同样带空格，切分链上任何差池都会让它查不到精度。
		{"double() min_display_digits('Product Price') title('Unit Price')", nil, "", "Product Price"},
	}

	for _, c := range cases {
		t.Run(c.tag, func(t *testing.T) {
			field := newBaseField("qty")
			for _, item := range splitTag(c.tag) {
				attrs := parseTag(item)
				if len(attrs) == 0 {
					continue
				}
				switch strings.ToLower(attrs[0]) {
				case TAG_DIGITS:
					if err := tag_digits(&TTagContext{Field: field, Params: attrs[1:]}); err != nil {
						t.Fatalf("tag_digits err: %v", err)
					}
				case TAG_MIN_DIGITS:
					if err := tag_min_display_digits(&TTagContext{Field: field, Params: attrs[1:]}); err != nil {
						t.Fatalf("tag_min_display_digits err: %v", err)
					}
				}
			}
			if !reflect.DeepEqual(field.Digits(), c.wantVal) {
				t.Errorf("Digits() = %v, want %v", field.Digits(), c.wantVal)
			}
			if field.DigitsUsage() != c.wantUsage {
				t.Errorf("DigitsUsage() = %q, want %q", field.DigitsUsage(), c.wantUsage)
			}
			if field.MinDisplayDigitsUsage() != c.wantMinUsage {
				t.Errorf("MinDisplayDigitsUsage() = %q, want %q", field.MinDisplayDigitsUsage(), c.wantMinUsage)
			}
		})
	}
}
