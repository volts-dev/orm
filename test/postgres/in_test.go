package postgres

import (
	"testing"
	"vectors/orm/test"
)

func TestIn(t *testing.T) {
	test.In(test.TestOrm, t)
}
