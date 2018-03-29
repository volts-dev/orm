package postgres

import (
	"testing"
	"vectors/orm/test"
)

func TestOr(t *testing.T) {
	test.Write(test.TestOrm, t)
}
