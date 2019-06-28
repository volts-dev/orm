package orm

import (
	//"fmt"
	"testing"
	//"github.com/volts-dev/utils"
)

func TestFieldMany2Many(o *TOrm, t *testing.T) {
	/*	err := o.SyncModel("test",
			new(Model1),
			new(Model2),
			new(ResCompanyUserRel),
			new(ResUser),
			new(ResPartner),
		)

		if err != nil {
			panic(err.Error())
		}

		// 创建新公司
		var ids []int64
		for i := 0; i < 5; i++ {
			NewCompany := &Model2{Name: "NewCompany" + utils.IntToStr(i)}
			model, _ := o.GetModel("res.company")
			if model == nil {
				panic("Syncmodel error! did not found model!")
			}

			effect, err := model.Records().Create(NewCompany)
			if err != nil {
				panic(err.Error())
			}
			ids = append(ids, effect)
			fmt.Println("Create Company:", NewCompany)
		}

		// 创建新用户
		lUserData := &ResUser{Passport: "create", Password: "create", CompanyId: 1, CompanyIds: ids}
		lUserData.Name = "Tester"
		lUserMdl, _ := o.GetModel("res.user")
		_, err = lUserMdl.Records().Create(lUserData)
		if err != nil {
			panic(err)
		}

		// 测试Select 默认所有
		lDs, err := lUserMdl.Records().Read()
		if err != nil {
			panic(err)
		}

		if lDs.Count() > 0 {
			t.Log(lDs.Count(), lDs.Position, lDs.Keys())
			t.Log(lDs.Record(), lDs.Record() == lDs.FieldByName("name").RecSet)
			t.Log(lDs.Record().AsItfMap())
			t.Log(lDs.Record().Fields)
			rec := lDs.FieldByName("name")
			//t.Log(rec.RecSet._getByName(rec.Name, false))
			t.Log(rec.AsString())

			// testing selection
			t.Log(lDs.FieldByName("type").AsInterface())

			// testing many2one
			t.Log(lDs.FieldByName("company_id").AsInterface())

			// testing many2many
			t.Log(lDs.FieldByName("company_ids").AsInterface())
		} else {
			t.Log("0 result")
		}
	*/
}
