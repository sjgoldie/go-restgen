package datastore

import (
	"database/sql"
	"errors"
	"testing"

	apperrors "github.com/sjgoldie/go-restgen/errors"
)

func TestTranslateError(t *testing.T) {
	w := &Wrapper[struct{}]{}

	tests := []struct {
		name     string
		err      error
		expected error
	}{
		{
			name:     "sql.ErrConnDone returns ErrUnavailable",
			err:      sql.ErrConnDone,
			expected: apperrors.ErrUnavailable,
		},
		{
			name:     "sql.ErrNoRows returns ErrNotFound",
			err:      sql.ErrNoRows,
			expected: apperrors.ErrNotFound,
		},
		{
			name:     "SQLite UNIQUE constraint returns ErrDuplicate",
			err:      errors.New("UNIQUE constraint failed: users.email"),
			expected: apperrors.ErrDuplicate,
		},
		{
			name:     "SQLite FOREIGN KEY constraint returns ErrInvalidReference",
			err:      errors.New("FOREIGN KEY constraint failed"),
			expected: apperrors.ErrInvalidReference,
		},
		{
			name:     "unknown error passes through",
			err:      errors.New("some other error"),
			expected: errors.New("some other error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := w.translateError(tt.err)

			// For sentinel errors, use errors.Is
			if errors.Is(tt.expected, apperrors.ErrUnavailable) ||
				errors.Is(tt.expected, apperrors.ErrNotFound) ||
				errors.Is(tt.expected, apperrors.ErrDuplicate) ||
				errors.Is(tt.expected, apperrors.ErrInvalidReference) {
				if !errors.Is(result, tt.expected) {
					t.Errorf("expected %v, got %v", tt.expected, result)
				}
			} else {
				// For pass-through errors, compare the error message
				if result.Error() != tt.expected.Error() {
					t.Errorf("expected %v, got %v", tt.expected, result)
				}
			}
		})
	}
}
