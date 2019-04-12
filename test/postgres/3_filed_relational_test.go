package postgres

import (
	"testing"
	"volts-dev/orm/test"
)

func TestFieldMany2Many(t *testing.T) {
	test.FieldMany2Many(test.TestOrm, t)
}
