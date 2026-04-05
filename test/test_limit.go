package test

import (
	"testing"
)

func (self *Testchain) Limit() *Testchain {
	self.PrintSubject("Limit")

	model, err := self.Orm.GetModel("user_model")
	if err != nil {
		self.Fatal(err)
	}

	// total count
	total, err := model.Records().Count()
	if err != nil {
		self.Fatal(err)
	}

	// Limit(3) should return at most 3 records
	ds, err := model.Records().Limit(3).Read()
	if err != nil {
		self.Fatal(err)
	}
	if ds.Count() > 3 {
		self.Fatalf("Limit(3) returned %d records", ds.Count())
	}

	// Limit with offset
	if total > 3 {
		ds2, err := model.Records().Limit(3, 1).Read()
		if err != nil {
			self.Fatal(err)
		}
		if ds2.Count() > 3 {
			self.Fatalf("Limit(3,1) returned %d records", ds2.Count())
		}
	}

	return self
}

func TestLimit(title string, t *testing.T) {

}
