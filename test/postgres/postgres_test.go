package postgres

import (
	//"database/sql"
	"testing"
	"volts-dev/orm/test"

	_ "github.com/lib/pq"
)

func TestPostgres(t *testing.T) {
	test.BaseTest(test.Orm(), t)
	//	UserTest1(engine, t)
	//	BaseTestAllSnakeMapper(engine, t)
	//	BaseTestAll2(engine, t)
	//	BaseTestAll3(engine, t)

	//<-make(chan int)
}

func BenchmarkPostgres(b *testing.B) {
	test.DoBenchInsert(test.Orm(), b)
	test.DoBenchFind(test.Orm(), b)
}
