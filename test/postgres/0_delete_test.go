package postgres

import (
	"testing"
	"volts-dev/orm"
)

func TestDelete(t *testing.T) {
	orm.TestDelete("", t)
}
