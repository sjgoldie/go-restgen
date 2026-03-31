package router

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/sjgoldie/go-restgen/handler"
	"github.com/sjgoldie/go-restgen/metadata"
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

	// Issue #24 fix: If auth is required (ScopeAuthOnly or ownership), verify UserID is non-empty.
	// This catches the case where middleware sets AuthInfo{} but doesn't populate UserID.
	authRequired := containsScope(config.Scopes, ScopeAuthOnly) || config.Ownership != nil
	if authRequired && authInfo.UserID == "" {
		return authResult{Status: authUnauthorized}
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

		// Build AllowedIncludes for child and parent routes
		// This must happen before the parent auth check so public parents still get child includes
		allowedIncludes := make(metadata.AllowedIncludes)

		if len(config.ChildAuth) > 0 {
			buildChildAllowedIncludes(authInfo, config.ChildAuth, "", false, allowedIncludes)
		}

		if config.ParentAuth != nil {
			buildParentAllowedIncludes(authInfo, config, allowedIncludes)
		}

		if len(allowedIncludes) > 0 {
			ctx = context.WithValue(ctx, metadata.AllowedIncludesKey, allowedIncludes)
		}

		// Check parent route auth
		result := checkAuth(authInfo, config)
		switch result.Status {
		case authUnauthorized:
			slog.WarnContext(ctx, "auth rejected: unauthorized", "path", r.URL.Path, "method", r.Method)
			handler.WriteError(w, http.StatusUnauthorized, handler.ErrCodeUnauthorized, http.StatusText(http.StatusUnauthorized))
			return
		case authForbidden:
			slog.WarnContext(ctx, "auth rejected: forbidden", "path", r.URL.Path, "method", r.Method)
			handler.WriteError(w, http.StatusForbidden, handler.ErrCodeForbidden, http.StatusText(http.StatusForbidden))
			return
			// authOK falls through to continue processing
		}

		// Apply ownership context if checkAuth determined it should be applied
		if result.ApplyOwnership {
			ctx = applyOwnershipContext(ctx, authInfo, config.Ownership)
		}

		// Issue #28 fix: Check parent chain for ownership requirements
		// Get metadata from context (set by metadata middleware that runs before auth middleware)
		meta, _ := ctx.Value(metadata.MetadataKey).(*metadata.TypeMetadata)
		if meta != nil {
			parentResult := checkParentOwnership(authInfo, meta)
			switch parentResult.status {
			case authUnauthorized:
				slog.WarnContext(ctx, "auth rejected: parent ownership requires auth", "path", r.URL.Path, "method", r.Method, "type", meta.TypeName)
				handler.WriteError(w, http.StatusUnauthorized, handler.ErrCodeUnauthorized, http.StatusText(http.StatusUnauthorized))
				return
			case authForbidden:
				slog.WarnContext(ctx, "auth rejected: parent ownership forbidden", "path", r.URL.Path, "method", r.Method, "type", meta.TypeName)
				handler.WriteError(w, http.StatusForbidden, handler.ErrCodeForbidden, http.StatusText(http.StatusForbidden))
				return
			}
			// Store parent ownership info in context for datastore to apply filtering
			if len(parentResult.parentsNeedingOwnership) > 0 {
				ctx = context.WithValue(ctx, metadata.ParentOwnershipKey, parentResult.parentsNeedingOwnership)
			}
		}

		// Issue #64: Multi-tenant scoping
		if meta != nil && (meta.TenantField != "" || meta.IsTenantTable) {
			if authInfo == nil || authInfo.TenantID == "" {
				slog.WarnContext(ctx, "auth rejected: tenant-scoped route requires TenantID", "path", r.URL.Path, "method", r.Method, "type", meta.TypeName)
				handler.WriteError(w, http.StatusUnauthorized, handler.ErrCodeUnauthorized, http.StatusText(http.StatusUnauthorized))
				return
			}
			ctx = applyTenantContext(ctx, authInfo.TenantID)

			// Walk parent chain for tenant filtering (same pattern as parent ownership)
			parentsNeedingTenant := checkParentTenant(meta)
			if len(parentsNeedingTenant) > 0 {
				ctx = context.WithValue(ctx, metadata.ParentTenantKey, parentsNeedingTenant)
			}
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// parentOwnershipResult holds the result of checking parent ownership chain
type parentOwnershipResult struct {
	status                  authStatus
	parentsNeedingOwnership []*metadata.TypeMetadata // Parents that need ownership filtering applied
}

// checkParentOwnership walks the parent metadata chain and checks ownership requirements.
// Returns unauthorized if any parent has ownership and user is not authenticated.
// Returns list of parents needing ownership filtering (user doesn't have bypass scope).
func checkParentOwnership(authInfo *AuthInfo, meta *metadata.TypeMetadata) parentOwnershipResult {
	var parentsNeedingOwnership []*metadata.TypeMetadata

	// Walk up the parent chain
	for parent := meta.ParentMeta; parent != nil; parent = parent.ParentMeta {
		// Check if this parent has ownership configured
		if len(parent.OwnershipFields) == 0 {
			continue
		}

		// Parent has ownership - user must be authenticated with a valid UserID
		if authInfo == nil || authInfo.UserID == "" {
			return parentOwnershipResult{status: authUnauthorized}
		}

		// Check if user has bypass scope for this parent
		hasBypass := false
		for _, bypassScope := range parent.BypassScopes {
			if hasAnyScope(authInfo.Scopes, []string{bypassScope}) {
				hasBypass = true
				break
			}
		}

		// If no bypass, this parent needs ownership filtering
		if !hasBypass {
			parentsNeedingOwnership = append(parentsNeedingOwnership, parent)
		}
	}

	return parentOwnershipResult{
		status:                  authOK,
		parentsNeedingOwnership: parentsNeedingOwnership,
	}
}

// buildChildAllowedIncludes recursively walks the ChildAuth tree, running checkAuth at each
// level and building dotted paths for AllowedIncludes. Auth is cumulative (AND): a deeper
// level is only reachable if its parent passes. Ownership flag is cumulative (OR): if any
// level in the chain has ApplyOwnership, the dotted path gets true.
func buildChildAllowedIncludes(authInfo *AuthInfo, childAuth map[string]*AuthConfig, prefix string, parentApplyOwnership bool, includes metadata.AllowedIncludes) {
	for relationName, childConfig := range childAuth {
		childResult := checkAuth(authInfo, childConfig)
		if childResult.Status != authOK {
			continue
		}

		path := relationName
		if prefix != "" {
			path = prefix + "." + relationName
		}

		applyOwnership := parentApplyOwnership || childResult.ApplyOwnership
		includes[path] = applyOwnership

		if len(childConfig.ChildAuth) > 0 {
			buildChildAllowedIncludes(authInfo, childConfig.ChildAuth, path, applyOwnership, includes)
		}
	}
}

// buildParentAllowedIncludes walks the ParentAuth chain, running checkAuth at each level
// and building dotted paths for parent includes (belongs-to direction).
// Auth is cumulative (AND): stops at the first level that fails.
// Ownership flag is cumulative (OR).
func buildParentAllowedIncludes(authInfo *AuthInfo, config *AuthConfig, includes metadata.AllowedIncludes) {
	var parts []string
	var cumulativeOwnership bool
	current := config

	for current.ParentAuth != nil {
		parentResult := checkAuth(authInfo, current.ParentAuth)
		if parentResult.Status != authOK {
			break
		}

		parts = append(parts, current.ParentIncludeName)
		cumulativeOwnership = cumulativeOwnership || parentResult.ApplyOwnership
		path := strings.Join(parts, ".")
		includes[path] = cumulativeOwnership

		current = current.ParentAuth
	}
}

// blockUnauthorized returns 401 for any request (no auth config = blocked)
func blockUnauthorized(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.WarnContext(r.Context(), "auth rejected: no auth config", "path", r.URL.Path, "method", r.Method)
		handler.WriteError(w, http.StatusUnauthorized, handler.ErrCodeUnauthorized, http.StatusText(http.StatusUnauthorized))
	})
}

