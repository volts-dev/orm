package test

import (
	"testing"

	_ "modernc.org/sqlite"
	"github.com/volts-dev/orm"
)

func TestBigNumberAs(t *testing.T) {
	ds := &orm.TDataSource{
		DbType: "sqlite",
		DbName: ":memory:",
	}

	// Initialize with BigNumberAs set to "string"
	o, err := orm.New(orm.WithDataSource(ds), orm.WithBigNumberToString(true))
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()

	type BigNumModel struct {
		orm.TModel `table:"name('bignum_model')"`
		Id         int64 `field:"pk autoincr"`
		ValBigInt  int64 `field:"bigint"`
	}

	modelObj := new(BigNumModel)
	_, err = o.SyncModel("test", modelObj)
	if err != nil {
		t.Fatal(err)
	}

	model, err := o.GetModel("bignum_model")
	if err != nil {
		t.Fatal(err)
	}

	session := model.Records()
	_, err = session.Create(map[string]any{
		"val_big_int": int64(987654321012345),
	})
	if err != nil {
		t.Fatal(err)
	}

	dsResult, err := model.Records().Read()
	if err != nil {
		t.Fatal(err)
	}

	if dsResult.Count() == 0 {
		t.Fatal("Expected 1 record")
	}

	// Verify that the retrieved type in AsMap is string
	recordMap := dsResult.Record().AsMap()
	valBigIntRaw := recordMap["val_big_int"]
	if _, ok := valBigIntRaw.(string); !ok {
		t.Fatalf("Expected val_big_int to be string, but got %T (%v)", valBigIntRaw, valBigIntRaw)
	}
	if valBigIntRaw != "987654321012345" {
		t.Fatalf("Expected val_big_int to be '987654321012345', but got %v", valBigIntRaw)
	}
}
