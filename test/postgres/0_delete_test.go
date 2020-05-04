package postgres

import (
	"testing"

	"github.com/volts-dev/orm"
)

func TestDelete(t *testing.T) {
	orm.TestDelete("", t)
}
