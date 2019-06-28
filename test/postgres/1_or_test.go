package postgres

import (
	"testing"
	"volts-dev/orm"
)

func TestOr(t *testing.T) {
	orm.TestOr(test.TestOrm, t)
}
