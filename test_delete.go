package orm

import (
	"testing"
)

func TestDelete(title string, t *testing.T) {
	PrintSubject(title, "Delete()")
	test_delete(test_orm, t)
}

func test_delete(o *TOrm, t *testing.T) {

}
