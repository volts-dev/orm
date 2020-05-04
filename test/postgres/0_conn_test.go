package postgres

import (
	"testing"

	"github.com/volts-dev/orm"
)

func TestConn(t *testing.T) {
	orm.TestConn("", t)
}
