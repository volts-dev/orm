package postgres

import (
	//"database/sql"
	"testing"
	"vectors/orm/test"

	_ "github.com/lib/pq"
)

func TestPostgres(t *testing.T) {
	test.BaseTest(test.TestOrm, t)
	//	UserTest1(engine, t)
	//	BaseTestAllSnakeMapper(engine, t)
	//	BaseTestAll2(engine, t)
	//	BaseTestAll3(engine, t)

	//<-make(chan int)
}

func BenchmarkPostgres(b *testing.B) {

	test.DoBenchInsert(test.TestOrm, b)
	test.DoBenchFind(test.TestOrm, b)
}
