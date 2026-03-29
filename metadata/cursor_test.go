package metadata

import (
	"net/url"
	"testing"
)

func TestEncodeCursor_RoundTrip(t *testing.T) {
	original := Cursor{
		Values: []any{float64(42), "hello"},
		PK:     float64(7),
	}

	encoded, err := EncodeCursor(original)
	if err != nil {
		t.Fatalf("EncodeCursor failed: %v", err)
	}

	if encoded == "" {
		t.Fatal("EncodeCursor returned empty string")
	}

	decoded, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeCursor failed: %v", err)
	}

	if len(decoded.Values) != 2 {
		t.Fatalf("Expected 2 values, got %d", len(decoded.Values))
	}

	if decoded.Values[0].(float64) != 42 {
		t.Errorf("Expected first value 42, got %v", decoded.Values[0])
	}
	if decoded.Values[1].(string) != "hello" {
		t.Errorf("Expected second value 'hello', got %v", decoded.Values[1])
	}
	if decoded.PK.(float64) != 7 {
		t.Errorf("Expected PK 7, got %v", decoded.PK)
	}
}

func TestEncodeCursor_EmptyValues(t *testing.T) {
	original := Cursor{
		Values: []any{},
		PK:     float64(1),
	}

	encoded, err := EncodeCursor(original)
	if err != nil {
		t.Fatalf("EncodeCursor failed: %v", err)
	}

	decoded, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeCursor failed: %v", err)
	}

	if len(decoded.Values) != 0 {
		t.Errorf("Expected 0 values, got %d", len(decoded.Values))
	}
	if decoded.PK.(float64) != 1 {
		t.Errorf("Expected PK 1, got %v", decoded.PK)
	}
}

func TestDecodeCursor_InvalidBase64(t *testing.T) {
	_, err := DecodeCursor("not-valid-base64!!!")
	if err == nil {
		t.Error("Expected error for invalid base64")
	}
}

func TestDecodeCursor_MalformedJSON(t *testing.T) {
	// Valid base64 but not valid JSON
	_, err := DecodeCursor("bm90LWpzb24=") // "not-json" in base64
	if err == nil {
		t.Error("Expected error for malformed JSON")
	}
}

func TestDecodeCursor_MissingPK(t *testing.T) {
	// Valid base64 JSON but missing pk field
	_, err := DecodeCursor("eyJ2IjpbMV19") // {"v":[1]} in base64
	if err == nil {
		t.Error("Expected error for missing PK")
	}
}

func TestDecodeCursor_EmptyString(t *testing.T) {
	_, err := DecodeCursor("")
	if err == nil {
		t.Error("Expected error for empty cursor string")
	}
}

func TestDecodeCursor_StringPK(t *testing.T) {
	original := Cursor{
		Values: []any{"some-sort-value"},
		PK:     "uuid-pk-value",
	}

	encoded, err := EncodeCursor(original)
	if err != nil {
		t.Fatalf("EncodeCursor failed: %v", err)
	}

	decoded, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeCursor failed: %v", err)
	}

	if decoded.PK.(string) != "uuid-pk-value" {
		t.Errorf("Expected PK 'uuid-pk-value', got %v", decoded.PK)
	}
}

func TestDecodeCursor_NilValues(t *testing.T) {
	original := Cursor{
		PK: float64(1),
	}

	encoded, err := EncodeCursor(original)
	if err != nil {
		t.Fatalf("EncodeCursor failed: %v", err)
	}

	decoded, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeCursor failed: %v", err)
	}

	if decoded.Values != nil {
		t.Errorf("Expected nil values, got %v", decoded.Values)
	}
}

func TestParseQueryOptions_CursorParams(t *testing.T) {
	tests := []struct {
		name           string
		query          url.Values
		expectedAfter  string
		expectedBefore string
	}{
		{
			name:           "after cursor",
			query:          url.Values{"after": {"abc123"}},
			expectedAfter:  "abc123",
			expectedBefore: "",
		},
		{
			name:           "before cursor",
			query:          url.Values{"before": {"xyz789"}},
			expectedAfter:  "",
			expectedBefore: "xyz789",
		},
		{
			name:           "no cursors",
			query:          url.Values{},
			expectedAfter:  "",
			expectedBefore: "",
		},
		{
			name:           "cursor with other params",
			query:          url.Values{"after": {"cursor1"}, "limit": {"20"}, "sort": {"name"}},
			expectedAfter:  "cursor1",
			expectedBefore: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := ParseQueryOptions(tt.query)

			if opts.After != tt.expectedAfter {
				t.Errorf("After: expected %q, got %q", tt.expectedAfter, opts.After)
			}
			if opts.Before != tt.expectedBefore {
				t.Errorf("Before: expected %q, got %q", tt.expectedBefore, opts.Before)
			}
		})
	}
}
