package test

import (
	"testing"
	"volts-dev/orm"
)

func Read(title string, t *testing.T) {
	PrintSubject(title, "Read()")
	read(test_orm, t)

	PrintSubject(title, "Read and convert")
	read_and_convert(test_orm, t)
}

func read(o *orm.TOrm, t *testing.T) {
	model, _ := o.GetModel("user.model")
	// 测试Select 默认所有
	ds, err := model.Records().Read()
	if err != nil {
		panic(err)
	}

	// 测试Select 所有
	ds, err = model.Records().Select("*").Read()
	if err != nil {
		panic(err)
	}

	ds, err = model.Records().Select("id", "name").Read()
	if err != nil {
		panic(err)
	}

	if ds.Count() == 0 {
		t.Fatalf("Read return %d", ds.Count())
	}
}

func read_and_convert(o *orm.TOrm, t *testing.T) {
	model, _ := o.GetModel("user.model")
	ds, err := model.Records().Read()
	if err != nil {
		panic(err)
	}
	user := new(UserModel)
	ds.Record().AsStruct(user)
	if user.Id < 0 {
		t.Fatalf("dataset convert to struct fail")
	}

}
