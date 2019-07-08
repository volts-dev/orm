package orm

import (
	"fmt"
	"testing"
)

func TestCount(title string, t *testing.T) {
	PrintSubject(title, "Count()")
	test_count(test_orm, t)

}

func test_count(orm *TOrm, t *testing.T) {
	lUserMdl, _ := orm.GetModel("res.user")
	lCount, err := lUserMdl.Records().Count()
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("Total %d records!!!\n", lCount)

	lCount, err = lUserMdl.Records().Where("name=? AND id=?", "Test Name", 1).Count()
	if err != nil {
		t.Fatal(err)
	}

	fmt.Printf("Total %d records!!!\n", lCount)

}
