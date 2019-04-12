package test

import (
	"testing"
	"volts-dev/orm"
)

func Search(title string, t *testing.T) {
	PrintSubject(title, "Search()")
	search(test_orm, t)
}

func search(o *orm.TOrm, t *testing.T) {
	model, _ := o.GetModel("user.model")
	ids := model.Records().Select("*").Search()
	if len(ids) == 0 {
		t.Fatalf("testing search() failure")
	}
}
