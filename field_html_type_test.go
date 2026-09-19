package orm

import "testing"

// html 字段：对外报 html（前端据此用富文本编辑器），列仍是 TEXT；按 typeName 归类的条件生成仍当文本。
func TestHtmlField_TypeNameIsHtmlColumnIsText(t *testing.T) {
	fld := newHtmlField()
	base := fld.Base()
	fld.Init(&TTagContext{Field: fld})
	if got := base.TypeName(); got != TYPE_HTML {
		t.Fatalf("TypeName() = %q, want %q", got, TYPE_HTML)
	}
	if got := base.SqlType.Name; got != Text {
		t.Fatalf("SqlType.Name = %q, want %q（列类型不能变）", got, Text)
	}
	if !base.store {
		t.Fatalf("html 字段必须落库（早先缺 Init 时静默不落库）")
	}
	// = False 在 text 类列上是「为空或空串」：html 必须同样处理
	if _, ok := isFalsyValue(TYPE_HTML, "t", "body"); !ok {
		t.Fatalf("isFalsyValue(html) 应该认得（按文本归类）")
	}
	sqlText, _ := isFalsyValue(Text, "t", "body")
	sqlHtml, _ := isFalsyValue(TYPE_HTML, "t", "body")
	if sqlText != sqlHtml {
		t.Fatalf("html 与 text 的 = False 条件不一致：\n text: %s\n html: %s", sqlText, sqlHtml)
	}
}
