package postgres

import (
	"testing"
	"vectors/orm/test"
)

func TestNotIn(t *testing.T) {
	test.NotIn(test.TestOrm, t)
}
