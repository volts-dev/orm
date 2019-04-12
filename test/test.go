package test

import (
	"fmt"
	"testing"
	"volts-dev/orm"
)

const DB_NAME = "test_orm"

var (
	test_orm *orm.TOrm
)

// get the test ORM object
func Orm() *orm.TOrm {
	return test_orm
}

// init the test ORM object by the driver data source
func Init(dataSource *orm.DataSource, show_sql bool) error {
	var err error

	if test_orm == nil {
		test_orm, err = orm.NewOrm(dataSource)
		if err != nil {
			return err
		}

		test_orm.ShowSql(show_sql)
		//test_orm.logger.SetLevel()
	}

	if !test_orm.IsExist(dataSource.DbName) {
		test_orm.CreateDatabase(dataSource.DbName)
	}

	// drop all table
	var table_Names []string
	for _, table := range test_orm.Tables() {
		table_Names = append(table_Names, table.Name)
	}

	if err = test_orm.DropTables(table_Names...); err != nil {
		return err
	}

	err = test_orm.SyncModel("test",
		new(PartnerModel),
		new(CompanyModel),
		new(UserModel),
		new(CompanyUserRef),
	)
	if err != nil {
		//t.Fatalf("test SyncModel() failure: %v", err)
		return err
	}

	return nil
}

func PrintSubject(subject, option string) {
	msg := fmt.Sprintf("-------------- %s : %s --------------", subject, option)
	fmt.Println(msg)
}

func BaseTest(orm *orm.TOrm, t *testing.T) {
	fmt.Println("-------------- tag --------------")
	Tag("Tag", t)

	fmt.Println("-------------- Create --------------")
	Create("Create", t)

	fmt.Println("-------------- Read --------------")
	Read("Read", t)

	fmt.Println("-------------- Write --------------")
	Write("Write", t)

	fmt.Println("-------------- Search --------------")
	Search("Search", t)

	fmt.Println("-------------- Delete --------------")
	Delete("Delete", t)

	fmt.Println("-------------- Count --------------")
	count("Count", orm, t)

	fmt.Println("-------------- Limit --------------")
	limit("Limit", orm, t)

	fmt.Println("-------------- Sum --------------")
	sum("Limit", orm, t)

	fmt.Println("-------------- Custom Table Name --------------")
	custom_table_name("Table", orm, t)

	fmt.Println("-------------- Dump --------------")
	dump("Dump", orm, t)
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

func ClassicTest(orm *orm.TOrm, t *testing.T) {

	fmt.Println("-------------- Method --------------")
	method(orm, t)
}
