package test

import (
	"testing"

	"github.com/volts-dev/orm"
	"github.com/volts-dev/utils"
)

func (self *Testchain) Create() *Testchain {
	self.PrintSubject("Create()")
	data := new(UserModel)
	*data = *user
	ss := self.orm.NewSession()
	ss.Begin()
	for i := 0; i < 10; i++ {
		data.Name = "Create" + utils.IntToStr(i)

		// Call the API Create()
		id, err := ss.Model("user_model").Create(data)
		if err != nil {
			self.Fatalf("create record failure with error < %s >", err.Error())
		}

		if id == nil {
			self.Fatal("creation didn't returnning Id!")
		}
	}

	err := ss.Commit()
	if err != nil {
		self.Log(err)

		e := ss.Rollback()
		if e != nil {
			self.Fatal(e)
		}
	}

	ss.Close()

	return self
}

func TestCreate(title string, t *testing.T) {
	PrintSubject(title, "Create()")
	test_create(test_orm, t)

	PrintSubject(title, "Create Relate")
	test_create_relate(test_orm, t)

	PrintSubject(title, "Create ManyToMany")
	test_create_m2m(test_orm, t)
	return
}

func TestCreate10(title string, t *testing.T) {
	PrintSubject(title, "Create() 10 records")
	test_create(test_orm, t)
}

// Test the model create a record by an object
func test_create(o *orm.TOrm, t *testing.T) {
	data := new(UserModel)
	*data = *user
	ss := o.NewSession()
	ss.Begin()
	for i := 0; i < 10; i++ {
		data.Name = "Create" + utils.IntToStr(i)

		// Call the API Create()
		id, err := ss.Model("user_model").Create(data)
		if err != nil {
			t.Fatalf("create data failure %d %s", id, err.Error())
		}

		if id == nil {
			t.Fatal("created not returned id")
			return
		}
	}

	err := ss.Commit()
	if err != nil {
		t.Log(err)

		e := ss.Rollback()
		if e != nil {
			t.Fatal(e)
		}
	}

	ss.Close()
}

func test_create_relate(o *orm.TOrm, t *testing.T) {
	data := new(CompanyModel)
	*data = *company

	model, err := o.GetModel("company_model")
	if err != nil {
		t.Fatal(err)
		return
	}

	id, err := model.Records().Create(data)
	if err != nil {
		t.Fatal(err)
		return
	}

	if id == nil {
		t.Fatal("create_with_relate not returned id")
		return
	}

	partner, err := o.GetModel("partner_model")
	if err != nil {
		t.Fatal(err)
	}

	_, err = partner.Records().Ids(utils.IntToStr(id)).Read()
	if err != nil {
		t.Fatal(err)
	}

	//	if ds.FieldByName("name").AsString() != data.Name && ds.FieldByName("homepage").AsString() != data.Homepage {
	//		t.Fatal("the value should be equal to the insert data")
	//	}
}

func test_create_m2m(o *orm.TOrm, t *testing.T) {
	model, _ := o.GetModel("user_model")
	ds, err := model.Records().Read()
	if err != nil {
		t.Fatalf("manyTomany read fail %v", err)
		return
	}

	ds.FieldByName("many_to_many").AsInterface([]interface{}{1, 2})
	cnt, err := model.Records().Ids(ds.FieldByName("id").AsInterface()).Write(ds.Record().AsItfMap(), true)
	if err != nil || cnt == 0 {
		t.Fatalf("create manyTomany fail: %v", err)
		return
	}
}
