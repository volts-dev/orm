package postgres

import (
	"testing"

	"github.com/volts-dev/orm"
)

func TestWrite(t *testing.T) {
	orm.TestWrite("", t)
}
