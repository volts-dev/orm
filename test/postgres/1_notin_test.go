package postgres

import (
	"testing"
	"volts-dev/orm"
)

func TestNotIn(t *testing.T) {
	orm.TestNotIn(test.TestOrm, t)
}
