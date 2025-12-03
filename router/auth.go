package router

import "github.com/sjgoldie/go-restgen/metadata"

// AuthInfo is re-exported from metadata package for convenience
type AuthInfo = metadata.AuthInfo

// Context key for AuthInfo
const AuthInfoKey = "authInfo"

// HTTP methods for auth configuration
const (
	MethodGet    = "GET"  // Single item: GET /resources/{id}
	MethodList   = "LIST" // Collection: GET /resources
	MethodPost   = "POST"
	MethodPut    = "PUT"
	MethodDelete = "DELETE"
	MethodAll    = "ALL"
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

// expandMethods expands MethodAll to individual HTTP methods
func expandMethods(methods []string) []string {
	var expanded []string
	for _, method := range methods {
		if method == MethodAll {
			expanded = append(expanded, MethodGet, MethodList, MethodPost, MethodPut, MethodDelete)
		} else {
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
