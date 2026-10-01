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
// Only GET is registered by default; the ID is resolved from the parent's FK field
type SingleRouteConfig struct {
	ParentFKField string // Field name on parent that holds the child's ID (e.g., "AuthorID")
	WithUpdate    bool   // If true, also register PUT and PATCH endpoints
}

// AsSingleRoute marks this as a single-object route with GET only
// parentFKField is the field name on the parent that holds this object's ID
// Example: AsSingleRoute("AuthorID") for /posts/{id}/author where Post.AuthorID holds User.ID
// A single route must be nested under its parent and name the parent's field; otherwise it is
// logged as a warning at registration and not registered. Use AsCurrentUser for the caller's
// own row.
func AsSingleRoute(parentFKField string) SingleRouteConfig {
	return SingleRouteConfig{ParentFKField: parentFKField}
}

// AsSingleRouteWithUpdate marks this as a single-object route with GET, PUT, and PATCH
// parentFKField is the field name on the parent that holds this object's ID
func AsSingleRouteWithUpdate(parentFKField string) SingleRouteConfig {
	return SingleRouteConfig{ParentFKField: parentFKField, WithUpdate: true}
}

// CurrentUserConfig marks a route as serving the caller's own row.
type CurrentUserConfig struct {
	Field string // Field matched against AuthInfo.UserID; empty for the primary key
}

// AsCurrentUser serves the caller's own row at the route's path: the row whose primary key is
// AuthInfo.UserID (use WithAlternatePK when the key field is not ID). The route's item routes
// (GET, PUT, PATCH, DELETE, actions, endpoints, SSE) are mounted at the path itself instead of
// under /{id}, and nested routes are scoped to that row as they would be under /{id}. The
// route's auth configs decide which methods are allowed; collection routes (list, create,
// batch) are not mounted. A request without a user ID gets 401.
//
// Example:
//
//	router.RegisterRoutes[User](b, "/me",
//	    router.AsCurrentUser(),
//	    router.AuthConfig{Methods: []string{router.MethodGet, router.MethodPatch}, Scopes: []string{router.ScopeAuthOnly}},
//	    func(b *router.Builder) {
//	        router.RegisterRoutes[Task](b, "/tasks", router.IsAuthenticated(), router.WithRelationName("Tasks"))
//	    },
//	)
func AsCurrentUser() CurrentUserConfig {
	return CurrentUserConfig{}
}

// AsCurrentUserExternal is AsCurrentUser for models keyed by their own primary key, where
// AuthInfo.UserID is held in another field, such as an identity provider's subject. The caller's
// row is the one whose field equals AuthInfo.UserID, scoped to the caller's tenant on tenant
// routes; the field must be unique. Updates always write AuthInfo.UserID to the field, so the
// caller cannot change it. A request with no matching row gets 404.
//
// Example:
//
//	router.RegisterRoutes[User](b, "/me",
//	    router.AsCurrentUserExternal("ExternalID"),
//	    router.AuthConfig{Methods: []string{router.MethodGet, router.MethodPut}, Scopes: []string{router.ScopeAuthOnly}},
//	)
func AsCurrentUserExternal(field string) CurrentUserConfig {
	return CurrentUserConfig{Field: field}
}
