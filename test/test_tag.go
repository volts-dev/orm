package test

import (
	"testing"

	"github.com/volts-dev/orm"
)

func TestTag(title string, t *testing.T) {
	PrintSubject(t, title, "Table Name")
	test_tag_table_name(test_orm, t)

	PrintSubject(t, title, "Auto ")
	test_tag_auto(test_orm, t)

	PrintSubject(t, title, "PK")
	test_tag_pk(test_orm, t)

	PrintSubject(t, title, "Name")
	test_tag_name(test_orm, t)

	PrintSubject(t, title, "Store")
	test_tag_store(test_orm, t)

	PrintSubject(t, title, "Domain")
	test_tag_domain(test_orm, t)

	PrintSubject(t, title, "Version")
	test_tag_ver(test_orm, t)

}

func test_tag_table_name(o *orm.TOrm, t *testing.T) {

}

func test_tag_auto(o *orm.TOrm, t *testing.T) {

}

func test_tag_pk(o *orm.TOrm, t *testing.T) {

}

func test_tag_name(o *orm.TOrm, t *testing.T) {

}

func test_tag_store(o *orm.TOrm, t *testing.T) {

}

func test_tag_domain(o *orm.TOrm, t *testing.T) {

}

func test_tag_ver(o *orm.TOrm, t *testing.T) {

}
