package postgres

import (
	"volts-dev/orm"

	_ "github.com/lib/pq"
)

func init() {
	src := &orm.DataSource{
		DbType:   "postgres",
		DbName:   orm.TEST_DB_NAME,
		UserName: "postgres",
		Password: "postgres",
		SSLMode:  "disable",
	}

	err := orm.TestInit(src, true)
	if err != nil {
		panic(err.Error())
	}
}
