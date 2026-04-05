package test

import (
	"fmt"
	"testing"

	"github.com/volts-dev/orm"
)

func (self *Testchain) Domain() *Testchain {
	self.PrintSubject("Domain")

	model, err := self.Orm.GetModel("user_model")
	if err != nil {
		self.Fatal(err)
	}

	// domain string: all records with id > 0
	ds, err := model.Records().Domain(`[('id', '>', 0)]`).Read()
	if err != nil {
		self.Fatal(err)
	}
	if ds.IsEmpty() {
		self.Fatal("Domain([id > 0]) returned empty dataset")
	}

	// domain with IN operator
	ids, _, err := model.Records().Search()
	if err != nil || len(ids) < 2 {
		return self
	}
	ds2, err := model.Records().Domain(`[('id', 'in', [` +
		idListString(ids[:2]) + `])]`).Read()
	if err != nil {
		self.Fatal(err)
	}
	if ds2.Count() > 2 {
		self.Fatalf("Domain with in returned %d records, expected <= 2", ds2.Count())
	}

	return self
}

// idListString converts first two ids to a comma-separated string for domain expressions
func idListString(ids []interface{}) string {
	result := ""
	for i, id := range ids {
		if i > 0 {
			result += ","
		}
		result += fmt.Sprint(id)
	}
	return result
}

func TestDomain(title string, o *orm.TOrm, t *testing.T) {
	PrintSubject(t, title, "Read By Domain")
	test_read_by_domain(o, t)
}

//# test domain to find out data
func test_read_by_domain(o *orm.TOrm, t *testing.T) {
	domain := `[('id', 'in', [1,6])]`

	model, _ := o.GetModel("user_model")
	ds, err := model.Records().Domain(domain).Read()
	if err != nil {
		panic(err)
	}

	if ds.Count() < 1 {
		t.Fatalf("domain query return 0 record")
	}
}
