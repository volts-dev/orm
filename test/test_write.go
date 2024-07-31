package test

import (
	"testing"

	"github.com/volts-dev/orm"
	"github.com/volts-dev/utils"
)

// TODO 无ID
// TODO 带条件和字段
func (self *Testchain) Write(classic ...bool) *Testchain {
	data := new(UserModel)
	*data = *user

	model, err := self.Orm.GetModel("user_model")
	if err != nil {
		self.T.Fatal(err)
	}

	for i := 0; i < 10; i++ {
		data.Name = "Write" + utils.ToString(i)
		data.Title = "Write"

		id, err := model.Records().Create(data)
		if err != nil {
			self.T.Fatalf("Create data failue %d %v", id, err)
		}
	}

	PrintSubject("Write", "Write()")
	test_write(test_orm, self.T)

	PrintSubject("Write", "write by id")
	test_write_by_id(test_orm, self.T)

	return self
}

func TestWrite(title string, t *testing.T) {
	data := new(UserModel)
	*data = *user

	model, err := test_orm.GetModel("user_model")
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 10; i++ {
		data.Name = "Write" + utils.ToString(i)
		data.Title = "Write"

		id, err := model.Records().Create(data)
		if err != nil {
			t.Fatalf("Create data failue %d %v", id, err)
		}
	}

	PrintSubject(title, "Write()")
	test_write(test_orm, t)

	PrintSubject(title, "write by id")
	test_write_by_id(test_orm, t)
}

func test_write(o *orm.TOrm, t *testing.T) {
	title := "Write Tested"

	// query data
	model, err := o.GetModel("user_model")
	if err != nil {
		t.Fatal(err)
	}

	ds, err := model.Records().Where("name=?", "Admin0").Read()
	if err != nil {
		t.Fatal(err)
	}

	// change data
	ds.Record().SetByField("title", title)

	// write data
	effect, err := model.Records().Write(ds.Record().AsMap())
	if err != nil {
		t.Fatal(err)
	}

	if effect != 1 {
		t.Fatalf("Write effected %d", effect)
	}

	ds, err = model.Records().Ids(ds.FieldByName("id").AsString()).Read()
	if ds.FieldByName("title").AsString() != title {
		t.Fatalf("Write data didn't effected!")
	}
}

func test_write_by_id(o *orm.TOrm, t *testing.T) {
	title := "Write Tested"

	model, err := o.GetModel("user_model")
	if err != nil {
		t.Fatal(err)
	}

	data := new(UserModel)
	*data = *user
	data.Title = title
	effect, err := model.Records().Ids(1).Write(data)
	if err != nil {
		t.Fatal(err)
	}

	if effect != 1 {
		t.Fatalf("Write effected %v", effect)
	}

	ds, err := model.Records().Ids(1).Read()
	if ds.FieldByName("title").AsString() != title {
		t.Fatalf("Write data didn't effected!")
	}
}

//
