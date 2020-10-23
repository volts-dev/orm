package postgres

import (
	"testing"

	"github.com/volts-dev/orm/test"
)

func TestCreate(t *testing.T) {
	test.ShowSql = true
	test.NewTest(t).Reset().Create().CreateM2m()
}
