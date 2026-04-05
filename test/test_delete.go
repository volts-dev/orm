package test

import (
	"testing"

	"github.com/volts-dev/orm"
)

func (self *Testchain) Delete() *Testchain {
	self.PrintSubject("Delete")

	model, err := self.Orm.GetModel("user_model")
	if err != nil {
		self.Fatal(err)
	}

	// get all IDs
	ids, _, err := model.Records().Search()
	if err != nil {
		self.Fatal(err)
	}
	if len(ids) == 0 {
		self.Fatal("Delete: no records to delete")
	}

	beforeCount, err := model.Records().Count()
	if err != nil {
		self.Fatal(err)
	}

	// delete single record by ID
	effect, err := model.Records().Ids(ids[0]).Delete()
	if err != nil {
		self.Fatal(err)
	}
	if effect != 1 {
		self.Fatalf("Delete effected %d records, expected 1", effect)
	}

	afterCount, err := model.Records().Count()
	if err != nil {
		self.Fatal(err)
	}
	if afterCount != beforeCount-1 {
		self.Fatalf("Delete: count mismatch after delete: before=%d after=%d", beforeCount, afterCount)
	}

	return self
}

func TestDelete(title string, t *testing.T) {
	PrintSubject(t, title, "Delete()")
	test_delete(test_orm, t)
}

func test_delete(o *orm.TOrm, t *testing.T) {
}
