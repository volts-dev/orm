package postgres

import (
	"testing"

	"github.com/volts-dev/orm/test"
)

func TestCreate(t *testing.T) {
	test.ShowSql = true
	test.NewTest(t).Reset().Create().CreateM2m()
}

func TestCreateO2O(t *testing.T) {
	test.ShowSql = true
	test.NewTest(t).Reset().Create()
}

func TestCreateWrongData(t *testing.T) {
	test.ShowSql = true
	test.NewTest(t).Reset().CreateNone()
}

func TestCreateOnConflict(t *testing.T) {
	test.ShowSql = true
	test.NewTest(t).Reset().CreateOnConflict()
}
