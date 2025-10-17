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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Error() != tt.message {
				t.Errorf("Expected message '%s', got '%s'", tt.message, tt.err.Error())
			}
		})
	}
}
