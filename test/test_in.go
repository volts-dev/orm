package test

import (
	//"fmt"
	"testing"
	"vectors/orm"
)

func In(orm *orm.TOrm, t *testing.T) {
	// 注册Model
	orm.SyncModel("test", new(Model1))

	model, _ := orm.GetModel("model1")
	session := model.Records()

	ids := make([]interface{}, 0)
	var i int64
	for i = 0; i < 10; i++ {
		data := &Model1{Type: "create", Lang: "CN", CreateId: i}

		id, err := session.Create(data)
		if err != nil {
			t.Error(err)
			panic(err)
		}
		t.Log(id)
		ids = append(ids, i)
	}
	session.Commit()

	dataset, err := model.Records().In("create_id", 1, 3).Read()
	if err != nil {
		panic(err.Error())
	}

	t.Log(dataset.Keys())

}
