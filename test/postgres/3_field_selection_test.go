package postgres

import (
	"testing"

	_ "github.com/lib/pq"
	"github.com/volts-dev/orm"
)

func TestFieldSelection(t *testing.T) {
	orm.TestFieldSelection("", t)
}
