package orm

import (
	"testing"
)

// test
// #1 sync model
func test_sync(t *testing.T) {
	title := "Sync"
	PrintSubject(title, "SyncModel()")
	err := test_orm.SyncModel("test",
		new(PartnerModel),
		new(CompanyModel),
		new(UserModel),
	)
	if err != nil {
		t.Fatalf("test SyncModel() failure: %v", err)
	}

	PrintSubject(title, "IsTableEmpty()")
	isEmpty, err := test_orm.IsTableEmpty("partner.model")
	if err != nil {
		t.Fatalf("test IsTableEmpty() failure: %v", err)
	}
	if !isEmpty {
		t.Fatalf("model should be empty!")
	}

	isEmpty, err = test_orm.IsTableEmpty("company.model")
	if err != nil {
		t.Fatalf("test IsTableEmpty() failure: %v", err)
	}
	if !isEmpty {
		t.Fatalf("model should be empty!")
	}

	isEmpty, err = test_orm.IsTableEmpty("user.model")
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

	PrintSubject(title, "DropTables()")
	err = test_orm.DropTables("partner.model", "company.model", "user.model", "company.user.ref")
	if err != nil {
		panic(err)
	}

	PrintSubject(title, "CreateTables()")
	err = test_orm.CreateTables("partner.model", "company.model", "user.model", "company.user.ref")
	if err != nil {
		panic(err)
	}

	PrintSubject(title, "CreateIndexes()")
	err = test_orm.CreateIndexes("partner.model")
	if err != nil {
		panic(err)
	}

	err = test_orm.CreateIndexes("company.model")
	if err != nil {
		panic(err)
	}
	err = test_orm.CreateIndexes("user.model")
	if err != nil {
		panic(err)
	}
	err = test_orm.CreateIndexes("company.user.ref")
	if err != nil {
		panic(err)
	}

	PrintSubject(title, "CreateUniques()")
	err = test_orm.CreateUniques("partner.model")
	if err != nil {
		panic(err)
	}
	err = test_orm.CreateUniques("company.model")
	if err != nil {
		panic(err)
	}
	err = test_orm.CreateUniques("user.model")
	if err != nil {
		panic(err)
	}
	err = test_orm.CreateUniques("company.user.ref")
	if err != nil {
		panic(err)
	}
}
