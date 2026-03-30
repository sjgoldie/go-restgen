package router

import "github.com/sjgoldie/go-restgen/metadata"

// PaginationMode is an alias for metadata.PaginationMode for use in router options.
type PaginationMode = metadata.PaginationMode

const (
	// CursorMode uses cursor-based (keyset) pagination. This is the default.
	CursorMode = metadata.CursorPagination
	// OffsetMode uses traditional offset-based pagination.
	OffsetMode = metadata.OffsetPagination
)

// QueryConfig defines query parameter configuration for GetAll endpoints
type QueryConfig struct {
	FilterableFields []string       // Field names allowed for filtering
	SortableFields   []string       // Field names allowed for sorting
	SummableFields   []string       // Field names allowed for sum aggregation
	DefaultSort      string         // Default sort field (prefix with - for descending)
	DefaultLimit     int            // Default page size (0 = no limit)
	MaxLimit         int            // Maximum allowed limit (0 = no max)
	Pagination       PaginationMode // Pagination mode (CursorMode or OffsetMode)
}

// WithQuery returns a QueryConfig with all query options configured
func WithQuery(config QueryConfig) QueryConfig {
	return config
}

// WithFilters returns a QueryConfig that enables filtering on the specified fields
func WithFilters(fields ...string) QueryConfig {
	return QueryConfig{
		FilterableFields: fields,
	}
}

// WithSorts returns a QueryConfig that enables sorting on the specified fields
func WithSorts(fields ...string) QueryConfig {
	return QueryConfig{
		SortableFields: fields,
	}
}

// WithPagination returns a QueryConfig with pagination settings.
// Defaults to cursor-based pagination. Pass OffsetMode for offset-based pagination.
func WithPagination(defaultLimit, maxLimit int, mode ...PaginationMode) QueryConfig {
	m := CursorMode
	if len(mode) > 0 {
		m = mode[0]
	}
	return QueryConfig{
		DefaultLimit: defaultLimit,
		MaxLimit:     maxLimit,
		Pagination:   m,
	}
}

// WithDefaultSort returns a QueryConfig with a default sort field
// Prefix with - for descending (e.g., "-created_at")
func WithDefaultSort(field string) QueryConfig {
	return QueryConfig{
		DefaultSort: field,
	}
}

// WithSums returns a QueryConfig that enables sum aggregation on the specified fields.
// Any field with a numeric database column type (including struct-based types like decimal.Decimal)
// can be summed. The database validates the type — summing a non-numeric column returns a database error.
func WithSums(fields ...string) QueryConfig {
	return QueryConfig{
		SummableFields: fields,
	}
}
