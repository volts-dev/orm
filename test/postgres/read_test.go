package postgres

import (
	"testing"
	"vectors/orm/test"
)

func TestRead(t *testing.T) {
	test.Read(test.TestOrm, t)
}
