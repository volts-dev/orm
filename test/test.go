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

	// 注意：暂不在 harness 里 Freeze 激活 one2one 委托继承。
	// 原因：继承字段读取走 inherits_join_calc 的隐式(INNER)JOIN，而写入仅在
	// 继承字段非空时才自动创建父记录；本套件的 fixture 创建 user/company 时未设
	// 继承字段(homepage 等)，partner_id 为 NULL，INNER JOIN 会过滤掉这些记录，
	// 导致 Read() 返回空集。待 inherits_join_calc 改 LEFT JOIN(或写入保证父记录)
	// 后再在 harness 启用 Freeze。one2one 读写的正确性已由独立用例
	// TestOne2OneInheritReadWrite(自带 Freeze + 父记录)覆盖。
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
