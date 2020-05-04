package postgres

import (
	"testing"

	"github.com/github.com/volts-dev/orm"
	_ "github.com/lib/pq"
)

func TestFieldSelection(t *testing.T) {
	orm.TestFieldSelection(test.TestOrm, t)
}
