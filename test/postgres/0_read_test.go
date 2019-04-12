package postgres

import (
	"testing"
	"volts-dev/orm/test"
)

func TestRead(t *testing.T) {
	test.Create("", t)
	test.Read("", t)
	test.Write("", t)
}
