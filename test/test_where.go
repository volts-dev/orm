package test

import (
	//"fmt"
	"testing"
	"vectors/orm"

	"github.com/volts-dev/utils"
)

func Where(orm *orm.TOrm, t *testing.T) {
	// 注册Model
	orm.SyncModel("test", new(Model1))

	model, _ := orm.GetModel("sys.action")
	session := model.Records()

	ids := make([]interface{}, 0)
	var i int64
	for i = 0; i < 10; i++ {
		data := &Model1{Type: "create", Lang: "CN" + utils.IntToStr(i), CreateId: i, WriteId: 99}

		id, err := session.Create(data)
		if err != nil {
			t.Error(err)
			panic(err)
		}
		t.Log(id)
		ids = append(ids, i)
	}
	session.Commit()

	dataset, err := model.Records().Where("create_id=?", 1).And("write_id=?", 99).And("lang ilike '?'", "cn").Read()
	if err != nil {
		panic(err.Error())
	}

	t.Log(dataset.Keys())
}
