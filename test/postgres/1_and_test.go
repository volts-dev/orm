package postgres

import (
	"testing"
	"volts-dev/orm"
)

func TestAnd(t *testing.T) {
	orm.TestAnd(test.TestOrm, t)
}
