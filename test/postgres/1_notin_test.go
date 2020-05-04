package postgres

import (
	"testing"

	"github.com/volts-dev/orm"
)

func TestNotIn(t *testing.T) {
	orm.TestNotIn(test.TestOrm, t)
}
