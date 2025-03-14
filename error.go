package orm

import (
	"errors"
	"fmt"
)

type (
	sessionError struct {
		title  string
		errors []error
	}
)

var (
	ErrNoMapPointer          = errors.New("mp should be a map's pointer")
	ErrNoStructPointer       = errors.New("mp should be a struct's pointer")
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

// 接受多个错误 如果0错误返回nil
func newSessionError(title string, errs ...error) *sessionError {
	e := &sessionError{
		title:  title,
		errors: make([]error, 0),
	}

	for _, err := range errs {
		if err != nil {
			e.errors = append(e.errors, err)
		}
	}

	if len(e.errors) == 0 {
		return nil
	}

	return e
}

func (self sessionError) Error() string {
	return fmt.Sprintf("%s:%v ", self.title, self.errors)
}
