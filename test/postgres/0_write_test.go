package postgres

import (
	"testing"
	"vectors/orm/test"
)

func TestWrite(t *testing.T) {
	test.Write(test.TestOrm, t)
}
