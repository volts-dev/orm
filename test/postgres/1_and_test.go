package postgres

import (
	"testing"
	"vectors/orm/test"
)

func TestAnd(t *testing.T) {
	test.And(test.TestOrm, t)
}
