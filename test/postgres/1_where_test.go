package postgres

import (
	"testing"
	"volts-dev/orm/test"
)

func TestWhere(t *testing.T) {
	test.Where(test.TestOrm, t)
}
