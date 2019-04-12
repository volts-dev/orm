package postgres

import (
	"volts-dev/orm"
	"volts-dev/orm/test"

	_ "github.com/lib/pq"
)

func init() {
	src := &orm.DataSource{
		DbType:   "postgres",
		DbName:   test.DB_NAME,
		UserName: "postgres",
		Password: "postgres",
		SSLMode:  "disable",
	}

	err := test.Init(src, true)
	if err != nil {
		panic(err.Error())
	}
}
