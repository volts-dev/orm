package test

import (
	"github.com/volts-dev/utils"
)

// @classic: will not create relate records for this
func (self *Testchain) Create(classic ...bool) *Testchain {
	self.PrintSubject("Create")

	isClassic := false
	if len(classic) > 0 {
		isClassic = classic[0]
	}

	user_data := new(UserModel)
	company_data := new(CompanyModel)
	*user_data = *user // copy data
	*company_data = *company

	ss := self.Orm.NewSession()
	ss.Begin()
	model, err := self.Orm.GetModel("company_model")
	if err != nil {
		self.Fatal(err)
	}

	companyId, err := model.Records().Create(company_data, isClassic)
	if err != nil {
		self.Fatal(err)
	}

	if companyId == nil {
		self.Fatal("creation didn't returnning a Id!")
	}

	user_data.CompanyId = companyId.(int64)
	// Call the API Create()
	_, err = ss.Model("user_model").Create(user_data, isClassic)
	if err != nil {
		self.Fatalf("create record failure with error < %s >", err.Error())
	}

	for i := 0; i < 10; i++ {
		user_data.Name = "Create" + utils.IntToStr(i)

		// Call the API Create()
		id, err := ss.Model("user_model").Create(user_data, isClassic)
		if err != nil {
			self.Fatalf("create record failure with error < %s >", err.Error())
		}

		if id == nil {
			self.Fatal("creation didn't returnning a Id!")
		}
	}

	err = ss.Commit()
	if err != nil {
		self.Log(err)

		e := ss.Rollback(err)
		if e != nil {
			self.Fatal(e)
		}
	}

	ss.Close()
	return self
}

func (self *Testchain) CreateM2m() *Testchain {
	self.PrintSubject("CreateM2m")

	isClassic := true
	model, _ := self.Orm.GetModel("user_model")
	dataset, err := model.Records().Read()
	if err != nil {
		self.Fatalf("manyTomany read fail %v", err)
	}

	if dataset.Count() == 0 {
		self.Fatal("please add some record first!")
	}

	dataset.Record().SetByField("many_to_many", []interface{}{1, 2})
	count, err := model.Records().Ids(dataset.FieldByName("id").AsInterface()).Write(dataset.Record().AsItfMap(), isClassic)
	if err != nil || count == 0 {
		self.Fatalf("create manyTomany fail: %v", err)
	}

	return self
}
