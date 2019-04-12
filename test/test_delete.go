package test

import (
	"testing"
	"volts-dev/orm"
)

func Delete(title string, t *testing.T) {
	PrintSubject(title, "Delete()")
	delete(test_orm, t)
}

func delete(o *orm.TOrm, t *testing.T) {

}
