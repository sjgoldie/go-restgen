package router

import (
	"context"
	"net/http"

	"github.com/sjgoldie/go-restgen/metadata"
)

// Context keys for ownership enforcement
const (
	ownershipEnforcedKey = "ownershipEnforced"
	ownershipUserIDKey   = "ownershipUserID"
	ownershipFieldsKey   = "ownershipFields"
)

// authStatus represents the result of an auth check
type authStatus int

const (
	authOK           authStatus = iota // Auth check passed
	authUnauthorized                   // No auth or no config (401)
	authForbidden                      // Auth exists but missing required scopes (403)
)

// authResult contains the result of an auth check
type authResult struct {
	Status         authStatus // Whether auth passed, or which error
	ApplyOwnership bool       // Whether ownership filtering should be applied
}

// checkAuth checks if the user is authorized for the given config.
// This is the core auth logic used by both route auth and include auth.
func checkAuth(authInfo *AuthInfo, config *AuthConfig) authResult {
	// Check for special public scope - no auth required
	if containsScope(config.Scopes, ScopePublic) {
		return authResult{Status: authOK, ApplyOwnership: false}
	}

	// Check for empty/nil scopes with no ownership - blocked (same as no config)
	if len(config.Scopes) == 0 && config.Ownership == nil {
		return authResult{Status: authUnauthorized}
	}

	// Check if auth info exists
	if authInfo == nil {
		return authResult{Status: authUnauthorized}
	}

	// Check scopes (unless ScopeAuthOnly or no scopes with ownership, which means auth is enough)
	if len(config.Scopes) > 0 && !containsScope(config.Scopes, ScopeAuthOnly) {
		if !hasAnyScope(authInfo.Scopes, config.Scopes) {
			return authResult{Status: authForbidden}
		}
	}

	// Auth passed - ownership applies if configured
	return authResult{Status: authOK, ApplyOwnership: config.Ownership != nil}
}

// wrapWithAuth wraps a handler with authentication and authorization checking based on AuthConfig
func wrapWithAuth(next http.Handler, config *AuthConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Extract AuthInfo from context (may be nil for unauthenticated requests)
		authInfo, _ := ctx.Value(AuthInfoKey).(*AuthInfo)

		// Build AllowedIncludes for child routes first
		// This must happen before the parent auth check so public parents still get child includes
		if len(config.ChildAuth) > 0 {
			allowedIncludes := make(metadata.AllowedIncludes)
			for relationName, childConfig := range config.ChildAuth {
				childResult := checkAuth(authInfo, childConfig)
				if childResult.Status == authOK {
					allowedIncludes[relationName] = childResult.ApplyOwnership
				}
			}
			ctx = context.WithValue(ctx, metadata.AllowedIncludesKey, allowedIncludes)
		}

		// Check parent route auth
		result := checkAuth(authInfo, config)
		switch result.Status {
		case authUnauthorized:
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		case authForbidden:
			http.Error(w, "forbidden", http.StatusForbidden)
			return
			// authOK falls through to continue processing
		}

		// Apply ownership context if checkAuth determined it should be applied
		if result.ApplyOwnership {
			ctx = applyOwnershipContext(ctx, authInfo, config.Ownership)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// blockUnauthorized returns 401 for any request (no auth config = blocked)
func blockUnauthorized(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

// applyOwnershipContext sets ownership context flags for all authenticated users
// The datastore layer will use these flags to:
// - Always populate ownership fields on create (even for admins)
// - Apply ownership filtering on reads (unless user has bypass scope)
func applyOwnershipContext(ctx context.Context, authInfo *AuthInfo, ownership *OwnershipConfig) context.Context {
	// Always set ownership context - this ensures ownership fields are populated on create
	// The datastore layer will check bypass scopes to skip filtering on reads
	ctx = context.WithValue(ctx, ownershipEnforcedKey, true)           //nolint:staticcheck // Using string keys for simplicity
	ctx = context.WithValue(ctx, ownershipUserIDKey, authInfo.UserID)  //nolint:staticcheck // Using string keys for simplicity
	ctx = context.WithValue(ctx, ownershipFieldsKey, ownership.Fields) //nolint:staticcheck // Using string keys for simplicity

	return ctx
}
