package postgres

import (
	"testing"
	"volts-dev/orm"
)

func TestFieldMany2Many(t *testing.T) {
	orm.TestFieldMany2Many(test.TestOrm, t)
}
