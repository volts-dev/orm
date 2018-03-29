package postgres

import (
	"vectors/orm/test"

	_ "github.com/lib/pq"
)

func init() {
	var (
		db_type  = "postgres"
		db_port  = "5433"
		conn_str = "postgres://?dbname=orm_test&sslmode=disable&user=postgres&password=postgres&port=" + db_port
		show_sql = true
	)

	err := test.InitOrm(db_type, conn_str, show_sql)
	if err != nil {
		panic(err.Error())
	}
}
