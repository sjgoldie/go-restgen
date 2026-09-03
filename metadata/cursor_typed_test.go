package metadata

import (
	"errors"
	"math"
	"testing"
	"time"

	apperrors "github.com/sjgoldie/go-restgen/errors"
)

func roundTrip(t *testing.T, c Cursor) Cursor {
	t.Helper()
	encoded, err := EncodeCursor(c)
	if err != nil {
		t.Fatalf("EncodeCursor: %v", err)
	}
	decoded, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeCursor: %v", err)
	}
	return decoded
}

func TestCursor_Int64AbovePrecisionLimitSurvives(t *testing.T) {
	big := int64(math.MaxInt64 - 1)
	decoded := roundTrip(t, Cursor{Values: []any{big}, PK: int64(9007199254740993)})

	if got, ok := decoded.Values[0].(int64); !ok || got != big {
		t.Errorf("sort value: got %v (%T), want %d (int64)", decoded.Values[0], decoded.Values[0], big)
	}
	if got, ok := decoded.PK.(int64); !ok || got != 9007199254740993 {
		t.Errorf("pk: got %v (%T), want 9007199254740993 (int64)", decoded.PK, decoded.PK)
	}
}

func TestCursor_SmallIntsDecodeAsInt64(t *testing.T) {
	decoded := roundTrip(t, Cursor{Values: []any{int(7), int32(8)}, PK: int(1)})

	for i, want := range []int64{7, 8} {
		if got, ok := decoded.Values[i].(int64); !ok || got != want {
			t.Errorf("value %d: got %v (%T), want %d (int64)", i, decoded.Values[i], decoded.Values[i], want)
		}
	}
	if got, ok := decoded.PK.(int64); !ok || got != 1 {
		t.Errorf("pk: got %v (%T), want 1 (int64)", decoded.PK, decoded.PK)
	}
}

func TestCursor_UintAndFloatAndBool(t *testing.T) {
	decoded := roundTrip(t, Cursor{Values: []any{uint64(math.MaxUint64), 3.25, true}, PK: uint8(2)})

	if got, ok := decoded.Values[0].(uint64); !ok || got != math.MaxUint64 {
		t.Errorf("uint: got %v (%T)", decoded.Values[0], decoded.Values[0])
	}
	if got, ok := decoded.Values[1].(float64); !ok || got != 3.25 {
		t.Errorf("float: got %v (%T)", decoded.Values[1], decoded.Values[1])
	}
	if got, ok := decoded.Values[2].(bool); !ok || !got {
		t.Errorf("bool: got %v (%T)", decoded.Values[2], decoded.Values[2])
	}
	if got, ok := decoded.PK.(uint64); !ok || got != 2 {
		t.Errorf("pk: got %v (%T)", decoded.PK, decoded.PK)
	}
}

func TestCursor_TimeSurvivesAsTime(t *testing.T) {
	stamp := time.Date(2026, 9, 3, 14, 5, 6, 789012345, time.FixedZone("AEST", 10*3600))
	decoded := roundTrip(t, Cursor{Values: []any{stamp, &stamp}, PK: "k"})

	for i := range 2 {
		got, ok := decoded.Values[i].(time.Time)
		if !ok {
			t.Fatalf("value %d: got %T, want time.Time", i, decoded.Values[i])
		}
		if !got.Equal(stamp) {
			t.Errorf("value %d: got %v, want %v", i, got, stamp)
		}
	}
	if got, ok := decoded.PK.(string); !ok || got != "k" {
		t.Errorf("pk: got %v (%T)", decoded.PK, decoded.PK)
	}
}

type namedStatus string

func TestCursor_NamedStringAndNilPointer(t *testing.T) {
	var nilTime *time.Time
	decoded := roundTrip(t, Cursor{Values: []any{namedStatus("open"), nilTime}, PK: "x"})

	if got, ok := decoded.Values[0].(string); !ok || got != "open" {
		t.Errorf("named string: got %v (%T)", decoded.Values[0], decoded.Values[0])
	}
	if decoded.Values[1] != nil {
		t.Errorf("nil pointer: got %v, want nil", decoded.Values[1])
	}
}

func TestCursor_StructFallsBackToJSON(t *testing.T) {
	type point struct {
		X int `json:"x"`
	}
	decoded := roundTrip(t, Cursor{Values: []any{point{X: 4}}, PK: 1})

	m, ok := decoded.Values[0].(map[string]any)
	if !ok {
		t.Fatalf("struct: got %T, want map[string]any", decoded.Values[0])
	}
	if m["x"] != float64(4) {
		t.Errorf("struct field: got %v", m["x"])
	}
}

func TestDecodeCursor_FailuresWrapErrInvalidCursor(t *testing.T) {
	cases := map[string]string{
		"bad base64":       "not-base64!",
		"malformed json":   "e30",
		"missing pk":       "eyJ2IjpbXX0=",
		"unknown type tag": "eyJ2IjpbXSwicGsiOnsidCI6InoiLCJ2IjoiMSJ9fQ==",
		"bad int value":    "eyJ2IjpbeyJ0IjoiaSIsInYiOiJhYmMifV0sInBrIjp7InQiOiJpIiwidiI6IjEifX0=",
	}
	for name, encoded := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeCursor(encoded)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !errors.Is(err, apperrors.ErrInvalidCursor) {
				t.Errorf("expected ErrInvalidCursor, got %v", err)
			}
		})
	}
}
