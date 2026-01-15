//nolint:goconst // Test file - repeated test strings are acceptable
package router

import (
	"testing"
)

func TestWithFilters(t *testing.T) {
	config := WithFilters("Name", "Email", "Status")

	if len(config.FilterableFields) != 3 {
		t.Errorf("expected 3 filterable fields, got %d", len(config.FilterableFields))
	}
	if config.FilterableFields[0] != "Name" {
		t.Errorf("expected first field to be 'Name', got '%s'", config.FilterableFields[0])
	}
	if config.FilterableFields[1] != "Email" {
		t.Errorf("expected second field to be 'Email', got '%s'", config.FilterableFields[1])
	}
	if config.FilterableFields[2] != "Status" {
		t.Errorf("expected third field to be 'Status', got '%s'", config.FilterableFields[2])
	}
}

func TestWithSorts(t *testing.T) {
	config := WithSorts("Name", "CreatedAt")

	if len(config.SortableFields) != 2 {
		t.Errorf("expected 2 sortable fields, got %d", len(config.SortableFields))
	}
	if config.SortableFields[0] != "Name" {
		t.Errorf("expected first field to be 'Name', got '%s'", config.SortableFields[0])
	}
	if config.SortableFields[1] != "CreatedAt" {
		t.Errorf("expected second field to be 'CreatedAt', got '%s'", config.SortableFields[1])
	}
}

func TestWithPagination(t *testing.T) {
	config := WithPagination(20, 100)

	if config.DefaultLimit != 20 {
		t.Errorf("expected default limit 20, got %d", config.DefaultLimit)
	}
	if config.MaxLimit != 100 {
		t.Errorf("expected max limit 100, got %d", config.MaxLimit)
	}
}

func TestWithDefaultSort(t *testing.T) {
	tests := []struct {
		name     string
		field    string
		expected string
	}{
		{"ascending", "Name", "Name"},
		{"descending", "-CreatedAt", "-CreatedAt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := WithDefaultSort(tt.field)
			if config.DefaultSort != tt.expected {
				t.Errorf("expected default sort '%s', got '%s'", tt.expected, config.DefaultSort)
			}
		})
	}
}

func TestWithQuery(t *testing.T) {
	input := QueryConfig{
		FilterableFields: []string{"Name", "Email"},
		SortableFields:   []string{"Name", "CreatedAt"},
		DefaultSort:      "-CreatedAt",
		DefaultLimit:     25,
		MaxLimit:         50,
	}

	config := WithQuery(input)

	if len(config.FilterableFields) != 2 {
		t.Errorf("expected 2 filterable fields, got %d", len(config.FilterableFields))
	}
	if len(config.SortableFields) != 2 {
		t.Errorf("expected 2 sortable fields, got %d", len(config.SortableFields))
	}
	if config.DefaultSort != "-CreatedAt" {
		t.Errorf("expected default sort '-CreatedAt', got '%s'", config.DefaultSort)
	}
	if config.DefaultLimit != 25 {
		t.Errorf("expected default limit 25, got %d", config.DefaultLimit)
	}
	if config.MaxLimit != 50 {
		t.Errorf("expected max limit 50, got %d", config.MaxLimit)
	}
}
