package postgres

import (
	"testing"

	"github.com/volts-dev/orm/test"
)

func TestRead(t *testing.T) {
	//test.ClearDatabase = false
	//test.TestCreate10("", t)
	test.TestRead("", t)
}
