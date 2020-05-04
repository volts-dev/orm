package orm

import (
	"testing"
)

func TestMethod(t *testing.T) {
	lUserMdl, _ := test_orm.GetModel("res.user")
	lMd := lUserMdl.MethodByName("call_test")
	lMd.Call(lUserMdl, "Test_Method")
}
