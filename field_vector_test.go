package orm

import (
	"reflect"
	"testing"
)

// 向量值的线上形态与 pgvector 的 input/output 函数一致：`[a,b,c]`。
func TestVector_FormatParseRoundTrip(t *testing.T) {
	in := []float32{0.5, -1.25, 3}
	lit := FormatVector(in)
	if lit != "[0.5,-1.25,3]" {
		t.Fatalf("FormatVector = %v", lit)
	}
	out := ParseVector([]byte(lit.(string)))
	if !reflect.DeepEqual(out, in) {
		t.Fatalf("ParseVector = %#v, want %#v", out, in)
	}
	if FormatVector([]float64{1, 2}) != "[1,2]" {
		t.Fatalf("[]float64 not formatted")
	}
	if FormatVector([]any{1, 2.5}) != "[1,2.5]" {
		t.Fatalf("[]any not formatted")
	}
}

// 空向量必须落 NULL：pgvector 拒绝零维。
func TestVector_EmptyIsNull(t *testing.T) {
	for _, v := range []any{nil, "", []byte{}, []float32{}, []float64{}, []any{}} {
		if got := FormatVector(v); got != nil {
			t.Fatalf("FormatVector(%#v) = %#v, want nil", v, got)
		}
	}
	if ParseVector(nil) != nil || ParseVector("") != nil {
		t.Fatalf("ParseVector of empty should be nil")
	}
}

// 反查回来的 vector(N) 列必须建得出字段，否则下一次启动整个模型加载失败。
func TestVector_ReverseLookupCreatesField(t *testing.T) {
	f, err := NewField("embedding_vector", WithSQLType(SQLType{Vector, 1536, 0}))
	if err != nil {
		t.Fatalf("NewField(vector) err = %v", err)
	}
	if _, ok := f.(*TVectorField); !ok {
		t.Fatalf("NewField(vector) = %T, want *TVectorField", f)
	}
	if _, ok := SqlTypes[Vector]; !ok {
		t.Fatalf("SqlTypes lacks Vector: parsePgColumn would reject the column")
	}
}
