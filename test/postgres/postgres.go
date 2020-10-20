package postgres

import (
	"github.com/volts-dev/orm"
	"github.com/volts-dev/orm/test"

	_ "github.com/lib/pq"
)

func init() {
	// set the connention source
	test.DataSource = &orm.TDataSource{
		DbType:   "postgres",
		DbName:   test.TEST_DB_NAME,
		UserName: "postgres",
		Password: "postgres",
		SSLMode:  "disable",
	}
}
