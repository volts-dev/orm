package orm

import (
	"testing"
)

func TestSearch(title string, t *testing.T) {
	PrintSubject(title, "Search()")
	test_search(test_orm, t)
}

func test_search(o *TOrm, t *testing.T) {
	model, _ := o.GetModel("user.model")
	ids, err := model.Records().Select("*").Search()
	if err != nil {
		t.Fatalf("testing search() failure")
	}

	t.Log(ids)
}
