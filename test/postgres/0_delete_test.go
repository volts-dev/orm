package postgres

import (
	"testing"

	"github.com/volts-dev/orm/test"
)

func TestDelete(t *testing.T) {
	test.TestDelete("", t)
}
