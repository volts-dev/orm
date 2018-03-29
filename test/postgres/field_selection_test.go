package postgres

import (
	"testing"
	//"vectors/orm"
	"vectors/orm/test"

	_ "github.com/lib/pq"
)

func Test1(t *testing.T) {
	test.FieldSelection(test.TestOrm, t)
}
