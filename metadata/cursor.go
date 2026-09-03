package metadata

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"time"

	apperrors "github.com/sjgoldie/go-restgen/errors"
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
	Values []any
	// PK holds the primary key value used as the tie-breaker.
	PK any
}

// Cursor value type tags. Integers and times are carried as strings so they
// survive the JSON round trip without float64 precision loss or a lossy
// conversion to text that then relies on database-side casting.
const (
	cursorTypeInt    = "i"
	cursorTypeUint   = "u"
	cursorTypeFloat  = "f"
	cursorTypeString = "s"
	cursorTypeBool   = "b"
	cursorTypeTime   = "t"
	cursorTypeJSON   = "j"
)

// cursorValue is the wire form of a single cursor value.
type cursorValue struct {
	Type  string `json:"t"`
	Value string `json:"v"`
}

// cursorWire is the JSON shape of an encoded cursor.
type cursorWire struct {
	Values []cursorValue `json:"v"`
	PK     *cursorValue  `json:"pk"`
}

var timeType = reflect.TypeOf(time.Time{})

// encodeCursorValue converts a Go value into its typed wire form.
func encodeCursorValue(v any) (cursorValue, error) {
	rv := reflect.ValueOf(v)
	for rv.IsValid() && rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return cursorValue{Type: cursorTypeJSON, Value: "null"}, nil
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() {
		return cursorValue{Type: cursorTypeJSON, Value: "null"}, nil
	}

	if rv.Type() == timeType {
		t := rv.Interface().(time.Time)
		return cursorValue{Type: cursorTypeTime, Value: t.Format(time.RFC3339Nano)}, nil
	}

	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return cursorValue{Type: cursorTypeInt, Value: strconv.FormatInt(rv.Int(), 10)}, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return cursorValue{Type: cursorTypeUint, Value: strconv.FormatUint(rv.Uint(), 10)}, nil
	case reflect.Float32, reflect.Float64:
		return cursorValue{Type: cursorTypeFloat, Value: strconv.FormatFloat(rv.Float(), 'g', -1, 64)}, nil
	case reflect.String:
		return cursorValue{Type: cursorTypeString, Value: rv.String()}, nil
	case reflect.Bool:
		return cursorValue{Type: cursorTypeBool, Value: strconv.FormatBool(rv.Bool())}, nil
	default:
		data, err := json.Marshal(rv.Interface())
		if err != nil {
			return cursorValue{}, fmt.Errorf("encoding cursor value: %w", err)
		}
		return cursorValue{Type: cursorTypeJSON, Value: string(data)}, nil
	}
}

// decodeCursorValue restores a Go value from its typed wire form.
func decodeCursorValue(cv cursorValue) (any, error) {
	switch cv.Type {
	case cursorTypeInt:
		return strconv.ParseInt(cv.Value, 10, 64)
	case cursorTypeUint:
		return strconv.ParseUint(cv.Value, 10, 64)
	case cursorTypeFloat:
		return strconv.ParseFloat(cv.Value, 64)
	case cursorTypeString:
		return cv.Value, nil
	case cursorTypeBool:
		return strconv.ParseBool(cv.Value)
	case cursorTypeTime:
		return time.Parse(time.RFC3339Nano, cv.Value)
	case cursorTypeJSON:
		var v any
		if err := json.Unmarshal([]byte(cv.Value), &v); err != nil {
			return nil, err
		}
		return v, nil
	default:
		return nil, fmt.Errorf("unknown cursor value type %q", cv.Type)
	}
}

// EncodeCursor serialises a Cursor to a URL-safe, opaque base64 string.
func EncodeCursor(c Cursor) (string, error) {
	wire := cursorWire{Values: make([]cursorValue, 0, len(c.Values))}
	for _, v := range c.Values {
		cv, err := encodeCursorValue(v)
		if err != nil {
			return "", fmt.Errorf("encoding cursor: %w", err)
		}
		wire.Values = append(wire.Values, cv)
	}
	if c.PK != nil {
		pk, err := encodeCursorValue(c.PK)
		if err != nil {
			return "", fmt.Errorf("encoding cursor: %w", err)
		}
		wire.PK = &pk
	}

	data, err := json.Marshal(wire)
	if err != nil {
		return "", fmt.Errorf("encoding cursor: %w", err)
	}
	return base64.URLEncoding.EncodeToString(data), nil
}

// DecodeCursor deserialises a base64 cursor string back into a Cursor.
// Every failure wraps errors.ErrInvalidCursor so handlers can answer 400:
// bad base64, malformed JSON, an unknown value type, or a missing primary key.
func DecodeCursor(encoded string) (Cursor, error) {
	data, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		return Cursor{}, fmt.Errorf("%w: bad encoding", apperrors.ErrInvalidCursor)
	}

	var wire cursorWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return Cursor{}, fmt.Errorf("%w: malformed data", apperrors.ErrInvalidCursor)
	}

	if wire.PK == nil {
		return Cursor{}, fmt.Errorf("%w: missing primary key", apperrors.ErrInvalidCursor)
	}

	var c Cursor
	for _, cv := range wire.Values {
		v, err := decodeCursorValue(cv)
		if err != nil {
			return Cursor{}, fmt.Errorf("%w: malformed value", apperrors.ErrInvalidCursor)
		}
		c.Values = append(c.Values, v)
	}
	pk, err := decodeCursorValue(*wire.PK)
	if err != nil {
		return Cursor{}, fmt.Errorf("%w: malformed primary key", apperrors.ErrInvalidCursor)
	}
	c.PK = pk

	return c, nil
}
