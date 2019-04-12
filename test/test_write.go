package test

import (
	"testing"
	"volts-dev/orm"
	"volts-dev/utils"
)

//TODO 无ID
//TODO 带条件和字段

func Write(title string, t *testing.T) {
	data := new(UserModel)
	*data = *user
	model, _ := test_orm.GetModel("user.model")
	for i := 0; i < 10; i++ {
		data.Name = "write" + utils.IntToStr(i)
		data.Title = "write"
		id, err = model.Records().Create(data)
		if err != nil {
			t.Fatalf("create data failue %d %s", id, err.Error())
		}
	}

	PrintSubject(title, "Write()")
	write(test_orm, t)

	PrintSubject(title, "write by id")
	write_by_id(test_orm, t)
}

func write(o *orm.TOrm, t *testing.T) {
	// query data
	model, _ := o.GetModel("user.model")
	ds, err := model.Records().Where("name=?", "Admin0").Read()

	// change data
	ds.FieldByName("title").AsString("Write Tested")

	// write data
	effect, err := model.Records().Write(ds.Record().AsItfMap())
	if err != nil {
		t.Fatal(err.Error())
	}

	if effect != 1 {
		t.Fatalf("write effected %i", effect)
	}

	ds, err = model.Records().Ids(ds.FieldByName("id").AsString()).Read()
	if ds.FieldByName("title").AsString() != "write tested" {
		t.Fatalf("write data didn't effected!")
	}

}

func write_by_id(o *orm.TOrm, t *testing.T) {
	model, _ := o.GetModel("user.model")
	data := new(UserModel)
	*data = *user
	data.Title = "write by id"
	effect, err := model.Records().Ids("1").Write(data)
	if err != nil {
		t.Fatal(err.Error())
	}

	if effect != 1 {
		t.Fatalf("write effected %i", effect)
	}

	ds, err := model.Records().Ids(utils.IntToStr(data.Id)).Read()
	if ds.FieldByName("title").AsString() != "write by id" {
		t.Fatalf("write data didn't effected!")
	}
}

//
