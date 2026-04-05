package test

import (
	"testing"

	"github.com/volts-dev/orm"
)

func (self *Testchain) Where() *Testchain {
	self.PrintSubject("Where")

	model, err := self.Orm.GetModel("user_model")
	if err != nil {
		self.Fatal(err)
	}

	// get first record to use its id
	allIds, _, err := model.Records().Search()
	if err != nil || len(allIds) == 0 {
		self.Fatal("Where: no records available")
	}

	// filter by exact id
	ds, err := model.Records().Where("id=?", allIds[0]).Read()
	if err != nil {
		self.Fatal(err)
	}
	if ds.IsEmpty() {
		self.Fatal("Where(id=?) returned empty dataset")
	}

	// filter with AND
	ds, err = model.Records().Where("id=?", allIds[0]).Where("id>?", 0).Read()
	if err != nil {
		self.Fatal(err)
	}
	if ds.IsEmpty() {
		self.Fatal("Where with chained conditions returned empty dataset")
	}

	return self
}

func TestWhere(title string, t *testing.T) {
	PrintSubject(t, title, "read by where")
	test_read_by_where(test_orm, t)

	PrintSubject(t, title, "write by where")
	//test_write_by_where(test_orm, t)
}

func test_read_by_where(o *orm.TOrm, t *testing.T) {
	model, _ := o.GetModel("user_model")
	ds, err := model.Records().Where("id=?", 1).Read()
	if err != nil || ds.Count() < 0 {
		t.Fatal(err)
	}
}

func test_write_by_where(o *orm.TOrm, t *testing.T) {
	model, err := o.GetModel("user_model")
	if err != nil {
		t.Fatal(err)
	}

	var data *UserModel
	*data = *user
	data.Title = "write_by_where"
	effect, err := model.Records().Where("name=?", "Admin1").Write(data)
	if err != nil {
		t.Fatal(err)
		return
	}

	if effect != 1 {
		t.Fatalf("insert not returned 1")
		return
	}

}
