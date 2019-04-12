package postgres

import (
	"testing"
	"volts-dev/orm/test"
)

func TestIn(t *testing.T) {
	test.In(test.TestOrm, t)
}
