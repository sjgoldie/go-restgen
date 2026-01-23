package router

// QueryConfig defines query parameter configuration for GetAll endpoints
type QueryConfig struct {
	FilterableFields []string // Field names allowed for filtering
	SortableFields   []string // Field names allowed for sorting
	SummableFields   []string // Field names allowed for sum aggregation
	DefaultSort      string   // Default sort field (prefix with - for descending)
	DefaultLimit     int      // Default page size (0 = no limit)
	MaxLimit         int      // Maximum allowed limit (0 = no max)
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

// WithPagination returns a QueryConfig with pagination settings
func WithPagination(defaultLimit, maxLimit int) QueryConfig {
	return QueryConfig{
		DefaultLimit: defaultLimit,
		MaxLimit:     maxLimit,
	}
}

// WithDefaultSort returns a QueryConfig with a default sort field
// Prefix with - for descending (e.g., "-created_at")
func WithDefaultSort(field string) QueryConfig {
	return QueryConfig{
		DefaultSort: field,
	}
}

// WithSums returns a QueryConfig that enables sum aggregation on the specified fields
// Only numeric fields (int, float, uint) can be summed; non-numeric fields return 0
func WithSums(fields ...string) QueryConfig {
	return QueryConfig{
		SummableFields: fields,
	}
}
