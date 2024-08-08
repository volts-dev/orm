package test

import (
	"testing"

	"github.com/bwmarrin/snowflake"
	"github.com/volts-dev/orm"
)

var uuid *snowflake.Node

// TODO 无ID
// TODO 带条件和字段
func (self *Testchain) Write(classic ...bool) *Testchain {
	TestWrite(self.Orm, self.T)
	return self
}

func TestWrite(o *orm.TOrm, t *testing.T) {
	data := new(UserModel)
	*data = *user

	model, err := o.GetModel("user_model")
	if err != nil {
		t.Fatal(err)
	}

	uuid, err = snowflake.NewNode(1)
	if err != nil {
		t.Fatal(err)
	}

	ids := make([]any, 0)
	for i := 0; i < 10; i++ {
		uid := uuid.Generate()
		data.Name = "Write" + uid.String()
		data.Title = "Write"

		id, err := model.Records().Create(data)
		if err != nil {
			t.Fatalf("Create data failue %v %v", id, err)
		}
		ids = append(ids, id)

	}

	PrintSubject("Write", "Write()")
	test_write(ids, o, t)

	PrintSubject("Write", "write by id")
	test_write_by_id(ids, o, t)
}

func test_write(ids []any, o *orm.TOrm, t *testing.T) {
	title := "Write Tested"

	// query data
	model, err := o.GetModel("user_model")
	if err != nil {
		t.Fatal(err)
	}

	// change data
	data := map[string]any{
		"title": title,
	}

	// write data
	effect, err := model.Records().Ids(ids...).Write(data)
	if err != nil {
		t.Fatal(err)
	}

	if effect != 1 {
		t.Fatalf("Write effected %d", effect)
	}
}

func test_write_by_id(ids []any, o *orm.TOrm, t *testing.T) {
	title := "Write Tested"

	model, err := o.GetModel("user_model")
	if err != nil {
		t.Fatal(err)
	}

	data := new(UserModel)
	*data = *user
	data.Title = title
	effect, err := model.Records().Ids(ids).Write(data)
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
