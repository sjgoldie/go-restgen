package common_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/sjgoldie/go-restgen/internal/common"
)

type testModel struct {
	ID       int    `bun:"id,pk,autoincrement"`
	Name     string `bun:"name,notnull"`
	AuthorID int    `bun:"author_id,notnull"`
}

type testUUIDModel struct {
	ID   uuid.UUID `bun:"id,pk"`
	Name string    `bun:"name"`
}

type testStringModel struct {
	ID   string `bun:"id,pk"`
	Name string `bun:"name"`
}

type testFloatModel struct {
	ID   float64 `bun:"id,pk"`
	Name string  `bun:"name"`
}

func TestSetFieldFromString(t *testing.T) {
	t.Run("int field", func(t *testing.T) {
		item := &testModel{}
		if err := common.SetFieldFromString(item, "ID", "42"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if item.ID != 42 {
			t.Errorf("expected 42, got %d", item.ID)
		}
	})

	t.Run("string field", func(t *testing.T) {
		item := &testStringModel{}
		if err := common.SetFieldFromString(item, "ID", "abc-123"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if item.ID != "abc-123" {
			t.Errorf("expected 'abc-123', got %q", item.ID)
		}
	})

	t.Run("uuid field", func(t *testing.T) {
		item := &testUUIDModel{}
		id := uuid.New()
		if err := common.SetFieldFromString(item, "ID", id.String()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if item.ID != id {
			t.Errorf("expected %s, got %s", id, item.ID)
		}
	})

	t.Run("invalid uuid", func(t *testing.T) {
		item := &testUUIDModel{}
		if err := common.SetFieldFromString(item, "ID", "not-a-uuid"); err == nil {
			t.Fatal("expected error for invalid UUID")
		}
	})

	t.Run("invalid int", func(t *testing.T) {
		item := &testModel{}
		if err := common.SetFieldFromString(item, "ID", "not-a-number"); err == nil {
			t.Fatal("expected error for invalid int")
		}
	})

	t.Run("unsupported type", func(t *testing.T) {
		item := &testFloatModel{}
		if err := common.SetFieldFromString(item, "ID", "1.5"); err == nil {
			t.Fatal("expected error for unsupported float type")
		}
	})

	t.Run("nonexistent field", func(t *testing.T) {
		item := &testModel{}
		if err := common.SetFieldFromString(item, "Nonexistent", "value"); err == nil {
			t.Fatal("expected error for nonexistent field")
		}
	})
}

func TestGetFieldAsString(t *testing.T) {
	t.Run("int field", func(t *testing.T) {
		item := &testModel{ID: 42}
		result := common.GetFieldAsString(item, "ID")
		if result != "42" {
			t.Errorf("expected '42', got %q", result)
		}
	})

	t.Run("string field", func(t *testing.T) {
		item := &testStringModel{ID: "abc-123"}
		result := common.GetFieldAsString(item, "ID")
		if result != "abc-123" {
			t.Errorf("expected 'abc-123', got %q", result)
		}
	})

	t.Run("uuid field", func(t *testing.T) {
		id := uuid.New()
		item := &testUUIDModel{ID: id}
		result := common.GetFieldAsString(item, "ID")
		if result != id.String() {
			t.Errorf("expected %q, got %q", id.String(), result)
		}
	})

	t.Run("nonexistent field", func(t *testing.T) {
		item := &testModel{ID: 1}
		result := common.GetFieldAsString(item, "Nonexistent")
		if result != "" {
			t.Errorf("expected empty string, got %q", result)
		}
	})

	t.Run("non-pointer value", func(t *testing.T) {
		item := testModel{ID: 99}
		result := common.GetFieldAsString(item, "ID")
		if result != "99" {
			t.Errorf("expected '99', got %q", result)
		}
	})
}
