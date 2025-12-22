package router

// RelationConfig defines the relation name for ?include= support
type RelationConfig struct {
	Name string // The field name on the parent struct (e.g., "Comments")
}

// WithRelationName configures the relation name for this child route
// This maps to a field on the parent struct for ?include= support
// e.g., WithRelationName("Comments") maps to Parent.Comments field
func WithRelationName(name string) RelationConfig {
	return RelationConfig{
		Name: name,
	}
}
