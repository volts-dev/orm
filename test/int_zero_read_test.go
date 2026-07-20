package test

import (
	"testing"

	"github.com/volts-dev/orm"
	_ "modernc.org/sqlite"
)

// TestIntZeroNotBlankedOnRead 走**真实读路径**验证：BigNumberToString 打开时，
// 普通 int64 数据列的 0 必须原样读回 "0"，只有外键列的 0 才归成空串。
//
// 回归背景：大数列此前与真字符列共用 converter(Varchar)，那里有一句无差别的
// `if v == "0" { return "" }`。后果是任何整数列只要值是 0，API 读出来就是空串——
// DB 里 NOT NULL 明明存着 0，调用方却分不清「这个字段是 0」还是「这个字段没设」。
// 真栈复现：pro.tag 的 sequence=0 / color=0 读回 ""，而 -1 和 5 都正常。
func TestIntZeroNotBlankedOnRead(t *testing.T) {
	ds := &orm.TDataSource{DbType: "sqlite", DbName: ":memory:"}
	o, err := orm.New(orm.WithDataSource(ds), orm.WithBigNumberToString(true))
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()

	type IntZeroModel struct {
		orm.TModel `table:"name('int_zero_model')"`
		Id         int64 `field:"pk autoincr"`
		Sequence   int64 `field:"bigint"`
		Color      int64 `field:"bigint"`
	}

	if _, err = o.SyncModel("test", new(IntZeroModel)); err != nil {
		t.Fatal(err)
	}
	model, err := o.GetModel("int_zero_model")
	if err != nil {
		t.Fatal(err)
	}

	for _, seq := range []int64{0, 5, -1} {
		if _, err := model.Records().Create(map[string]any{
			"sequence": seq,
			"color":    int64(0),
		}); err != nil {
			t.Fatalf("create sequence=%d: %v", seq, err)
		}
	}

	res, err := model.Records().OrderBy("id").Read()
	if err != nil {
		t.Fatal(err)
	}
	if res.Count() != 3 {
		t.Fatalf("expected 3 records, got %d", res.Count())
	}

	want := []string{"0", "5", "-1"}
	res.First()
	for i := 0; !res.Eof(); i++ {
		m := res.Record().AsMap()
		if got := m["sequence"]; got != want[i] {
			t.Errorf("record %d: sequence = %#v, want %q（0 被归空说明回归了）", i, got, want[i])
		}
		// color 恒为 0：它是普通数据列，不是外键，同样必须读回 "0"
		if got := m["color"]; got != "0" {
			t.Errorf("record %d: color = %#v, want \"0\"", i, got)
		}
		res.Next()
	}
}
