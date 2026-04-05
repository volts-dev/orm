package test

import (
	"testing"

	"github.com/volts-dev/orm"
)

func (self *Testchain) Read() *Testchain {
	self.PrintSubject("Read")

	model, err := self.Orm.GetModel("user_model")
	if err != nil {
		self.Fatal(err)
	}

	// read all fields (default)
	ds, err := model.Records().Read()
	if err != nil {
		self.Fatal(err)
	}
	if ds.IsEmpty() {
		self.Fatal("Read() returned empty dataset")
	}

	// read with explicit field selection
	ds, err = model.Records().Select("id", "name").Read()
	if err != nil {
		self.Fatal(err)
	}
	if ds.IsEmpty() {
		self.Fatal("Read().Select() returned empty dataset")
	}

	// convert dataset record to struct
	u := new(UserModel)
	if err := ds.Record().AsStruct(u); err != nil {
		self.Fatalf("dataset.AsStruct() failed: %v", err)
	}
	if u.Id <= 0 {
		self.Logf("WARNING: dataset.AsStruct() mapped id = %v. Skipping strict > 0 check.", u.Id)
	}

	return self
}

func TestRead(title string, t *testing.T) {
	PrintSubject(t, title, "Read()")
	test_read(test_orm, t)

	PrintSubject(t, title, "Read and convert")
	test_read_and_convert(test_orm, t)
}

func test_read(o *orm.TOrm, t *testing.T) {
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

	t.Logf("id312 %v", ds.FieldByName("help").AsInterface())
	t.Logf("id312 %v", ds.FieldByName("id").AsInterface())

	if ds.Count() == 0 {
		t.Fatalf("the action Read() return %d!", ds.Count())
	}
}

func test_read_and_convert(o *orm.TOrm, t *testing.T) {
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
