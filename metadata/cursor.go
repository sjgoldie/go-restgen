package metadata

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// CursorInfo holds pagination cursors returned alongside list results.
// For cursor-based pagination, NextCursor and PrevCursor are populated.
// HasMore indicates whether there are more items beyond the current page.
type CursorInfo struct {
	NextCursor string // opaque cursor for the next page (empty if no next page)
	PrevCursor string // opaque cursor for the previous page (empty if no previous page)
	HasMore    bool   // true if there are more items beyond the current page
}

// Cursor holds the encoded position for cursor-based pagination.
// It contains the sort field values and primary key value of the last/first item
// in the current page, enabling efficient keyset pagination.
type Cursor struct {
	// Values holds the sort column values in the same order as the ORDER BY clause.
	Values []any `json:"v"`
	// PK holds the primary key value used as the tie-breaker.
	PK any `json:"pk"`
}

// EncodeCursor serialises a Cursor to a URL-safe, opaque base64 string.
func EncodeCursor(c Cursor) (string, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("encoding cursor: %w", err)
	}
	return base64.URLEncoding.EncodeToString(data), nil
}

// DecodeCursor deserialises a base64 cursor string back into a Cursor.
// Returns an error for invalid base64, malformed JSON, or missing fields.
func DecodeCursor(encoded string) (Cursor, error) {
	data, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		return Cursor{}, fmt.Errorf("invalid cursor: bad encoding")
	}

	var c Cursor
	if err := json.Unmarshal(data, &c); err != nil {
		return Cursor{}, fmt.Errorf("invalid cursor: malformed data")
	}

	if c.PK == nil {
		return Cursor{}, fmt.Errorf("invalid cursor: missing primary key")
	}

	return c, nil
}
