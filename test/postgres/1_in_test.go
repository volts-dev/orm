package postgres

import (
	"testing"

	"github.com/volts-dev/orm/test"
)

func TestIn(t *testing.T) {
	test.ShowSql = true
	test.NewTest(t).Reset().Create().In()
}
