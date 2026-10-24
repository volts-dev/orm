package orm

import (
	"path/filepath"
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

// MySQL 上向量列落 JSON（MySQL 8.0 没有向量类型）：读写原样往返、重启不发 DDL。PG 要 pgvector 扩展，
// docker 里的测试 PG 没装，这里不跑。
type VecRec struct {
	TModel `table:"name('vec_rec')"`
	Id     int64     `field:"pk autoincr"`
	Emb    []float32 `field:"vector() size(3)"`
}

func TestVector_RoundTripWithoutPgvector(t *testing.T) {
	skipUnlessDialect(t, "mysql", "sqlite")
	ds := testDataSource(t, filepath.Join(t.TempDir(), "vec.db"))
	for round := 0; round < 2; round++ {
		o, err := New(WithDataSource(ds))
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		rec := &ddlRecorder{}
		o.AddHook(rec)
		if _, err := o.SyncModel("", new(VecRec)); err != nil {
			t.Fatalf("SyncModel: %v", err)
		}
		if round == 1 && len(rec.ddl) > 0 {
			t.Errorf("重启后仍发 DDL：%q", rec.ddl)
		}
		if round == 0 {
			ids, err := o.Model("vec.rec").Create(map[string]any{"emb": []float32{0.5, -1, 2.25}})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			ds2, err := o.Model("vec.rec").Ids(ids...).Read()
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			got, ok := ds2.FieldByName("emb").AsInterface().([]float32)
			if !ok || len(got) != 3 || got[0] != 0.5 || got[1] != -1 || got[2] != 2.25 {
				t.Errorf("读回 %#v", ds2.FieldByName("emb").AsInterface())
			}
		}
		o.Close()
	}
}
