package postgres

import (
	"testing"

	"github.com/volts-dev/orm"
)

func TestRead(t *testing.T) {
	orm.TestCreate("", t)
}
