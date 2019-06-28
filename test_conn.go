package orm

import (
	//"fmt"
	"testing"
)

func TestConn(title string, t *testing.T) {
	if !test_orm.IsExist("test_db_ext") {
		t.Fatalf("IsExist failed!")
	}

	err := test_orm.CreateDatabase("test_db_ext")
	if err != nil {
		t.Fatalf("CreateDatabase failed! : %v", err)
	}
}
