package test

import (
	"testing"
	"volts-dev/orm"
	"volts-dev/utils"
)

var (
	id  int64
	err error
)

//TODO 无ID
//TODO 带条件和字段
func Create(title string, t *testing.T) {
	PrintSubject(title, "Create()")
	create(test_orm, t)

	PrintSubject(title, "Create Relate")
	create_relate(test_orm, t)

	PrintSubject(title, "Create ManyToMany")
	create_m2m(test_orm, t)
}

// test the model create a record by an object
func create(o *orm.TOrm, t *testing.T) {
	data := new(UserModel)
	*data = *user
	ss := o.NewSession()
	for i := 0; i < 10; i++ {
		data.Name = "Create" + utils.IntToStr(i)
		id, err = ss.Model("user.model").Create(data)
		if err != nil {
			e := ss.Rollback()
			if e != nil {
				t.Fatal(e)
			}

			t.Fatalf("create data failue %d %s", id, err.Error())
		}

		if id < 0 {
			t.Fatal("created not returned id")
			return
		}
	}

	err = ss.Commit()
	if err != nil {
		e := ss.Rollback()
		if e != nil {
			t.Fatal(e)
		}

		t.Fatal(err)
	}
}

func create_relate(o *orm.TOrm, t *testing.T) {
	data := new(CompanyModel)
	*data = *company

	model, _ := o.GetModel("company.model")
	id, err := model.Records().Create(data)
	if err != nil {
		t.Error(err)
	}
	if id < 0 {
		t.Error("create_with_relate not returned id")
		return
	}

	partner, _ := o.GetModel("partner.model")
	ds, err := partner.Records().Ids(utils.IntToStr(id)).Read()
	if err != nil {
		t.Error(err)
	}

	if ds.FieldByName("name").AsString() != data.Name && ds.FieldByName("homepage").AsString() != data.Homepage {
		t.Errorf("the value should be equal to the insert data")
	}
}

func create_m2m(o *orm.TOrm, t *testing.T) {
	model, _ := o.GetModel("user.model")
	ds, err := model.Records().Ids("1").Read()
	if err != nil {

	}

	ds.FieldByName("many_to_many").AsInterface([]int64{1, 2})
	cnt, err := model.Records().Ids(ds.FieldByName("id").AsString()).Write(ds.Record().AsItfMap(), true)
	if err != nil || cnt == 0 {
		t.Fatalf("write manyTomany fail %v", err)
	}
}
