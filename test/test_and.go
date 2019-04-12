package test

import (
	//"fmt"
	"testing"
	"volts-dev/orm"
	//	"github.com/volts-dev/utils"
)

func And(orm *orm.TOrm, t *testing.T) {
	// 注册Model
	/*	orm.SyncModel("test",
			new(BaseModel),
			new(RelateModel),
			new(BaseRelateRef),
		)

		model, _ := orm.GetModel("base.model")
		session := model.Records()

		ids := make([]interface{}, 0)
		var i int64
		for i = 0; i < 50; i++ {
			data := &BaseModel{Name: "Orm" + utils.IntToStr(i), Title: "小明" + utils.IntToStr(i+5), Help: utils.Md5(utils.IntToStr(i))}

			id, err := session.Create(data)
			if err != nil {
				t.Error(err)
				panic(err)
			}
			t.Log(id)
			ids = append(ids, i)
		}
		session.Commit()

		dataset, err := model.Records().Where("name=?", "Orm1").And("title=?", "小明6").And("help ilike ?", "%a%").Read()
		if err != nil {
			panic(err.Error())
		}

		t.Log(dataset.Keys())*/
}
