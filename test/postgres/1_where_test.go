package postgres

import (
	"testing"
	"volts-dev/orm"
)

func TestWhere(t *testing.T) {
	orm.TestWhere(test.TestOrm, t)
}
