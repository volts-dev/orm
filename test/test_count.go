package test

import (
	"fmt"
	"testing"
	"vectors/orm"
)

func count(orm *orm.TOrm, t *testing.T) {
	lUserMdl, _ := orm.GetModel("res.user")
	lCount, err := lUserMdl.Records().Count()
	fmt.Printf("Total %d records!!!\n", lCount, err)

	lCount, err = lUserMdl.Records().Where("name=? AND id=?", "Test Name", 1).Count()
	fmt.Printf("Total %d records!!!\n", lCount, err)

}
