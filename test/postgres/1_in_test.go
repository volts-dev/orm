package postgres

import (
	"testing"
	"volts-dev/orm"
)

func TestIn(t *testing.T) {
	orm.TestIn(test.TestOrm, t)
}