// applyTenantContext sets tenant scoping context keys for the datastore layer
func applyTenantContext(ctx context.Context, tenantID string) context.Context {
	ctx = context.WithValue(ctx, metadata.TenantScopedKey, true)
	ctx = context.WithValue(ctx, metadata.TenantIDValueKey, tenantID)
	return ctx
}

// checkParentTenant walks the parent metadata chain and collects parents that have tenant scoping.
// The datastore layer uses this to JOIN and filter parent tables by tenant ID.
func checkParentTenant(meta *metadata.TypeMetadata) []*metadata.TypeMetadata {
	var parentsNeedingTenant []*metadata.TypeMetadata

	for parent := meta.ParentMeta; parent != nil; parent = parent.ParentMeta {
		if parent.TenantField != "" {
			parentsNeedingTenant = append(parentsNeedingTenant, parent)
		}
	}

	return parentsNeedingTenant
}

// applyOwnershipContext sets ownership context flags for all authenticated users
// The datastore layer will use these flags to:
// - Always populate ownership fields on create (even for admins)
// - Apply ownership filtering on reads (unless user has bypass scope)
func applyOwnershipContext(ctx context.Context, authInfo *AuthInfo, ownership *OwnershipConfig) context.Context {
	// Always set ownership context - this ensures ownership fields are populated on create
	// The datastore layer will check bypass scopes to skip filtering on reads
	ctx = context.WithValue(ctx, metadata.OwnershipEnforcedKey, true)
	ctx = context.WithValue(ctx, metadata.OwnershipUserIDKey, authInfo.UserID)
	ctx = context.WithValue(ctx, metadata.OwnershipFieldsKey, ownership.Fields)

	return ctx
}
