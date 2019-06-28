package orm

import (
	"testing"
)

func TestTag(title string, t *testing.T) {
	PrintSubject(title, "Table Name")
	test_tag_table_name(test_orm, t)

	PrintSubject(title, "Auto ")
	test_tag_auto(test_orm, t)

	PrintSubject(title, "PK")
	test_tag_pk(test_orm, t)

	PrintSubject(title, "Name")
	test_tag_name(test_orm, t)

	PrintSubject(title, "Store")
	test_tag_store(test_orm, t)

	PrintSubject(title, "Domain")
	test_tag_domain(test_orm, t)

	PrintSubject(title, "Version")
	test_tag_ver(test_orm, t)

}

func test_tag_table_name(o *TOrm, t *testing.T) {

}

func test_tag_auto(o *TOrm, t *testing.T) {

}

func test_tag_pk(o *TOrm, t *testing.T) {

}

func test_tag_name(o *TOrm, t *testing.T) {

}

func test_tag_store(o *TOrm, t *testing.T) {

}

func test_tag_domain(o *TOrm, t *testing.T) {

}

func test_tag_ver(o *TOrm, t *testing.T) {

}
