package orm

import (
	"testing"
)

func TestConn(title string, t *testing.T) {
	if !test_orm.IsExist(TEST_DB_NAME) {
		t.Fatalf("IsExist failed!")
	}
}
