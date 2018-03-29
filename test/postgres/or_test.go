package postgres

import (
	"testing"
	"vectors/orm/test"
)

func TestOr(t *testing.T) {
	test.Or(test.TestOrm, t)
}
