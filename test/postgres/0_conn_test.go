package postgres

import (
	"testing"
	"volts-dev/orm"
)

func TestConn(t *testing.T) {
	orm.TestConn("", t)
}
