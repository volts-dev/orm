package test

import (
	"errors"
	"fmt"
	//	"errors"
	//	"fmt"
	"testing"
	//	"time"

	"vectors/orm"
)

//TODO 无ID
//TODO 带条件和字段

func create(orm *orm.TOrm, t *testing.T) {
	lUserData := &ResUser{Passport: "create", Password: "create", CompanyId: 1}

	lUserMdl, _ := orm.GetModel("res.user")
	id, err := lUserMdl.Records().Create(lUserData)
	if err != nil {
		t.Error(err)
		panic(err)
	}

	if id < 0 {
		err = errors.New("create not returned id")
		t.Error(err)
		panic(err)
		return
	}

	fmt.Println("create completed")
	t.Log("create() completed with return:", id)
}

func create_by_relate(orm *orm.TOrm, t *testing.T) {
	lUserData := &ResUser{Passport: "create_with_relate", Password: "create_with_relate", CompanyId: 1}
	lUserData.Name = "create_with_relate"
	lUserData.Active = true

	lUserMdl, _ := orm.GetModel("res.user")
	id, err := lUserMdl.Records().Create(lUserData)
	if err != nil {
		t.Error(err)
		panic(err)
	}

	if id < 0 {
		err = errors.New("create_with_relate not returned id")
		t.Error(err)
		panic(err)
		return
	}

	fmt.Println("create_with_relate completed")
	t.Log("create_with_relate() completed with return:", id)
}

func create_with_5000(orm *orm.TOrm, t *testing.T) {
	lUserData := &ResUser{Passport: "create_with_relate", Password: "create_with_relate", CompanyId: 1}
	lUserData.Name = "create_with_relate"
	lUserData.Active = true

	lUserMdl, _ := orm.GetModel("res.user")

	for i := 0; i < 5000; i++ {
		lUserData.CompanyId = int64(i)
		id, err := lUserMdl.Records().Create(lUserData)
		if err != nil {
			t.Error(err)
			panic(err)
		}

		if id < 0 {
			err = errors.New("create_with_relate not returned id")
			t.Error(err)
			panic(err)
			return
		}
	}

	t.Log("create_with_5000() completed)")
}
