package postgres

import (
	"testing"
	"volts-dev/orm"
)

func TestRead(t *testing.T) {
	orm.TestCreate10("", t)
	orm.TestRead("", t)
}
