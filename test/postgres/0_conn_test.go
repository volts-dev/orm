package postgres

import (
	"testing"
	"vectors/orm/test"
)

func TestConn(t *testing.T) {
	test.Conn(test.TestOrm, t)
}
