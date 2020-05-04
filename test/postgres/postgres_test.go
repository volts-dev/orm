package postgres

import (
	//"database/sql"
	"testing"

	"github.com/volts-dev/orm"

	_ "github.com/lib/pq"
)

func TestPostgres(t *testing.T) {
	orm.BaseTest(test.Orm(), t)
	//	UserTest1(engine, t)
	//	BaseTestAllSnakeMapper(engine, t)
	//	BaseTestAll2(engine, t)
	//	BaseTestAll3(engine, t)

	//<-make(chan int)
}

func BenchmarkPostgres(b *testing.B) {
	orm.DoBenchInsert(test.Orm(), b)
	orm.DoBenchFind(test.Orm(), b)
}
