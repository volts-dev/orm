package orm

import "testing"

// binary() 字段读回来要是字符串：bytea 交回的 []byte 原样上交，过 JSON 会被再编一遍 base64
// （social.media 的平台图标读成 "UEhOMlp5…"，见 TBinField.onConvertToRead）。
func TestBinFieldReadsBase64TextAsString(t *testing.T) {
	f := &TBinField{}
	var cell any = []byte("PHN2Zy8+")
	if got, ok := f.onConvertToRead(nil, nil, []any{&cell}, 0).(string); !ok || got != "PHN2Zy8+" {
		t.Fatalf("[]byte 应还原成同一串文本，得到 %#v", f.onConvertToRead(nil, nil, []any{&cell}, 0))
	}
	var empty any
	if got := f.onConvertToRead(nil, nil, []any{&empty}, 0); got != nil {
		t.Fatalf("空值应保持 nil，得到 %#v", got)
	}
	var s any = "QUJD"
	if got := f.onConvertToRead(nil, nil, []any{&s}, 0); got != "QUJD" {
		t.Fatalf("已经是字符串的原样返回，得到 %#v", got)
	}
}
