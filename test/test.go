package test

import (
	"testing"

	"github.com/volts-dev/orm"
)

const TEST_DB_NAME = "test_orm"

var (
	test_orm   *orm.TOrm
	ShowSql    bool = true
	DataSource *orm.TDataSource
)

type (
	Testchain struct {
		*testing.T
		Orm *orm.TOrm
	}
)

func NewTest(t *testing.T) *Testchain {
	self := &Testchain{
		T:   t,
		Orm: test_orm,
	}

	var err error
	self.Orm, err = orm.New(orm.WithDataSource(DataSource))
	if err != nil {
		self.Fatal(err)
	}

	self.Orm.Config().Init(orm.WithShowSql(true))

	if !self.Orm.IsExist(DataSource.DbName) {
		self.Orm.CreateDatabase(DataSource.DbName)
	}

	_, err = self.Orm.SyncModel("test",
		new(PartnerModel),
		new(CompanyModel),
		new(UserModel),
	)

	if err != nil {
		self.Fatal(err)
	}

	return self
}

func (self *Testchain) PrintSubject(subject string) *Testchain {
	self.Logf("-------------------- %s --------------------", subject)
	return self
}

func (self *Testchain) ShowSql(show bool) *Testchain {
	self.Orm.Config().Init(orm.WithShowSql(show))
	return self
}

func (self *Testchain) Reset() *Testchain {
	// drop all table
	self.PrintSubject("Reset database")

	self.Log("Loading database...")
	var table_Names []string
	for _, table := range self.Orm.GetModels() {
		table_Names = append(table_Names, table)
		self.Logf("Table < %s > found!", table)
	}

	self.Log("Dropping tables...")
	if len(table_Names) > 0 {
		if err := self.Orm.DropTables(table_Names...); err != nil {
			self.Fatal(err)
		}
	}

	self.Logf("Creating Tables...")
	_, err := self.Orm.SyncModel("test",
		new(PartnerModel),
		new(CompanyModel),
		new(UserModel),
	)
	if err != nil {
		self.Fatal(err)
	}

	self.Logf("Rest database completed!")
	return self
}

// get the test ORM object
func Orm() *orm.TOrm {
	return test_orm
}

// init the test ORM object by the driver data source
func TestInit(dataSource *orm.TDataSource, show_sql bool) error {
	var err error

	if test_orm == nil {
		test_orm, err = orm.New(orm.WithDataSource(DataSource))
		if err != nil {
			return err
		}
		test_orm.Config().Init(orm.WithShowSql(show_sql))
	}

	if !test_orm.IsExist(dataSource.DbName) {
		test_orm.CreateDatabase(dataSource.DbName)
	}

	// drop all table
	var table_Names []string
	for _, table := range test_orm.GetModels() {
		table_Names = append(table_Names, table)
	}

	_, err = test_orm.SyncModel("test",
		new(PartnerModel),
		new(CompanyModel),
		new(UserModel),
	)

	if err != nil {
		return err
	}

	return nil
}

func PrintSubject(t *testing.T, subject, option string) {
	t.Logf("-------------- %s : %s --------------", subject, option)
}

