package datastore

import (
	"database/sql"
	"errors"
	"reflect"
	"testing"

	apperrors "github.com/sjgoldie/go-restgen/errors"
	"github.com/sjgoldie/go-restgen/metadata"
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

func TestDefaultParentJoinCol(t *testing.T) {
	if got := defaultParentJoinCol(""); got != "id" {
		t.Errorf("expected 'id' for empty string, got %q", got)
	}
	if got := defaultParentJoinCol("nmi"); got != "nmi" {
		t.Errorf("expected 'nmi', got %q", got)
	}
}

func TestDerefType(t *testing.T) {
	type Foo struct{}

	direct := reflect.TypeOf(Foo{})
	if got := derefType(direct); got != direct {
		t.Errorf("expected non-pointer type returned as-is, got %v", got)
	}

	ptr := reflect.TypeOf((*Foo)(nil))
	if got := derefType(ptr); got != direct {
		t.Errorf("expected pointer type unwrapped to %v, got %v", direct, got)
	}
}

func TestIsRelationAuthorized(t *testing.T) {
	if !isRelationAuthorized(nil, "Posts") {
		t.Error("nil allowedIncludes should authorize everything")
	}

	includes := metadata.AllowedIncludes{"Posts": true, "Comments": false}

	if !isRelationAuthorized(includes, "Posts") {
		t.Error("expected Posts to be authorized")
	}
	if !isRelationAuthorized(includes, "Comments") {
		t.Error("expected Comments to be authorized")
	}
	if isRelationAuthorized(includes, "Tags") {
		t.Error("expected Tags to be unauthorized")
	}
}
