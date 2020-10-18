package postgres

import (
	"github.com/volts-dev/orm"
	"github.com/volts-dev/orm/test"

	_ "github.com/lib/pq"
)

func init() {
	src := &orm.TDataSource{
		DbType:   "postgres",
		DbName:   test.TEST_DB_NAME,
		UserName: "postgres",
		Password: "postgres",
		SSLMode:  "disable",
	}

	err := test.TestInit(src, true)
	if err != nil {
		panic(err.Error())
	}
}
