package postgres

import (
	"testing"

	"github.com/volts-dev/orm"
)

func TestWhere(t *testing.T) {
	orm.TestCreate10("", t)
	orm.TestWhere("", t)
}
