package postgres

import (
	"testing"
	//"volts-dev/orm"
	"volts-dev/orm/test"

	_ "github.com/lib/pq"
)

func TestFieldSelection(t *testing.T) {
	test.FieldSelection(test.TestOrm, t)
}
