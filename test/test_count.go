package test

import (
	"fmt"
)

//
func (self *Testchain) Count() *Testchain {
	self.PrintSubject("Count")
	lUserMdl, _ := self.Orm.GetModel("res.user")
	lCount, err := lUserMdl.Records().Count()
	if err != nil {
		self.Fatal(err)
	}
	fmt.Printf("Total %d records!!!\n", lCount)

	lCount, err = lUserMdl.Records().Where("name=? AND id=?", "Test Name", 1).Count()
	if err != nil {
		self.Fatal(err)
	}

	fmt.Printf("Total %d records!!!\n", lCount)

	return self
}
