package postgres

import (
	"testing"

	_ "github.com/lib/pq"
	"github.com/volts-dev/orm"
)

func TestPostgres(t *testing.T) {
	orm.BaseTest(t)
	//	UserTest1(engine, t)
	//	BaseTestAllSnakeMapper(engine, t)
	//	BaseTestAll2(engine, t)
	//	BaseTestAll3(engine, t)

	//<-make(chan int)
}

func BenchmarkPostgres(b *testing.B) {
	orm.DoBenchInsert(b)
	orm.DoBenchFind(b)
}
