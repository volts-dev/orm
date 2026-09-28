package orm

import "testing"

// DeleteRequest 只给 Domain：只删匹配的行。此前 Domain 被无视，删掉的是整张表（租户内）。
func TestModelDelete_HonorsDomain(t *testing.T) {
	o := setupIntegrationOrm(t)
	for _, n := range []string{"a", "b", "c"} {
		if _, err := o.Model("bench.model").Create(map[string]any{"name": n}); err != nil {
			t.Fatal(err)
		}
	}
	m, err := o.GetModel("bench.model")
	if err != nil {
		t.Fatal(err)
	}
	cnt, err := m.Delete(&DeleteRequest{Domain: `[('name','=','b')]`})
	if err != nil {
		t.Fatalf("delete by domain: %v", err)
	}
	ds, err := o.Model("bench.model").OrderBy("name").Limit(-1).Read()
	if err != nil {
		t.Fatal(err)
	}
	var left []string
	for ds.First(); !ds.Eof(); ds.Next() {
		left = append(left, ds.FieldByName("name").AsString())
	}
	if cnt != 1 || len(left) != 2 || left[0] != "a" || left[1] != "c" {
		t.Fatalf("按条件只该删 b：删了 %d 行，剩 %v", cnt, left)
	}
}
