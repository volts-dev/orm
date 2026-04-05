package test

import (
	"testing"
)

func (self *Testchain) Sync() *Testchain {
	self.PrintSubject("Sync")

	_, err := self.Orm.SyncModel("test",
		new(PartnerModel),
		new(CompanyModel),
		new(UserModel),
	)
	if err != nil {
		self.Fatalf("SyncModel() failed: %v", err)
	}

	isEmpty, err := self.Orm.IsTableEmpty("user_model")
	if err != nil {
		self.Fatalf("IsTableEmpty() failed: %v", err)
	}
	self.Logf("Sync: user_model isEmpty=%v", isEmpty)

	return self
}

// test
// #1 sync model
func test_sync(t *testing.T) {
	title := "Sync"
	PrintSubject(t, title, "SyncModel()")
	_, err := test_orm.SyncModel("test",
		new(PartnerModel),
		new(CompanyModel),
		new(UserModel),
	)
	if err != nil {
		t.Fatalf("test SyncModel() failure: %v", err)
	}

	PrintSubject(t, title, "IsTableEmpty()")
	isEmpty, err := test_orm.IsTableEmpty("partner_model")
	if err != nil {
		t.Fatalf("test IsTableEmpty() failure: %v", err)
	}
	if !isEmpty {
		t.Fatalf("model should be empty!")
	}

	isEmpty, err = test_orm.IsTableEmpty("company_model")
	if err != nil {
		t.Fatalf("test IsTableEmpty() failure: %v", err)
	}
	if !isEmpty {
		t.Fatalf("model should be empty!")
	}

	isEmpty, err = test_orm.IsTableEmpty("user_model")
	if err != nil {
		t.Fatalf("test IsTableEmpty() failure: %v", err)
	}
	if !isEmpty {
		t.Fatalf("model should be empty!")
	}

	isEmpty, err = test_orm.IsTableEmpty("company.user.ref")
	if err != nil {
		t.Fatalf("test IsTableEmpty() failure: %v", err)
	}
	if !isEmpty {
		t.Fatalf("model should be empty!")
	}

	PrintSubject(t, title, "DropTables()")
	err = test_orm.DropTables("partner_model", "company_model", "user_model", "company_user_ref")
	if err != nil {
		panic(err)
	}

	PrintSubject(t, title, "CreateTables()")
	err = test_orm.CreateTables("partner_model", "company_model", "user_model", "company_user_ref")
	if err != nil {
		panic(err)
	}

	PrintSubject(t, title, "CreateIndexes()")
	err = test_orm.CreateIndexes("partner_model")
	if err != nil {
		panic(err)
	}

	err = test_orm.CreateIndexes("company_model")
	if err != nil {
		panic(err)
	}
	err = test_orm.CreateIndexes("user_model")
	if err != nil {
		panic(err)
	}
	err = test_orm.CreateIndexes("company_user_ref")
	if err != nil {
		panic(err)
	}

	PrintSubject(t, title, "CreateUniques()")
	err = test_orm.CreateUniques("partner_model")
	if err != nil {
		panic(err)
	}
	err = test_orm.CreateUniques("company_model")
	if err != nil {
		panic(err)
	}
	err = test_orm.CreateUniques("user_model")
	if err != nil {
		panic(err)
	}
	err = test_orm.CreateUniques("company_user_ref")
	if err != nil {
		panic(err)
	}
}
