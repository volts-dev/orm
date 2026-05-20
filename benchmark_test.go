package orm

import (
	"testing"

	_ "modernc.org/sqlite"
)

// BenchModel covers Phase 3 perf target field types.
// table tag uses table_name('...') syntax; field tags use pk/autoincr/index/varchar/int.
type BenchModel struct {
	TModel `table:"name('bench_model')"`
	Id     int64  `field:"pk autoincr title('ID')"`
	Name   string `field:"varchar() size(64) index"`
	Age    int    `field:"int()"`
}

func setupBenchOrm(b *testing.B) *TOrm {
	b.Helper()
	ds := &TDataSource{DbType: "sqlite", DbName: ":memory:"}
	o, err := New(WithDataSource(ds))
	if err != nil {
		b.Fatalf("orm.New failed: %v", err)
	}
	if _, err := o.SyncModel("", new(BenchModel)); err != nil {
		b.Fatalf("SyncModel failed: %v", err)
	}
	return o
}

func seedBenchRows(b *testing.B, o *TOrm, n int) {
	b.Helper()
	for i := 0; i < n; i++ {
		_, err := o.Model("bench.model").Create(map[string]any{
			"name": "seed",
			"age":  i,
		})
		if err != nil {
			b.Fatalf("seed Create failed at i=%d: %v", i, err)
		}
	}
}

func BenchmarkCreate_Single(b *testing.B) {
	o := setupBenchOrm(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := o.Model("bench.model").Create(map[string]any{
			"name": "alice",
			"age":  30,
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCreate_Batch_100(b *testing.B) {
	o := setupBenchOrm(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 100; j++ {
			_, err := o.Model("bench.model").Create(map[string]any{
				"name": "alice",
				"age":  j,
			})
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkRead_OneById(b *testing.B) {
	o := setupBenchOrm(b)
	seedBenchRows(b, o, 1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := o.Model("bench.model").Ids(int64((i % 1000) + 1)).Read()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRead_All_100(b *testing.B) {
	o := setupBenchOrm(b)
	seedBenchRows(b, o, 1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := o.Model("bench.model").Limit(100).Read()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWrite_Single(b *testing.B) {
	o := setupBenchOrm(b)
	seedBenchRows(b, o, 1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := o.Model("bench.model").Ids(int64((i % 1000) + 1)).Write(map[string]any{
			"age": i % 100,
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDelete_Single(b *testing.B) {
	o := setupBenchOrm(b)
	seedBenchRows(b, o, b.N+10)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := o.Model("bench.model").Delete(int64(i + 1))
		if err != nil {
			b.Fatal(err)
		}
	}
}
