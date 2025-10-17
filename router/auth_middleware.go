package router

import (
	"context"
	"net/http"
)

// Context keys for ownership enforcement
const (
	ownershipEnforcedKey = "ownershipEnforced"
	ownershipUserIDKey   = "ownershipUserID"
	ownershipFieldsKey   = "ownershipFields"
)

// wrapWithAuth wraps a handler with authentication and authorization checking based on AuthConfig
func wrapWithAuth(next http.Handler, config *AuthConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for special public scope - no auth required
		if containsScope(config.Scopes, ScopePublic) {
			next.ServeHTTP(w, r)
			return
		}

		// Check for empty/nil scopes with no ownership - blocked (same as no config)
		if len(config.Scopes) == 0 && config.Ownership == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// Extract AuthInfo from context
		authInfo, ok := r.Context().Value(AuthInfoKey).(*AuthInfo)
		if !ok || authInfo == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// Check scopes (unless ScopeAuthOnly or no scopes with ownership, which means auth is enough)
		if len(config.Scopes) > 0 && !containsScope(config.Scopes, ScopeAuthOnly) {
			if !hasAnyScope(authInfo.Scopes, config.Scopes) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}

		// Handle ownership if configured
		ctx := r.Context()
		if config.Ownership != nil {
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

// applyOwnershipContext checks bypass scopes and sets ownership context flags
// The datastore layer will use these flags to enforce ownership filtering
func applyOwnershipContext(ctx context.Context, authInfo *AuthInfo, ownership *OwnershipConfig) context.Context {
	// Check if user has bypass scope (e.g., admin)
	if hasAnyScope(authInfo.Scopes, ownership.BypassScopes) {
		// User bypasses ownership - don't enforce
		return ctx
	}

	// Enforce ownership by setting context flags
	ctx = context.WithValue(ctx, ownershipEnforcedKey, true)
	ctx = context.WithValue(ctx, ownershipUserIDKey, authInfo.UserID)
	ctx = context.WithValue(ctx, ownershipFieldsKey, ownership.Fields)
	return ctx
}
