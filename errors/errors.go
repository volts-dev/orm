package errors

import (
	"bytes"
)

type (
	Errors struct {
		errors []error
	}
)

func New(errs ...error) *Errors {
	return &Errors{
		errors: errs,
	}

}

func (self Errors) Error() string {
	buf := bytes.NewBufferString("")
	for _, err := range self.errors {
		buf.WriteString(err.Error() + "\n")
	}
	return buf.String()
}
