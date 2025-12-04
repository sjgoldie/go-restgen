package errors_test

import (
	"testing"

	apperrors "github.com/sjgoldie/go-restgen/errors"
)

func TestErrors(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		check func(error) bool
	}{
		{
			name: "ErrNotFound is defined",
			err:  apperrors.ErrNotFound,
			check: func(e error) bool {
				return e != nil && e.Error() != ""
			},
		},
		{
			name: "ErrDuplicate is defined",
			err:  apperrors.ErrDuplicate,
			check: func(e error) bool {
				return e != nil && e.Error() != ""
			},
		},
		{
			name: "ErrInvalidReference is defined",
			err:  apperrors.ErrInvalidReference,
			check: func(e error) bool {
				return e != nil && e.Error() != ""
			},
		},
		{
			name: "ErrUnavailable is defined",
			err:  apperrors.ErrUnavailable,
			check: func(e error) bool {
				return e != nil && e.Error() != ""
			},
		},
		{
			name: "ErrMetadataNotFound is defined",
			err:  apperrors.ErrMetadataNotFound,
			check: func(e error) bool {
				return e != nil && e.Error() != ""
			},
		},
		{
			name: "ErrValidation is defined",
			err:  apperrors.ErrValidation,
			check: func(e error) bool {
				return e != nil && e.Error() != ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.check(tt.err) {
				t.Errorf("Error check failed for %s", tt.name)
			}
		})
	}
}

func TestErrors_Distinct(t *testing.T) {
	errors := []error{
		apperrors.ErrNotFound,
		apperrors.ErrDuplicate,
		apperrors.ErrInvalidReference,
		apperrors.ErrUnavailable,
		apperrors.ErrMetadataNotFound,
		apperrors.ErrValidation,
	}

	// Verify all errors are distinct
	for i, err1 := range errors {
		for j, err2 := range errors {
			if i != j && err1 == err2 {
				t.Errorf("Errors at index %d and %d are the same", i, j)
			}
		}
	}
}

func TestErrors_Messages(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		message string
	}{
		{
			name:    "ErrNotFound message",
			err:     apperrors.ErrNotFound,
			message: "resource not found",
		},
		{
			name:    "ErrDuplicate message",
			err:     apperrors.ErrDuplicate,
			message: "resource already exists",
		},
		{
			name:    "ErrInvalidReference message",
			err:     apperrors.ErrInvalidReference,
			message: "invalid reference to related resource",
		},
		{
			name:    "ErrUnavailable message",
			err:     apperrors.ErrUnavailable,
			message: "service temporarily unavailable",
		},
		{
			name:    "ErrMetadataNotFound message",
			err:     apperrors.ErrMetadataNotFound,
			message: "metadata not found in context",
		},
		{
			name:    "ErrValidation message",
			err:     apperrors.ErrValidation,
			message: "validation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Error() != tt.message {
				t.Errorf("Expected message '%s', got '%s'", tt.message, tt.err.Error())
			}
		})
	}
}

func TestValidationError(t *testing.T) {
	t.Run("NewValidationError creates error with message", func(t *testing.T) {
		err := apperrors.NewValidationError("priority must be between 1 and 5")
		if err.Message != "priority must be between 1 and 5" {
			t.Errorf("Expected message 'priority must be between 1 and 5', got '%s'", err.Message)
		}
	})

	t.Run("Error returns message", func(t *testing.T) {
		err := apperrors.NewValidationError("invalid status")
		if err.Error() != "invalid status" {
			t.Errorf("Expected Error() to return 'invalid status', got '%s'", err.Error())
		}
	})

	t.Run("Is matches ErrValidation", func(t *testing.T) {
		err := apperrors.NewValidationError("test error")
		if !err.Is(apperrors.ErrValidation) {
			t.Error("Expected ValidationError.Is(ErrValidation) to return true")
		}
	})

	t.Run("Is does not match other errors", func(t *testing.T) {
		err := apperrors.NewValidationError("test error")
		if err.Is(apperrors.ErrNotFound) {
			t.Error("Expected ValidationError.Is(ErrNotFound) to return false")
		}
	})
}
