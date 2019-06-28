package postgres

import (
	"testing"
	"volts-dev/orm"
)

func TestWrite(t *testing.T) {
	orm.TestWrite("", t)
}
