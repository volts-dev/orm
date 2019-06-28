package orm

import (
	"testing"
)

func TestRead(title string, t *testing.T) {
	PrintSubject(title, "Read()")
	test_read(test_orm, t)

	PrintSubject(title, "Read and convert")
	test_read_and_convert(test_orm, t)
}

func test_read(o *TOrm, t *testing.T) {
	model, err := o.GetModel("user_model")
	if err != nil {
		t.Fatal(err)
	}
	// 测试Select 默认所有
	ds, err := model.Records().Read()
	if err != nil {
		t.Fatal(err)
	}

	// 测试Select 所有
	ds, err = model.Records().Select("*").Read()
	if err != nil {
		t.Fatal(err)
	}

	ds, err = model.Records().Select("id", "name").Read()
	if err != nil {
		t.Fatal(err)
	}

	if ds.Count() == 0 {
		t.Fatalf("Read return %d", ds.Count())
	}
}

func test_read_and_convert(o *TOrm, t *testing.T) {
	model, _ := o.GetModel("user_model")
	ds, err := model.Records().Read()
	if err != nil {
		t.Fatal(err)
	}
	user := new(UserModel)
	ds.Record().AsStruct(user)
	if user.Id < 0 {
		t.Fatalf("dataset convert to struct fail")
	}

}
