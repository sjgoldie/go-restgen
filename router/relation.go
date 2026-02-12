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

// JoinOnConfig specifies custom join columns for non-FK relationships.
// Use this when the child has no belongs-to tag pointing to the parent
// and the join is on a shared attribute (e.g., NMI) rather than the parent's PK.
type JoinOnConfig struct {
	ChildCol  string // Column on child table (e.g., "nmi")
	ParentCol string // Column on parent table (e.g., "nmi")
}

// WithJoinOn configures custom join columns for this child route.
// Used when the relationship is through a shared attribute rather than a foreign key.
// Example: WithJoinOn("NMI", "NMI") joins usage_data.nmi = sites.nmi
func WithJoinOn(childCol, parentCol string) JoinOnConfig {
	return JoinOnConfig{ChildCol: childCol, ParentCol: parentCol}
}

// SingleRouteConfig marks this route as a single-object route (not a collection)
// Used for belongs-to relations like /posts/{id}/author
// Only GET is registered; the ID is resolved from the parent's FK field
type SingleRouteConfig struct {
	ParentFKField string // Field name on parent that holds the child's ID (e.g., "AuthorID")
	WithPut       bool   // If true, also register PUT endpoint
}

// AsSingleRoute marks this as a single-object route with GET only
// parentFKField is the field name on the parent that holds this object's ID
// Example: AsSingleRoute("AuthorID") for /posts/{id}/author where Post.AuthorID holds User.ID
// Pass empty string for routes like /api/me where ID comes from auth context
func AsSingleRoute(parentFKField string) SingleRouteConfig {
	return SingleRouteConfig{ParentFKField: parentFKField, WithPut: false}
}

// AsSingleRouteWithPut marks this as a single-object route with GET and PUT
// parentFKField is the field name on the parent that holds this object's ID
func AsSingleRouteWithPut(parentFKField string) SingleRouteConfig {
	return SingleRouteConfig{ParentFKField: parentFKField, WithPut: true}
}
