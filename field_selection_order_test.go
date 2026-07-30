package orm

import (
	"reflect"
	"testing"
)

// TestParseSelectionJSON_PreservesOrder 钉住 selection 选项的**书写顺序**。
//
// 原实现是 json.Unmarshal 进 map 再 range，Go 的 map 迭代顺序是故意随机化的：
// 同一个字段每次进程启动的选项顺序都不同。statusbar 直接按这个顺序画流水线，
// 销售单状态栏因此渲成「Sales Order → Quotation → Quotation Sent」，刷新一次
// 还会换个顺序。全仓 126 个 selection 字段都用这种对象写法。
//
// 循环跑多次：单次通过有可能是 map 随机序恰好撞对了。
func TestParseSelectionJSON_PreservesOrder(t *testing.T) {
	const src = `{"draft":"Quotation","sent":"Quotation Sent","sale":"Sales Order","cancel":"Cancelled"}`
	want := [][]string{
		{"draft", "Quotation"},
		{"sent", "Quotation Sent"},
		{"sale", "Sales Order"},
		{"cancel", "Cancelled"},
	}

	for i := 0; i < 50; i++ {
		got, err := parseSelectionJSON(src)
		if err != nil {
			t.Fatalf("parseSelectionJSON err: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("第 %d 次解析顺序不对\n got: %v\nwant: %v", i, got, want)
		}
	}
}

// 键里带空格（purchase 的 "to approve"）与标签里带非 ASCII 都不能破坏顺序或内容。
func TestParseSelectionJSON_KeysWithSpacesAndUnicode(t *testing.T) {
	got, err := parseSelectionJSON(`{"to approve":"To Approve","done":"已完成"}`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := [][]string{{"to approve", "To Approve"}, {"done", "已完成"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// 坏输入必须报错而不是悄悄给出半份选项——半份 selection 会让某些状态在界面上
// 根本不存在，比启动失败难查得多。
func TestParseSelectionJSON_RejectsBadInput(t *testing.T) {
	for _, src := range []string{
		`[["draft","Draft"]]`,          // 数组不是对象
		`{"draft":"Draft"`,             // 截断
		`{"draft":1}`,                  // 标签不是字符串
		`{"draft":"Draft"} trailing`,   // 尾部有垃圾
		``,                             // 空
	} {
		if _, err := parseSelectionJSON(src); err == nil {
			t.Errorf("parseSelectionJSON(%q) 应当报错", src)
		}
	}
}
