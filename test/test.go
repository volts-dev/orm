package test

import (
	"errors"
	"fmt"
	"testing"
	"vectors/orm"
)

var (
	TestOrm *orm.TOrm
)

func InitOrm(db_type, conn_str string, show_sql bool) error {
	if TestOrm == nil {
		var err error
		TestOrm, err = orm.NewOrm(db_type, conn_str)
		if err != nil {
			return err
		}

		TestOrm.ShowSql(show_sql)
		//TestOrm.logger.SetLevel()
	}

	var table_Names []string
	for _, table := range TestOrm.Tables() {
		table_Names = append(table_Names, table.Name)
	}

	if err := TestOrm.DropTables(table_Names...); err != nil {
		return err
	}

	return nil
}

func directCreateTable(orm *orm.TOrm, t *testing.T) {
	err := orm.DropTables("sys.action,res.company,res.user", "res.partner")
	if err != nil {
		t.Error(err)
	}

	err = orm.SyncModel("test",
		new(Model1),
		new(Model2),
		new(ResPartner),
		new(ResUser))
	if err != nil {
		panic(err)
	}

	isEmpty, err := orm.IsTableEmpty("res.user")
	if err != nil {
		panic(err)
	}
	if !isEmpty {
		err = errors.New("res.user should empty")
		panic(err)
	}

	isEmpty, err = orm.IsTableEmpty("res.partner")
	if err != nil {
		panic(err)
	}
	if !isEmpty {
		err = errors.New("res.partner should empty")
		panic(err)
	}

	err = orm.DropTables("res.user", "res.partner")
	if err != nil {
		panic(err)
	}

	err = orm.CreateTables("res.partner")
	err = orm.CreateTables("res.partner", "res.user")
	if err != nil {
		panic(err)
	}

	err = orm.CreateIndexes("res.user")
	if err != nil {
		panic(err)
	}

	err = orm.CreateIndexes("res.partner")
	if err != nil {
		panic(err)
	}

	err = orm.CreateUniques("res.user")
	if err != nil {
		panic(err)
	}

	err = orm.CreateUniques("res.partner")
	if err != nil {
		panic(err)
	}
}

func BaseTest(orm *orm.TOrm, t *testing.T) {
	fmt.Println("-------------- Direct Create Table --------------")
	directCreateTable(orm, t)
	fmt.Println("-------------- tag --------------")
	tag(orm, t)

	fmt.Println("-------------- Create --------------")
	create(orm, t)
	create_by_relate(orm, t)

	fmt.Println("-------------- Write --------------")
	Write(orm, t)
	write_by_id(orm, t)
	write_by_where(orm, t)

	fmt.Println("-------------- Read --------------")
	read(orm, t)
	read_by_where(orm, t)
	read_by_domain(orm, t)
	read_to_struct(orm, t)

	fmt.Println("-------------- Search --------------")
	search(orm, t)

	fmt.Println("-------------- Delete --------------")
	del(orm, t)

	fmt.Println("-------------- Count --------------")
	count(orm, t)

	fmt.Println("-------------- Limit --------------")
	limit(orm, t)

	fmt.Println("-------------- Sum --------------")
	sum(orm, t)

	fmt.Println("-------------- Custom Table Name --------------")
	custom_table_name(orm, t)

	fmt.Println("-------------- Dump --------------")
	dump(orm, t)
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
