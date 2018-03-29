package test

import (
	"fmt"
	"testing"
	"vectors/orm"
)

func Read(arg_orm *orm.TOrm, t *testing.T) {

	// 注册Model
	arg_orm.SyncModel("test", new(ResUser), new(ResPartner))

	var (
		lDs *orm.TDataSet
		err error
	)
	lUserMdl, _ := arg_orm.GetModel("res.user")
	// 测试Select 默认所有
	lDs, err = lUserMdl.Records().Read()
	if err != nil {
		panic(err)
	}

	// 测试Select 所有
	lDs, err = lUserMdl.Records().Select("*").Read()
	if err != nil {
		panic(err)
	}

	lDs, err = lUserMdl.Records().Select("id", "name").Read()
	if err != nil {
		panic(err)
	}

	if lDs.Count() == 0 {
		//panic("Read return 0")
	}

	t.Log(lDs.Record().AsItfMap())

	User := new(ResUser)
	lDs.Record().AsStruct(User)
	t.Log(User)

}

func read(arg_orm *orm.TOrm, t *testing.T) {
	var (
		lDs *orm.TDataSet
		err error
	)
	lUserMdl, _ := arg_orm.GetModel("res.user")
	// 测试Select 默认所有
	lDs, err = lUserMdl.Records().Read()
	if err != nil {
		panic(err)
	}

	// 测试Select 所有
	lDs, err = lUserMdl.Records().Select("*").Read()
	if err != nil {
		panic(err)
	}

	lDs, err = lUserMdl.Records().Select("id", "name").Read()
	if err != nil {
		panic(err)
	}

	if lDs.Count() == 0 {
		panic("Read return 0")
	}

	t.Log(lDs.Record().AsItfMap())

	User := new(ResUser)
	lDs.Record().AsStruct(User)
	t.Log(User)

}

//# 测试Domain
func read_by_where(arg_orm *orm.TOrm, t *testing.T) {
	var (
		lDs *orm.TDataSet
		err error
	)
	fmt.Println("-------------- read_by_where --------------")
	lUserMdl, _ := arg_orm.GetModel("res.user")
	lDs, err = lUserMdl.Records().Where(`id = 1 and name ='test'`).Read()
	if err != nil {
		panic(err)
	}
	NewUser := new(ResUser)
	lDs.Record().AsStruct(NewUser)
	fmt.Println(NewUser)

	t.Log("read_by_where ok!")
}

func read_by_where_3000(arg_orm *orm.TOrm, t *testing.T) {
	var (
		//		lDs *orm.TDataSet
		err error
	)
	fmt.Println("-------------- read_by_where --------------")
	for i := 0; i < 3000; i++ {
		_, err = arg_orm.Model("res.user").Where(`id = ? and passport ='create_with_relate'`, i).Read()
		if err != nil {
			panic(err)
		}
	}

	//NewUser := new(ResUser)
	//lDs.Record().AsStruct(NewUser)
	//fmt.Println(NewUser)

	t.Log("read_by_where_5000 ok!")
}

//# 测试Domain
func read_by_where_5000(arg_orm *orm.TOrm, t *testing.T) {
	var (
		//		lDs *orm.TDataSet
		err error
	)
	fmt.Println("-------------- read_by_where --------------")
	lUserMdl, _ := arg_orm.GetModel("res.user")

	for i := 0; i < 5000; i++ {
		_, err = lUserMdl.Records().Where(`id = ? and passport ='create_with_relate'`, i).Read()
		if err != nil {
			panic(err)
		}
	}

	//NewUser := new(ResUser)
	//lDs.Record().AsStruct(NewUser)
	//fmt.Println(NewUser)

	t.Log("read_by_where_5000 ok!")
}

//# 测试Domain
func read_by_domain(arg_orm *orm.TOrm, t *testing.T) {
	var (
		lDs *orm.TDataSet
		err error
	)

	lUserMdl, _ := arg_orm.GetModel("res.user")
	lDs, err = lUserMdl.Records().Domain(`[('id', 'in', [1,6])]`).Read()
	if err != nil {
		panic(err)
	}
	NewUser := new(ResUser)
	lDs.Record().AsStruct(NewUser)
	fmt.Println(NewUser)

	t.Log("read_by_domain ok!")
}

func read_to_struct(arg_orm *orm.TOrm, t *testing.T) {
	var (
		lDs *orm.TDataSet
		err error
	)
	lUserMdl, _ := arg_orm.GetModel("res.user")
	lDs, err = lUserMdl.Records().Limit(10).Read()
	if err != nil {
		panic(err)
	}
	NewUser := new(ResUser)
	lDs.Record().AsStruct(NewUser)

	fmt.Println(NewUser)
	// 测试Select 所有
	lDs, err = lUserMdl.Records().Select("*").Limit(10).Read()
	if err != nil {
		panic(err)
	}
	User := new(ResUser)
	lDs.Record().AsStruct(&User)
	t.Log(User)

	lDs, err = lUserMdl.Records().Select("id", "name").Read()
	if err != nil {
		panic(err)
	}

	if lDs.Count() == 0 {
		panic("Read return 0")
	}

	t.Log(lDs.Record().AsItfMap())

}
