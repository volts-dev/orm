package test

import (
	"errors"
	"time"
	//	"errors"
	//	"fmt"
	"testing"
	//	"time"

	"vectors/orm"
)

//TODO 无ID
//TODO 带条件和字段

func Write(orm *orm.TOrm, t *testing.T) {
	// 注册Model
	orm.SyncModel("test", new(ResUser), new(ResPartner))

	lUserData := &ResUser{Passport: "create", Password: "create", CompanyId: 1}
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

	lUserData = &ResUser{Passport: "create", Password: "create_write", CompanyId: 1}
	lUserData.Name = "Test Name"

	lUserMdl, _ = orm.GetModel("res.user")
	lWhere := `passport='create' and password='create'`
	effect, err := lUserMdl.Records().Select("password").Where(lWhere).Write(lUserData)
	if err != nil {
		t.Error(err)
		panic(err)
	}

	if effect != 1 {
		err = errors.New("insert not returned 1")
		t.Error(err)
		panic(err)
		return
	}

	t.Log("write_select_where return effect", effect)
}

func write(orm *orm.TOrm, t *testing.T) {
	lUserData := &ResUser{Passport: "create", Password: "create", CompanyId: 1}
	lUserData.Name = "Test Name"

	lUserMdl, _ := orm.GetModel("res.user")
	effect, err := lUserMdl.Records().Write(lUserData)
	if err != nil {
		t.Error(err)
		panic(err)
	}

	if effect != 1 {
		err = errors.New("insert not returned 1")
		t.Error(err)
		//panic(err)
		//return
	}

	t.Log("WriteRecord return effect", effect)
}

func write_by_id(orm *orm.TOrm, t *testing.T) {
}

//
func write_by_where(orm *orm.TOrm, t *testing.T) {
	// 延时测试 时间Create|update更新
	time.Sleep(1 * time.Second)

	lUserData := &ResUser{Passport: "create", Password: "create_write", CompanyId: 1}
	lUserData.Name = "Test Name"

	lUserMdl, _ := orm.GetModel("res.user")
	lWhere := `passport='create' and password='create'`
	effect, err := lUserMdl.Records().Select("password").Where(lWhere).Write(lUserData)
	if err != nil {
		t.Error(err)
		panic(err)
	}

	if effect != 1 {
		err = errors.New("insert not returned 1")
		t.Error(err)
		panic(err)
		return
	}

	t.Log("write_select_where return effect", effect)
}
