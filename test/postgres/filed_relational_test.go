package postgres

import (
	"testing"
	"vectors/orm/test"
)

func Test1(t *testing.T) {
	test.FieldMany2Many(test.TestOrm, t)
}
