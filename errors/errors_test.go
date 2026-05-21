package errors

import (
	"errors"
	"testing"
)

func TestSentinel_ErrUnsafe(t *testing.T) {
	if ErrUnsafe == nil {
		t.Fatal("ErrUnsafe must be defined")
	}
	if !errors.Is(ErrUnsafe, ErrUnsafe) {
		t.Fatal("ErrUnsafe should be errors.Is(self)")
	}
	if ErrUnsafe.Error() == "" {
		t.Fatal("ErrUnsafe must have message")
	}
}

func TestSentinel_ErrNoSoftDelete(t *testing.T) {
	if ErrNoSoftDelete == nil {
		t.Fatal("ErrNoSoftDelete must be defined")
	}
	if ErrNoSoftDelete.Error() == "" {
		t.Fatal("ErrNoSoftDelete must have message")
	}
}

func TestSentinel_ErrSoftDeleteMisconfigured(t *testing.T) {
	if ErrSoftDeleteMisconfigured == nil {
		t.Fatal("ErrSoftDeleteMisconfigured must be defined")
	}
	if ErrSoftDeleteMisconfigured.Error() == "" {
		t.Fatal("ErrSoftDeleteMisconfigured must have message")
	}
}
