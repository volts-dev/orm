package orm

import "testing"

// 导入/导出按字段取选项表时曾只传 Field 不传 Model：`selection(GetXxx)` 型字段
// 在 Attributes 里对零值 reflect.Value 调 MethodByName，整个请求 panic 成 500
// （2026-09-17，mrp.bom 导入）。没有 Model 时必须退回静态选项表，而不是崩。
func TestSelectionField_AttributesWithoutModelDoesNotPanic(t *testing.T) {
	f := newSelectionField().(*TSelectionField)
	f.getterMethod = "GetTypeSelection"
	f.selection = [][]string{{"normal", "Normal"}}

	attrs := f.Attributes(&TTagContext{Field: f})

	sel, _ := attrs["selection"].([][]string)
	if len(sel) != 1 || sel[0][0] != "normal" {
		t.Fatalf("selection = %v, want the static fallback", attrs["selection"])
	}
}
