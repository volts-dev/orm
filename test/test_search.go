package test

import (
	"testing"

	"github.com/volts-dev/orm"
)

func TestSearch(title string, t *testing.T) {
	PrintSubject(title, "Search()")
	test_search(test_orm, t)
}

func test_search(o *orm.TOrm, t *testing.T) {
	model, _ := o.GetModel("user_model")
	ids, err := model.Records().Select("*").Search()
	if err != nil {
		t.Fatalf("testing search() failure")
	}

	t.Log(ids)
}
