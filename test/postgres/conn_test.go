package postgres

import (
	"testing"

	"github.com/volts-dev/orm/test"
)

func TestConn(t *testing.T) {
	test.NewTest(t).Conn()
}
