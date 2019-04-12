package test

import (
	//"fmt"
	"testing"
	"volts-dev/orm"
)

func Where(title string, o *orm.TOrm, t *testing.T) {
	PrintSubject(title, "read by where")
	read_by_where(o, t)

	PrintSubject(title, "write by where")
	write_by_where(o, t)
}

func read_by_where(o *orm.TOrm, t *testing.T) {
	model, _ := o.GetModel("user.model")
	ds, err := model.Records().Where("id=?", 1).Read()
	if err != nil || ds.Count() < 0 {
		t.Fatal(err)
	}
}

func write_by_where(orm *orm.TOrm, t *testing.T) {
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
