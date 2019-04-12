package test

import (
	"testing"
	"volts-dev/orm"
)

func Tag(title string, t *testing.T) {
	PrintSubject(title, "Table Name")
	tag_table_name(test_orm, t)

	PrintSubject(title, "Auto ")
	tag_auto(test_orm, t)

	PrintSubject(title, "PK")
	tag_pk(test_orm, t)

	PrintSubject(title, "Name")
	tag_name(test_orm, t)

	PrintSubject(title, "Store")
	tag_store(test_orm, t)

	PrintSubject(title, "Domain")
	tag_domain(test_orm, t)

	PrintSubject(title, "Version")
	tag_ver(test_orm, t)

}

func tag_table_name(o *orm.TOrm, t *testing.T) {

}

func tag_auto(o *orm.TOrm, t *testing.T) {

}

func tag_pk(o *orm.TOrm, t *testing.T) {

}

func tag_name(o *orm.TOrm, t *testing.T) {

}

func tag_store(o *orm.TOrm, t *testing.T) {

}

func tag_domain(o *orm.TOrm, t *testing.T) {

}

func tag_ver(o *orm.TOrm, t *testing.T) {

}
