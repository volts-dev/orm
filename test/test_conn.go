package test

import (
	//"fmt"
	"testing"
	"vectors/orm"
)

func Conn(orm *orm.TOrm, t *testing.T) {
	err := orm.CreateDatabase("test_db_ext")
	if err != nil {
		t.Fatalf("CreateDatabase failed!")
	}

	if !orm.IsExist("test_db_ext") {
		t.Fatalf("IsExist failed!")
	}
}
