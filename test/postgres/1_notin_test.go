package postgres

import (
	"testing"
	"volts-dev/orm/test"
)

func TestNotIn(t *testing.T) {
	test.NotIn(test.TestOrm, t)
}
