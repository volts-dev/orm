package test

import (
	"testing"
	"vectors/orm"
)

func method(orm *orm.TOrm, t *testing.T) {
	lUserMdl, _ := orm.GetModel("res.user")
	lMd := lUserMdl.MethodByName("call_test")
	lMd.SetArgs(lUserMdl, "Test_Method")
	lMd.Call()
}
