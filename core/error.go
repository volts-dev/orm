package core

import "errors"

var (
	// ErrNoMapPointer represents error when no map pointer
	ErrNoMapPointer = errors.New("mp should be a map's pointer")
	// ErrNoStructPointer represents error when no struct pointer
	ErrNoStructPointer = errors.New("mp should be a struct's pointer")
)
