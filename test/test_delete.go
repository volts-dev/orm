package test

import (
	"testing"

	"github.com/volts-dev/orm"
)

func TestDelete(title string, t *testing.T) {
	PrintSubject(title, "Delete()")
	test_delete(test_orm, t)
}

func test_delete(o *orm.TOrm, t *testing.T) {

}
