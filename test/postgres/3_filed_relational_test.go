package postgres

import (
	"testing"
	"vectors/orm/test"
)

func TestFieldMany2Many(t *testing.T) {
	test.FieldMany2Many(test.TestOrm, t)
}
