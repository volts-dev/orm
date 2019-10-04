package orm

import (
	"testing"
)

func TestConn(t *testing.T) {
	src := &orm.DataSource{
		DbType:   "postgres",
		DbName:   orm.TEST_DB_NAME,
		UserName: "postgres",
		Password: "postgres",
		SSLMode:  "disable",
	}

	err := TestInit(src, true)
	if err != nil {
		panic(err.Error())
	}

	orm.TestConn("", t)
}
