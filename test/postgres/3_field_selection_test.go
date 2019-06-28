package postgres

import (
	"testing"
	//"volts-dev/orm"
	"volts-dev/orm"

	_ "github.com/lib/pq"
)

func TestFieldSelection(t *testing.T) {
	orm.TestFieldSelection(test.TestOrm, t)
}
