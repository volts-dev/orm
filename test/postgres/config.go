package postgres

import (
	"vectors/orm"
	"vectors/orm/test"

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

	err := test.InitOrm(src, true)
	if err != nil {
		panic(err.Error())
	}
}
