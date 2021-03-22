package orm

import (
	"errors"
)

type (
	TErrors struct {
		list []error
	}
)

var (
	ErrNoMapPointer    = errors.New("mp should be a map's pointer")
	ErrNoStructPointer = errors.New("mp should be a struct's pointer")

	ErrParamsType      error = errors.New("Params type error")
	ErrTableNotFound   error = errors.New("Not found table")
	ErrUnSupportedType error = errors.New("Unsupported type error")
	ErrNotExist        error = errors.New("Not exist error")
	ErrCacheFailed     error = errors.New("Cache failed")
	ErrNeedDeletedCond error = errors.New("Delete need at least one condition")
	ErrNotImplemented  error = errors.New("Not implemented.")
	ErrDeleteFailed    error = errors.New("Delete Failed.")
	ErrInvalidSession  error = errors.New("The session of query is invalid!")
)
