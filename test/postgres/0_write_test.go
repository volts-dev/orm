package postgres

import (
	"testing"

	"github.com/volts-dev/orm/test"
)

func TestWrite(t *testing.T) {
	test.NewTest(t).Write()
}
