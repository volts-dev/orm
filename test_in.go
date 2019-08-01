package orm

import (
	//"fmt"
	"testing"
)

func TestIn(title string, t *testing.T) {
	PrintSubject(title, "And()")
	test_in(test_orm, t)

}

func test_in(o *TOrm, t *testing.T) {
	model, err := o.GetModel("user_model")
	if err != nil {
		t.Fatal(err)
	}

	ds, err := model.Records().In("title", "aa").Where("name=?", "中国").Read()
	if err != nil {
		t.Fatal(err)
	}

	if ds.IsEmpty() {
		t.Fatalf("the action Read() return %d!", ds.Count())
	}
}
