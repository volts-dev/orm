package postgres

import (
	"testing"
	"volts-dev/orm/test"
)

func TestAnd(t *testing.T) {
	test.And(test.TestOrm, t)
}
