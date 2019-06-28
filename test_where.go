package orm

import (
	//"fmt"
	"testing"
)

func TestWhere(title string, o TOrm, t *testing.T) {
	PrintSubject(title, "read by where")
	test_read_by_where(o, t)

	PrintSubject(title, "write by where")
	test_write_by_where(o, t)
}

func test_read_by_where(o TOrm, t *testing.T) {
	model, _ := o.GetModel("user.model")
	ds, err := model.Records().Where("id=?", 1).Read()
	if err != nil || ds.Count() < 0 {
		t.Fatal(err)
	}
}

func test_write_by_where(orm TOrm, t *testing.T) {
	model, _ := orm.GetModel("user.model")
	var data *UserModel
	*data = *user
	data.Title = "write_by_where"
	effect, err := model.Records().Where("name=?", "Admin1").Write(data)
	if err != nil {
		t.Error(err)
		return
	}

	if effect != 1 {
		t.Error("insert not returned 1")
		return
	}

}
