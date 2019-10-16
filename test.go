package orm

import (
	"fmt"
	"testing"
)

const TEST_DB_NAME = "orm_test"

var (
	test_orm *TOrm
)

// get the test ORM object
func Orm() *TOrm {
	return test_orm
}

// init the test ORM object by the driver data source
func TestInit(dataSource *TDataSource, show_sql bool) error {
	var err error

	if test_orm == nil {
		test_orm, err = NewOrm(dataSource)
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

	if len(table_Names) > 0 {
		if err = test_orm.DropTables(table_Names...); err != nil {
			return err
		}
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

func BaseTest(orm *TOrm, t *testing.T) {
	fmt.Println("-------------- tag --------------")
	TestTag("Tag", t)

	fmt.Println("-------------- Create --------------")
	TestCreate("Create", t)

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
	TestLimit("Limit", orm, t)

	fmt.Println("-------------- Sum --------------")
	TestSum("Limit", orm, t)

	fmt.Println("-------------- Custom Table Name --------------")
	custom_table_name("Table", orm, t)

	fmt.Println("-------------- Dump --------------")
	TestDump("Dump", orm, t)
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

func ClassicTest(orm *TOrm, t *testing.T) {
	fmt.Println("-------------- Method --------------")
	TestMethod(orm, t)
}
