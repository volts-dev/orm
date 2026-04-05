package test

import (
	"testing"
)

func (self *Testchain) NotIn() *Testchain {
	self.PrintSubject("NotIn")

	model, err := self.Orm.GetModel("user_model")
	if err != nil {
		self.Fatal(err)
	}

	// collect all IDs
	allIds, _, err := model.Records().Search()
	if err != nil {
		self.Fatal(err)
	}
	if len(allIds) < 2 {
		self.Skip("NotIn: need at least 2 records")
	}

	// exclude first ID
	excluded := allIds[:1]
	ds, err := model.Records().NotIn("id", excluded...).Read()
	if err != nil {
		self.Fatal(err)
	}
	if ds.Count() >= len(allIds) {
		self.Fatalf("NotIn did not filter: got %d, total %d", ds.Count(), len(allIds))
	}

	return self
}

func TestNotIn(title string, t *testing.T) {
	// 注册Model
	/*	orm.SyncModel("test", new(Model1))

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

		dataset, err := model.Records().NotIn("create_id", 1, 3).Read()
		if err != nil {
			panic(err.Error())
		}

		t.Log(dataset.Keys())
	*/
}
