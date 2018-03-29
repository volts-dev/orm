package postgres

import (
	"testing"
	"vectors/orm/test"
)

func TestWhere(t *testing.T) {
	test.Where(test.TestOrm, t)
}
