package orm

import (
	//"fmt"
	"testing"
)

func TestDomain(title string, o *TOrm, t *testing.T) {
	PrintSubject(title, "Read By Domain")
	test_read_by_domain(o, t)
}

//# test domain to find out data
func test_read_by_domain(o *TOrm, t *testing.T) {
	domain := `[('id', 'in', [1,6])]`

	model, _ := o.GetModel("user.model")
	ds, err := model.Records().Domain(domain).Read()
	if err != nil {
		panic(err)
	}

	if ds.Count() < 1 {
		t.Fatalf("domain query return 0 record")
	}
}
