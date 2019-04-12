package postgres

import (
	"testing"
	"volts-dev/orm/test"
)

func TestOr(t *testing.T) {
	test.Or(test.TestOrm, t)
}
