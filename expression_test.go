package orm

import (
	"fmt"
	"testing"

	"github.com/volts-dev/orm/domain"
)

func TestExpression(t *testing.T) {
	test_leaf_1(t)
}

func test_leaf_1(t *testing.T) {
	var err error
	qry := `[('id', 'in', [1])]`
	fmt.Println(qry)

	node, err := domain.String2Domain(qry)
	if err != nil {
		t.Fatal(err)
	}
	domain.PrintDomain(node)

	node, err = normalize_domain(node)
	if err != nil {
		t.Fatal(err)
	}
	domain.PrintDomain(node)

	node = distribute_not(node)
	domain.PrintDomain(node)
}
