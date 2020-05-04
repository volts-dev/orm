package postgres

import (
	"testing"

	"github.com/volts-dev/orm"
)

func TestCreate(t *testing.T) {
	orm.TestCreate("", t)
}
