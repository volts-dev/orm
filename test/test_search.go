package test

import (
	"testing"

	"github.com/volts-dev/orm"
)

func (self *Testchain) Search() *Testchain {
	self.PrintSubject("Search")

	model, err := self.Orm.GetModel("user_model")
	if err != nil {
		self.Fatal(err)
	}

	// search all
	ids, total, err := model.Records().Select("*").Search()
	if err != nil {
		self.Fatal(err)
	}
	if len(ids) == 0 {
		self.Fatal("Search() returned no IDs")
	}

	if len(ids) != int(total) {
		self.Fatal("Search() returned wrong total")
	}

	// search with where filter
	ids2, _, err := model.Records().Where("id=?", ids[0]).Search()
	if err != nil {
		self.Fatal(err)
	}
	if len(ids2) != 1 {
		self.Fatalf("Search with Where returned %d records, expected 1", len(ids2))
	}

	// search with limit
	ids3, _, err := model.Records().Limit(3).Search()
	if err != nil {
		self.Fatal(err)
	}
	if len(ids3) > 3 {
		self.Fatalf("Search with Limit(3) returned %d records", len(ids3))
	}

	return self
}

func TestSearch(title string, t *testing.T) {
	PrintSubject(t, title, "Search()")
	test_search(test_orm, t)
}

func test_search(o *orm.TOrm, t *testing.T) {
	model, _ := o.GetModel("user_model")
	ids, _, err := model.Records().Select("*").Search()
	if err != nil {
		t.Fatalf("testing search() failure")
	}

	t.Log(ids)
}
