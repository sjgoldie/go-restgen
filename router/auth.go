package router

import "github.com/sjgoldie/go-restgen/metadata"

// AuthInfo is re-exported from metadata package for convenience
type AuthInfo = metadata.AuthInfo

// AuthInfoKey is re-exported from metadata package for convenience
const AuthInfoKey = metadata.AuthInfoKey

// HTTP methods for auth configuration
const (
	MethodGet    = "GET"  // Single item: GET /resources/{id}
	MethodList   = "LIST" // Collection: GET /resources
	MethodPost   = "POST"
	MethodPut    = "PUT"
	MethodDelete = "DELETE"
	MethodAll    = "ALL" // Expands to single operations only (GET, LIST, POST, PUT, DELETE)

	// Batch methods for bulk operations via /resources/batch
	MethodBatchCreate = "BATCH_CREATE" // POST /resources/batch
	MethodBatchUpdate = "BATCH_UPDATE" // PUT /resources/batch
	MethodBatchDelete = "BATCH_DELETE" // DELETE /resources/batch

	// MethodAllWithBatch expands to all methods including batch operations
	MethodAllWithBatch = "ALL_WITH_BATCH"
)

// Special internal scopes (prefixed to avoid clashing with user-defined scopes)
const (
	ScopePublic   = "__restgen_public__"    // No authentication required
	ScopeAuthOnly = "__restgen_auth_only__" // Authentication required, no scope check
)

// AuthConfig defines authentication and authorization requirements per operation.
// Multiple AuthConfigs can be provided - last one wins for each method (MethodAll expands to individual methods).
type AuthConfig struct {
	Methods   []string         // HTTP methods this config applies to (MethodGet, MethodPost, etc.)
	Scopes    []string         // Required scopes - user must have at least one (empty/nil = blocked)
	Ownership *OwnershipConfig // Optional ownership enforcement

	// ChildAuth holds auth configs for child routes (used for ?include= authorization).
	// Populated automatically when child routes are registered with WithRelationName().
	// Key is the relation name (e.g., "Posts"), value is the child's merged auth config for MethodGet.
	ChildAuth map[string]*AuthConfig

	// ParentAuth points to the parent route's GET auth config (for ?include= authorization of
	// parent types in the belongs-to direction). Forms a linked list up the parent chain.
	// Populated automatically during route registration.
	ParentAuth *AuthConfig

	// ParentIncludeName is the belongs-to relation field name used in ?include= paths
	// (e.g., "Author" for a Post that belongs to Author). Derived from the model's struct
	// field during route registration.
	ParentIncludeName string
}

// OwnershipConfig defines ownership validation rules.
// Ownership is enforced by filtering queries and validating mutations against the authenticated user's ID.
type OwnershipConfig struct {
	Fields       []string // Model field names to check for ownership (OR logic - user owns if ANY field matches)
	BypassScopes []string // Scopes that bypass ownership check (e.g., "admin")
}

// mergeAuthConfigs processes multiple AuthConfigs and creates a per-method map.
// Last config wins for each method. MethodAll expands to individual methods.
func mergeAuthConfigs(configs []AuthConfig) map[string]*AuthConfig {
	result := make(map[string]*AuthConfig)
	for i := range configs {
		methods := expandMethods(configs[i].Methods)
		for _, method := range methods {
			// Create a copy to avoid pointer issues
			configCopy := configs[i]
			result[method] = &configCopy
		}
	}
	return result
}

// expandMethods expands MethodAll and MethodAllWithBatch to individual HTTP methods
func expandMethods(methods []string) []string {
	var expanded []string
	for _, method := range methods {
		switch method {
		case MethodAll:
			// MethodAll expands to single operations only (no batch)
			expanded = append(expanded, MethodGet, MethodList, MethodPost, MethodPut, MethodDelete)
		case MethodAllWithBatch:
			// MethodAllWithBatch expands to all methods including batch
			expanded = append(expanded, MethodGet, MethodList, MethodPost, MethodPut, MethodDelete,
				MethodBatchCreate, MethodBatchUpdate, MethodBatchDelete)
		default:
			expanded = append(expanded, method)
		}
	}
	return expanded
}

// containsScope checks if a scope exists in a slice
func containsScope(scopes []string, scope string) bool {
	for _, s := range scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// hasAnyScope checks if user has at least one of the required scopes
func hasAnyScope(userScopes, requiredScopes []string) bool {
	for _, required := range requiredScopes {
		for _, userScope := range userScopes {
			if userScope == required {
				return true
			}
		}
	}
	return false
}

// Convenience functions for common auth patterns

// AllPublic returns an AuthConfig that allows public access to all methods
func AllPublic() AuthConfig {
	return AuthConfig{
		Methods: []string{MethodAll},
		Scopes:  []string{ScopePublic},
	}
}

// IsAuthenticated returns an AuthConfig that requires authentication for all methods (no specific scopes)
func IsAuthenticated() AuthConfig {
	return AuthConfig{
		Methods: []string{MethodAll},
		Scopes:  []string{ScopeAuthOnly},
	}
}

// AllScoped returns an AuthConfig that requires specific scope(s) for all methods
func AllScoped(scopes ...string) AuthConfig {
	return AuthConfig{
		Methods: []string{MethodAll},
		Scopes:  scopes,
	}
}

// AllWithOwnershipUnless returns an AuthConfig with ownership enforcement for all methods and bypass scopes
func AllWithOwnershipUnless(fields []string, bypassScopes ...string) AuthConfig {
	return AuthConfig{
		Methods: []string{MethodAll},
		Ownership: &OwnershipConfig{
			Fields:       fields,
			BypassScopes: bypassScopes,
		},
	}
}

// PublicReadOnly returns an AuthConfig that allows public read access (both List and Get)
func PublicReadOnly() AuthConfig {
	return AuthConfig{
		Methods: []string{MethodGet, MethodList},
		Scopes:  []string{ScopePublic},
	}
}

// PublicList returns an AuthConfig that allows public LIST access only (collection endpoint)
func PublicList() AuthConfig {
	return AuthConfig{
		Methods: []string{MethodList},
		Scopes:  []string{ScopePublic},
	}
}

// PublicGet returns an AuthConfig that allows public GET access only (single item endpoint)
func PublicGet() AuthConfig {
	return AuthConfig{
		Methods: []string{MethodGet},
		Scopes:  []string{ScopePublic},
	}
}

// AllPublicWithBatch returns an AuthConfig that allows public access to all methods including batch
func AllPublicWithBatch() AuthConfig {
	return AuthConfig{
		Methods: []string{MethodAllWithBatch},
		Scopes:  []string{ScopePublic},
	}
}

// AllScopedWithBatch returns an AuthConfig that requires specific scope(s) for all methods including batch
func AllScopedWithBatch(scopes ...string) AuthConfig {
	return AuthConfig{
		Methods: []string{MethodAllWithBatch},
		Scopes:  scopes,
	}
}

// hasBatchMethods checks if any batch methods are configured in the auth map
func hasBatchMethods(authMap map[string]*AuthConfig) bool {
	return authMap[MethodBatchCreate] != nil ||
		authMap[MethodBatchUpdate] != nil ||
		authMap[MethodBatchDelete] != nil
}
