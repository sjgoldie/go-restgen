package handler

// ListResponse is the envelope returned by GetAll endpoints.
// Data contains the result items. Pagination, Sums, and Counts are omitted when empty.
type ListResponse struct {
	Data       any                       `json:"data"`
	Pagination *PaginationInfo           `json:"pagination,omitempty"`
	Sums       map[string]float64        `json:"sums,omitempty"`
	Counts     map[string]map[string]int `json:"counts,omitempty"`
}

// PaginationInfo contains pagination metadata in the response envelope.
// For offset mode: Limit, Offset, and TotalCount are populated.
// For cursor mode: NextCursor, PrevCursor, HasMore, and TotalCount are populated.
type PaginationInfo struct {
	// Offset pagination fields
	Limit  *int `json:"limit,omitempty"`
	Offset *int `json:"offset,omitempty"`

	// Cursor pagination fields
	NextCursor *string `json:"next_cursor,omitempty"`
	PrevCursor *string `json:"prev_cursor,omitempty"`
	HasMore    *bool   `json:"has_more,omitempty"`

	// Common field
	TotalCount *int `json:"total_count,omitempty"`
}

// BatchResponse is the envelope returned by batch create/update/patch endpoints.
type BatchResponse struct {
	Data any `json:"data"`
}
