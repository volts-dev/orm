package orm

import (
	"path/filepath"
	"strings"
	"testing"
)

// text 字段不限长，三个库都一样（MySQL 兼容 P4，2026-10-25）：MySQL 上 text 曾落 TEXT（64KB），account 的
// view_move_form 的 arch 写入 1406 Data too long，带 account 的租户建不出来。PG 的 text 本来不限长。

type TextBigRec struct {
	TModel `table:"name('text_big_rec')"`
	Id     int64  `field:"pk autoincr"`
	Body   string `field:"text()"`
}

func TestTextField_HoldsMoreThan64KB(t *testing.T) {
	ds := testDataSource(t, filepath.Join(t.TempDir(), "textbig.db"))
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer o.Close()
	if _, err := o.SyncModel("", new(TextBigRec)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	big := strings.Repeat("<field name=\"x\"/>中", 10000) // ~200KB
	ids, err := o.Model("text.big.rec").Create(map[string]any{"body": big})
	if err != nil {
		t.Fatalf("写入 %d 字节的 text: %v", len(big), err)
	}
	m, err := o.GetModel("text.big.rec")
	if err != nil {
		t.Fatal(err)
	}
	ds2, err := m.Records().Ids(ids[0]).Read()
	if err != nil || ds2.Count() != 1 {
		t.Fatalf("读回: %v", err)
	}
	if got := ds2.FieldByName("body").AsString(); got != big {
		t.Errorf("读回 %d 字节，写入的是 %d 字节", len(got), len(big))
	}
}
