package orm

import (
	"fmt"
	"testing"
)

func TestExpression(t *testing.T) {
	test_leaf_1(t)
}

func test_leaf_1(t *testing.T) {
	var err error
	qry := `[('id', 'in', [1])]`
	fmt.Println(qry)

	domain, err := String2Domain(qry)
	if err != nil {
		t.Fatal(err)
	}
	PrintDomain(domain)

	domain, err = normalize_domain(domain)
	if err != nil {
		t.Fatal(err)
	}
	PrintDomain(domain)

	domain = distribute_not(domain)
	PrintDomain(domain)
}
