// Package errors defines domain errors for the application.
package errors

import "errors"

// Domain errors that can be returned by operations
var (
	ErrNotFound         = errors.New("resource not found")
	ErrDuplicate        = errors.New("resource already exists")
	ErrInvalidReference = errors.New("invalid reference to related resource")
	ErrUnavailable      = errors.New("service temporarily unavailable")
	ErrMetadataNotFound = errors.New("metadata not found in context")
)
