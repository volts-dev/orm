package test

import (
	//"fmt"
	"testing"
)

func Conn(t *testing.T) {
	err := test_orm.CreateDatabase("test_db_ext")
	if err != nil {
		t.Fatalf("CreateDatabase failed!")
	}

	if !test_orm.IsExist("test_db_ext") {
		t.Fatalf("IsExist failed!")
	}
}
