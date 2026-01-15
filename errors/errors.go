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
	ErrValidation       = errors.New("validation failed")
)

// ValidationError wraps a validation error with a custom message
// The message will be returned to the client as the error response
type ValidationError struct {
	Message string
}

// Error implements the error interface
func (e *ValidationError) Error() string {
	return e.Message
}

// Is allows errors.Is(err, ErrValidation) to match ValidationError
func (e *ValidationError) Is(target error) bool {
	return target == ErrValidation
}

// NewValidationError creates a new ValidationError with the given message
func NewValidationError(message string) *ValidationError {
	return &ValidationError{Message: message}
}
