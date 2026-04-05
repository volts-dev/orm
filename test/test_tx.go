package test

func (self *Testchain) Transaction() *Testchain {
	self.PrintSubject("Transaction")

	ss := self.Orm.NewSession()
	defer ss.Close()

	model, err := self.Orm.GetModel("company_model")
	if err != nil {
		self.Fatal(err)
	}

	// 1. Test Rollback
	ss.Begin()
	// Create a new record in transaction
	company_data := new(CompanyModel)
	company_data.Name = "RollbackCompany"

	// Using the session to create
	_, err = model.Tx(ss).Create(company_data)
	if err != nil {
		self.Fatalf("create record failure with error < %s >", err.Error())
	}

	// Rollback the transaction
	if err = ss.Rollback(nil); err != nil {
		self.Fatalf("rollback failure with error < %s >", err.Error())
	}

	// Verify rollback works
	count, err := model.Records().Where("name = ?", "RollbackCompany").Count()
	if err != nil {
		self.Fatal(err)
	}
	if count > 0 {
		self.Fatal("Rollback failed! The record 'RollbackCompany' still exists in the database.")
	}

	// 2. Test Commit
	ss.Begin()
	company_data = new(CompanyModel)
	company_data.Name = "CommitCompany"

	_, err = model.Tx(ss).Create(company_data)
	if err != nil {
		self.Fatalf("create record failure with error < %s >", err.Error())
	}

	// Commit the transaction
	if err = ss.Commit(); err != nil {
		self.Fatalf("commit failure with error < %s >", err.Error())
	}

	// Verify commit works
	count, err = model.Records().Where("name = ?", "CommitCompany").Count()
	//count, err = ss.Model("company_model").Where("name = ?", "CommitCompany").Count()
	if err != nil {
		self.Fatal(err)
	}
	if count == 0 {
		self.Fatal("Commit failed! The record 'CommitCompany' does not exist in the database.")
	}

	self.Log("Transaction Test Passed: Rollback and Commit work correctly.")
	return self
}
