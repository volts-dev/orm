package test

import (
	"fmt"
	"testing"

	"github.com/volts-dev/orm"
)

const TEST_DB_NAME = "test_orm"

var (
	test_orm   *orm.TOrm
	ShowSql    bool
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
	self.Orm, err = orm.NewOrm(DataSource)
	if err != nil {
		self.Fatal(err)
	}

	self.Orm.ShowSql(ShowSql)

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
	msg := fmt.Sprintf("-------------------- %s --------------------", subject)
	fmt.Println(msg)
	return self
}

func (self *Testchain) ShowSql(show bool) *Testchain {
	self.Orm.ShowSql(show)
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
		test_orm, err = orm.NewOrm(dataSource)
		if err != nil {
			return err
		}

		test_orm.ShowSql(show_sql)
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

func PrintSubject(subject, option string) {
	msg := fmt.Sprintf("-------------- %s : %s --------------", subject, option)
	fmt.Println(msg)
}

func BaseTest(t *testing.T) {
	/*
		fmt.Println("-------------- tag --------------")
		TestTag("Tag", t)

		fmt.Println("-------------- Read --------------")
		TestRead("Read", t)

		fmt.Println("-------------- Write --------------")
		TestWrite("Write", t)

		fmt.Println("-------------- Search --------------")
		TestSearch("Search", t)

		fmt.Println("-------------- Delete --------------")
		TestDelete("Delete", t)

		fmt.Println("-------------- Count --------------")
		TestCount("Count", t)

		fmt.Println("-------------- Limit --------------")
		TestLimit("Limit", t)

		fmt.Println("-------------- Sum --------------")
		TestSum("Limit", t)

		fmt.Println("-------------- Custom Table Name --------------")
		custom_table_name("Table", t)

		fmt.Println("-------------- Dump --------------")
		TestDump("Dump", t)
		/*	fmt.Println("-------------- insertAutoIncr --------------")
			insertAutoIncr(orm, t)
			fmt.Println("-------------- insertMulti --------------")
			insertMulti(orm, t)
			fmt.Println("-------------- insertTwoTable --------------")
			insertTwoTable(orm, t)
			fmt.Println("-------------- testDelete --------------")
			testDelete(orm, t)
			fmt.Println("-------------- get --------------")
			get(orm, t)
			fmt.Println("-------------- testCascade --------------")
			testCascade(orm, t)
			fmt.Println("-------------- find --------------")
			find(orm, t)
			fmt.Println("-------------- find2 --------------")
			find2(orm, t)
			fmt.Println("-------------- findMap --------------")
			findMap(orm, t)
			fmt.Println("-------------- findMap2 --------------")
			findMap2(orm, t)
			fmt.Println("-------------- count --------------")
			count(orm, t)
			fmt.Println("-------------- where --------------")
			where(orm, t)
			fmt.Println("-------------- in --------------")
			in(orm, t)

			fmt.Println("-------------- testCustomTableName --------------")
			testCustomTableName(orm, t)
			fmt.Println("-------------- testDump --------------")
			testDump(orm, t)
			fmt.Println("-------------- testConversion --------------")
			testConversion(orm, t)
			fmt.Println("-------------- testJsonField --------------")
			testJsonField(orm, t)

	*/
}

func ClassicTest(t *testing.T) {
	fmt.Println("-------------- Method --------------")
	TestMethod(t)
}
