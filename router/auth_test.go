//nolint:goconst,gocyclo,staticcheck // Test code - test data strings, complex test functions, and string context keys are acceptable
package router_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/router"
)

// Test models for auth tests
type AuthTestUser struct {
	bun.BaseModel `bun:"table:auth_test_users"`
	ID            int    `bun:"id,pk,autoincrement"`
	Name          string `bun:"name"`
}

type AuthTestPost struct {
	bun.BaseModel `bun:"table:auth_test_posts"`
	ID            int    `bun:"id,pk,autoincrement"`
	UserID        string `bun:"user_id,notnull"` // String for external user IDs
	Title         string `bun:"title"`
}

// setupAuthTest creates tables and returns a fresh router
func setupAuthTest(t *testing.T, registerFunc func(*router.Builder)) *chi.Mux {
	ds, err := datastore.Get()
	if err != nil {
		t.Fatalf("failed to get datastore: %v", err)
	}

	db := ds.GetDB()
	ctx := context.Background()

	// Create tables
	if _, err := db.NewCreateTable().Model((*AuthTestUser)(nil)).IfNotExists().Exec(ctx); err != nil {
		t.Fatalf("failed to create users table: %v", err)
	}
	if _, err := db.NewCreateTable().Model((*AuthTestPost)(nil)).IfNotExists().Exec(ctx); err != nil {
		t.Fatalf("failed to create posts table: %v", err)
	}

	// Clean tables
	_, _ = db.NewDelete().Model((*AuthTestPost)(nil)).Where("1=1").Exec(ctx)
	_, _ = db.NewDelete().Model((*AuthTestUser)(nil)).Where("1=1").Exec(ctx)
	_, _ = db.Exec("DELETE FROM sqlite_sequence WHERE name IN ('auth_test_users', 'auth_test_posts')")

	// Create router
	r := chi.NewRouter()
	b := router.NewBuilder(r)
	registerFunc(b)

	return r
}

// addAuthMiddleware adds test auth middleware to a request
func addAuthMiddleware(r *chi.Mux, userID string, scopes []string) *chi.Mux {
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			authInfo := &router.AuthInfo{
				UserID: userID,
				Scopes: scopes,
			}
			ctx := context.WithValue(req.Context(), router.AuthInfoKey, authInfo)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	return r
}

func TestAuth_PublicRoute(t *testing.T) {
	r := setupAuthTest(t, func(b *router.Builder) {
		router.RegisterRoutes[AuthTestUser](b, "/users", router.AuthConfig{
			Methods: []string{router.MethodAll},
			Scopes:  []string{router.ScopePublic},
		})
	})

	// Test without auth - should succeed
	req := httptest.NewRequest("GET", "/users", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for public route, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuth_BlockedRoute(t *testing.T) {
	r := setupAuthTest(t, func(b *router.Builder) {
		router.RegisterRoutes[AuthTestUser](b, "/users", router.AuthConfig{
			Methods: []string{router.MethodAll},
			Scopes:  []string{}, // Empty scopes = blocked
		})
	})

	// Test without auth - should fail
	req := httptest.NewRequest("GET", "/users", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 for blocked route, got %d", w.Code)
	}
}

func TestAuth_AuthOnlyRoute(t *testing.T) {
	// Test without auth
	r := setupAuthTest(t, func(b *router.Builder) {
		router.RegisterRoutes[AuthTestUser](b, "/users", router.AuthConfig{
			Methods: []string{router.MethodAll},
			Scopes:  []string{router.ScopeAuthOnly},
		})
	})

	req := httptest.NewRequest("GET", "/users", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 without auth, got %d", w.Code)
	}

	// Test with auth (any scopes) - create new router with auth middleware
	r2 := addAuthMiddleware(chi.NewRouter(), "user123", []string{"random_scope"})
	b := router.NewBuilder(r2)
	router.RegisterRoutes[AuthTestUser](b, "/users", router.AuthConfig{
		Methods: []string{router.MethodAll},
		Scopes:  []string{router.ScopeAuthOnly},
	})

	req = httptest.NewRequest("GET", "/users", nil)
	w = httptest.NewRecorder()
	r2.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 with auth, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuth_ScopeRequired(t *testing.T) {
	r := setupAuthTest(t, func(b *router.Builder) {
		router.RegisterRoutes[AuthTestUser](b, "/users", router.AuthConfig{
			Methods: []string{router.MethodAll},
			Scopes:  []string{"admin", "moderator"},
		})
	})

	// Without auth - 401
	req := httptest.NewRequest("GET", "/users", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 without auth, got %d", w.Code)
	}

	// With auth but wrong scope - 403
	r = addAuthMiddleware(chi.NewRouter(), "user123", []string{"user"})
	b := router.NewBuilder(r)
	router.RegisterRoutes[AuthTestUser](b, "/users", router.AuthConfig{
		Methods: []string{router.MethodAll},
		Scopes:  []string{"admin", "moderator"},
	})

	req = httptest.NewRequest("GET", "/users", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403 with wrong scope, got %d", w.Code)
	}

	// With correct scope - 200
	r = addAuthMiddleware(chi.NewRouter(), "user123", []string{"admin"})
	b = router.NewBuilder(r)
	router.RegisterRoutes[AuthTestUser](b, "/users", router.AuthConfig{
		Methods: []string{router.MethodAll},
		Scopes:  []string{"admin", "moderator"},
	})

	req = httptest.NewRequest("GET", "/users", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 with correct scope, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuth_MethodSpecificAuth(t *testing.T) {
	// Public reads, authenticated writes
	r := addAuthMiddleware(chi.NewRouter(), "user123", []string{"user"})
	b := router.NewBuilder(r)

	// First register without auth for GET/LIST (public reads)
	router.RegisterRoutes[AuthTestUser](b, "/users",
		router.AuthConfig{
			Methods: []string{router.MethodGet, router.MethodList},
			Scopes:  []string{router.ScopePublic},
		},
		router.AuthConfig{
			Methods: []string{router.MethodPost, router.MethodPut, router.MethodDelete},
			Scopes:  []string{router.ScopeAuthOnly},
		},
	)

	// GET /users (list) without auth - should succeed (public)
	r2 := chi.NewRouter()
	b2 := router.NewBuilder(r2)
	router.RegisterRoutes[AuthTestUser](b2, "/users",
		router.AuthConfig{
			Methods: []string{router.MethodGet, router.MethodList},
			Scopes:  []string{router.ScopePublic},
		},
		router.AuthConfig{
			Methods: []string{router.MethodPost},
			Scopes:  []string{router.ScopeAuthOnly},
		},
	)

	req := httptest.NewRequest("GET", "/users", nil)
	w := httptest.NewRecorder()
	r2.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for public LIST, got %d", w.Code)
	}

	// POST without auth - should fail (auth required)
	req = httptest.NewRequest("POST", "/users", bytes.NewBufferString(`{"name":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r2.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 for POST without auth, got %d", w.Code)
	}
}

func TestAuth_MethodListVsMethodGet(t *testing.T) {
	// Test that MethodList and MethodGet can have different auth configs
	// Use case: List requires auth (can't browse), but Get is public (shareable links)

	// Setup: Create a post first
	ds, _ := datastore.Get()
	db := ds.GetDB()
	ctx := context.Background()

	_, _ = db.NewDelete().Model((*AuthTestPost)(nil)).Where("1=1").Exec(ctx)
	post := &AuthTestPost{UserID: "user1", Title: "Test Post"}
	_, _ = db.NewInsert().Model(post).Returning("*").Exec(ctx)

	t.Run("PublicGet_AuthenticatedList", func(t *testing.T) {
		// List requires auth, Get is public
		r := chi.NewRouter()
		b := router.NewBuilder(r)

		router.RegisterRoutes[AuthTestPost](b, "/posts",
			router.AuthConfig{
				Methods: []string{router.MethodList},
				Scopes:  []string{router.ScopeAuthOnly}, // List requires auth
			},
			router.AuthConfig{
				Methods: []string{router.MethodGet},
				Scopes:  []string{router.ScopePublic}, // Get is public
			},
		)

		// GET /posts (list) without auth - should fail
		req := httptest.NewRequest("GET", "/posts", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401 for list without auth, got %d", w.Code)
		}

		// GET /posts/1 (single item) without auth - should succeed
		req = httptest.NewRequest("GET", "/posts/1", nil)
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200 for public get, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("PublicList_AuthenticatedGet", func(t *testing.T) {
		// List is public, Get requires auth
		r := chi.NewRouter()
		b := router.NewBuilder(r)

		router.RegisterRoutes[AuthTestPost](b, "/posts",
			router.AuthConfig{
				Methods: []string{router.MethodList},
				Scopes:  []string{router.ScopePublic}, // List is public
			},
			router.AuthConfig{
				Methods: []string{router.MethodGet},
				Scopes:  []string{router.ScopeAuthOnly}, // Get requires auth
			},
		)

		// GET /posts (list) without auth - should succeed
		req := httptest.NewRequest("GET", "/posts", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200 for public list, got %d: %s", w.Code, w.Body.String())
		}

		// GET /posts/1 (single item) without auth - should fail
		req = httptest.NewRequest("GET", "/posts/1", nil)
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401 for get without auth, got %d", w.Code)
		}
	})
}

func TestAuth_MethodAllOverride(t *testing.T) {
	// MethodAll sets default, specific method overrides
	r := chi.NewRouter()
	b := router.NewBuilder(r)

	router.RegisterRoutes[AuthTestUser](b, "/users",
		router.AuthConfig{
			Methods: []string{router.MethodAll},
			Scopes:  []string{"user"}, // Default: user scope required
		},
		router.AuthConfig{
			Methods: []string{router.MethodGet, router.MethodList},
			Scopes:  []string{router.ScopePublic}, // Override: both GET and LIST are public
		},
	)

	// GET /users (list) should be public (override)
	req := httptest.NewRequest("GET", "/users", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for overridden LIST, got %d", w.Code)
	}

	// POST should require scope (default from MethodAll)
	req = httptest.NewRequest("POST", "/users", bytes.NewBufferString(`{"name":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 for POST (needs user scope), got %d", w.Code)
	}
}

func TestAuth_Ownership_Create(t *testing.T) {
	// Setup with ownership on posts
	r := addAuthMiddleware(chi.NewRouter(), "auth0|user123", []string{"user"})
	b := router.NewBuilder(r)

	router.RegisterRoutes[AuthTestPost](b, "/posts", router.AuthConfig{
		Methods: []string{router.MethodAll},
		Ownership: &router.OwnershipConfig{
			Fields:       []string{"UserID"},
			BypassScopes: []string{},
		},
	})

	// Create post - should auto-set UserID from auth
	postData := map[string]interface{}{
		"title": "Test Post",
	}
	body, _ := json.Marshal(postData)

	req := httptest.NewRequest("POST", "/posts", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", w.Code, w.Body.String())
	}

	// Verify UserID was set from auth
	var created AuthTestPost
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if created.UserID != "auth0|user123" {
		t.Errorf("expected UserID 'auth0|user123', got '%s'", created.UserID)
	}
}

func TestAuth_Ownership_List(t *testing.T) {
	// Setup two users and create posts for each
	ds, _ := datastore.Get()
	db := ds.GetDB()
	ctx := context.Background()

	// Clean and create test data
	_, _ = db.NewDelete().Model((*AuthTestPost)(nil)).Where("1=1").Exec(ctx)

	post1 := &AuthTestPost{UserID: "user1", Title: "User 1 Post"}
	post2 := &AuthTestPost{UserID: "user2", Title: "User 2 Post"}
	_, _ = db.NewInsert().Model(post1).Returning("*").Exec(ctx)
	_, _ = db.NewInsert().Model(post2).Returning("*").Exec(ctx)

	// User1 should only see their post
	r := addAuthMiddleware(chi.NewRouter(), "user1", []string{"user"})
	b := router.NewBuilder(r)

	router.RegisterRoutes[AuthTestPost](b, "/posts", router.AuthConfig{
		Methods: []string{router.MethodAll},
		Ownership: &router.OwnershipConfig{
			Fields:       []string{"UserID"},
			BypassScopes: []string{},
		},
	})

	req := httptest.NewRequest("GET", "/posts", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var posts []AuthTestPost
	if err := json.Unmarshal(w.Body.Bytes(), &posts); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Should only see user1's post
	if len(posts) != 1 {
		t.Errorf("expected 1 post, got %d", len(posts))
	}
	if len(posts) > 0 && posts[0].UserID != "user1" {
		t.Errorf("expected post for user1, got %s", posts[0].UserID)
	}
}

func TestAuth_Ownership_BypassScope(t *testing.T) {
	// Setup test data
	ds, _ := datastore.Get()
	db := ds.GetDB()
	ctx := context.Background()

	_, _ = db.NewDelete().Model((*AuthTestPost)(nil)).Where("1=1").Exec(ctx)

	post1 := &AuthTestPost{UserID: "user1", Title: "User 1 Post"}
	post2 := &AuthTestPost{UserID: "user2", Title: "User 2 Post"}
	_, _ = db.NewInsert().Model(post1).Returning("*").Exec(ctx)
	_, _ = db.NewInsert().Model(post2).Returning("*").Exec(ctx)

	// Admin with bypass scope should see all posts
	r := addAuthMiddleware(chi.NewRouter(), "admin_user", []string{"admin"})
	b := router.NewBuilder(r)

	router.RegisterRoutes[AuthTestPost](b, "/posts", router.AuthConfig{
		Methods: []string{router.MethodAll},
		Ownership: &router.OwnershipConfig{
			Fields:       []string{"UserID"},
			BypassScopes: []string{"admin"},
		},
	})

	req := httptest.NewRequest("GET", "/posts", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var posts []AuthTestPost
	if err := json.Unmarshal(w.Body.Bytes(), &posts); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Admin should see all posts
	if len(posts) != 2 {
		t.Errorf("expected 2 posts for admin, got %d", len(posts))
	}
}

func TestAuth_Ownership_Get404(t *testing.T) {
	// Setup test data
	ds, _ := datastore.Get()
	db := ds.GetDB()
	ctx := context.Background()

	_, _ = db.NewDelete().Model((*AuthTestPost)(nil)).Where("1=1").Exec(ctx)

	post := &AuthTestPost{UserID: "user1", Title: "User 1 Post"}
	_, _ = db.NewInsert().Model(post).Returning("*").Exec(ctx)

	// User2 tries to access user1's post - should get 404
	r := addAuthMiddleware(chi.NewRouter(), "user2", []string{"user"})
	b := router.NewBuilder(r)

	router.RegisterRoutes[AuthTestPost](b, "/posts", router.AuthConfig{
		Methods: []string{router.MethodAll},
		Ownership: &router.OwnershipConfig{
			Fields:       []string{"UserID"},
			BypassScopes: []string{},
		},
	})

	req := httptest.NewRequest("GET", "/posts/1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404 when accessing other user's post, got %d", w.Code)
	}
}

// Tests for convenience helper functions
func TestAuthHelpers(t *testing.T) {
	t.Run("AllPublic", func(t *testing.T) {
		cfg := router.AllPublic()
		if len(cfg.Methods) != 1 || cfg.Methods[0] != router.MethodAll {
			t.Errorf("expected Methods [MethodAll], got %v", cfg.Methods)
		}
		if len(cfg.Scopes) != 1 || cfg.Scopes[0] != router.ScopePublic {
			t.Errorf("expected Scopes [ScopePublic], got %v", cfg.Scopes)
		}
		if cfg.Ownership != nil {
			t.Errorf("expected nil Ownership, got %v", cfg.Ownership)
		}
	})

	t.Run("IsAuthenticated", func(t *testing.T) {
		cfg := router.IsAuthenticated()
		if len(cfg.Methods) != 1 || cfg.Methods[0] != router.MethodAll {
			t.Errorf("expected Methods [MethodAll], got %v", cfg.Methods)
		}
		if len(cfg.Scopes) != 1 || cfg.Scopes[0] != router.ScopeAuthOnly {
			t.Errorf("expected Scopes [ScopeAuthOnly], got %v", cfg.Scopes)
		}
		if cfg.Ownership != nil {
			t.Errorf("expected nil Ownership, got %v", cfg.Ownership)
		}
	})

	t.Run("AllScoped_SingleScope", func(t *testing.T) {
		cfg := router.AllScoped("admin")
		if len(cfg.Methods) != 1 || cfg.Methods[0] != router.MethodAll {
			t.Errorf("expected Methods [MethodAll], got %v", cfg.Methods)
		}
		if len(cfg.Scopes) != 1 || cfg.Scopes[0] != "admin" {
			t.Errorf("expected Scopes [admin], got %v", cfg.Scopes)
		}
		if cfg.Ownership != nil {
			t.Errorf("expected nil Ownership, got %v", cfg.Ownership)
		}
	})

	t.Run("AllScoped_MultipleScopes", func(t *testing.T) {
		cfg := router.AllScoped("admin", "moderator")
		if len(cfg.Methods) != 1 || cfg.Methods[0] != router.MethodAll {
			t.Errorf("expected Methods [MethodAll], got %v", cfg.Methods)
		}
		if len(cfg.Scopes) != 2 || cfg.Scopes[0] != "admin" || cfg.Scopes[1] != "moderator" {
			t.Errorf("expected Scopes [admin, moderator], got %v", cfg.Scopes)
		}
		if cfg.Ownership != nil {
			t.Errorf("expected nil Ownership, got %v", cfg.Ownership)
		}
	})

	t.Run("AllWithOwnershipUnless_NoBypass", func(t *testing.T) {
		cfg := router.AllWithOwnershipUnless([]string{"UserID"})
		if len(cfg.Methods) != 1 || cfg.Methods[0] != router.MethodAll {
			t.Errorf("expected Methods [MethodAll], got %v", cfg.Methods)
		}
		if len(cfg.Scopes) != 0 {
			t.Errorf("expected empty Scopes, got %v", cfg.Scopes)
		}
		if cfg.Ownership == nil {
			t.Fatal("expected Ownership config, got nil")
		}
		if len(cfg.Ownership.Fields) != 1 || cfg.Ownership.Fields[0] != "UserID" {
			t.Errorf("expected Fields [UserID], got %v", cfg.Ownership.Fields)
		}
		if len(cfg.Ownership.BypassScopes) != 0 {
			t.Errorf("expected empty BypassScopes, got %v", cfg.Ownership.BypassScopes)
		}
	})

	t.Run("AllWithOwnershipUnless_WithBypass", func(t *testing.T) {
		cfg := router.AllWithOwnershipUnless([]string{"UserID"}, "admin", "moderator")
		if len(cfg.Methods) != 1 || cfg.Methods[0] != router.MethodAll {
			t.Errorf("expected Methods [MethodAll], got %v", cfg.Methods)
		}
		if cfg.Ownership == nil {
			t.Fatal("expected Ownership config, got nil")
		}
		if len(cfg.Ownership.Fields) != 1 || cfg.Ownership.Fields[0] != "UserID" {
			t.Errorf("expected Fields [UserID], got %v", cfg.Ownership.Fields)
		}
		if len(cfg.Ownership.BypassScopes) != 2 || cfg.Ownership.BypassScopes[0] != "admin" || cfg.Ownership.BypassScopes[1] != "moderator" {
			t.Errorf("expected BypassScopes [admin, moderator], got %v", cfg.Ownership.BypassScopes)
		}
	})

	t.Run("PublicReadOnly", func(t *testing.T) {
		cfg := router.PublicReadOnly()
		if len(cfg.Methods) != 2 || cfg.Methods[0] != router.MethodGet || cfg.Methods[1] != router.MethodList {
			t.Errorf("expected Methods [MethodGet, MethodList], got %v", cfg.Methods)
		}
		if len(cfg.Scopes) != 1 || cfg.Scopes[0] != router.ScopePublic {
			t.Errorf("expected Scopes [ScopePublic], got %v", cfg.Scopes)
		}
		if cfg.Ownership != nil {
			t.Errorf("expected nil Ownership, got %v", cfg.Ownership)
		}
	})

	t.Run("PublicList", func(t *testing.T) {
		cfg := router.PublicList()
		if len(cfg.Methods) != 1 || cfg.Methods[0] != router.MethodList {
			t.Errorf("expected Methods [MethodList], got %v", cfg.Methods)
		}
		if len(cfg.Scopes) != 1 || cfg.Scopes[0] != router.ScopePublic {
			t.Errorf("expected Scopes [ScopePublic], got %v", cfg.Scopes)
		}
		if cfg.Ownership != nil {
			t.Errorf("expected nil Ownership, got %v", cfg.Ownership)
		}
	})

	t.Run("PublicGet", func(t *testing.T) {
		cfg := router.PublicGet()
		if len(cfg.Methods) != 1 || cfg.Methods[0] != router.MethodGet {
			t.Errorf("expected Methods [MethodGet], got %v", cfg.Methods)
		}
		if len(cfg.Scopes) != 1 || cfg.Scopes[0] != router.ScopePublic {
			t.Errorf("expected Scopes [ScopePublic], got %v", cfg.Scopes)
		}
		if cfg.Ownership != nil {
			t.Errorf("expected nil Ownership, got %v", cfg.Ownership)
		}
	})
}

// Test models for include/relation auth tests
type IncludeTestAuthor struct {
	bun.BaseModel `bun:"table:include_test_authors"`
	ID            int                `bun:"id,pk,autoincrement"`
	Name          string             `bun:"name"`
	Posts         []*IncludeTestPost `bun:"rel:has-many,join:id=author_id"`
}

type IncludeTestPost struct {
	bun.BaseModel `bun:"table:include_test_posts"`
	ID            int                `bun:"id,pk,autoincrement"`
	AuthorID      int                `bun:"author_id,notnull"`
	Author        *IncludeTestAuthor `bun:"rel:belongs-to,join:author_id=id"`
	OwnerID       string             `bun:"owner_id,notnull"`
	Title         string             `bun:"title"`
}

// TestAuth_ChildAuthPopulated tests that ChildAuth is populated when WithRelationName is used
// This exercises the wrapWithAuth code path that builds AllowedIncludes
func TestAuth_ChildAuthPopulated(t *testing.T) {
	ds, err := datastore.Get()
	if err != nil {
		t.Fatalf("failed to get datastore: %v", err)
	}

	db := ds.GetDB()
	ctx := context.Background()

	// Create tables
	if _, err := db.NewCreateTable().Model((*IncludeTestAuthor)(nil)).IfNotExists().Exec(ctx); err != nil {
		t.Fatalf("failed to create authors table: %v", err)
	}
	if _, err := db.NewCreateTable().Model((*IncludeTestPost)(nil)).IfNotExists().Exec(ctx); err != nil {
		t.Fatalf("failed to create posts table: %v", err)
	}

	// Clean tables
	_, _ = db.NewDelete().Model((*IncludeTestPost)(nil)).Where("1=1").Exec(ctx)
	_, _ = db.NewDelete().Model((*IncludeTestAuthor)(nil)).Where("1=1").Exec(ctx)

	// Create router with nested routes using WithRelationName
	r := chi.NewRouter()

	// Add auth middleware
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			authInfo := &router.AuthInfo{
				UserID: "alice",
				Scopes: []string{"user"},
			}
			ctx := context.WithValue(req.Context(), router.AuthInfoKey, authInfo)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})

	b := router.NewBuilder(r)

	// Register parent (public) with child (ownership-based) using WithRelationName
	router.RegisterRoutes[IncludeTestAuthor](b, "/authors",
		router.AllPublic(),
		func(b *router.Builder) {
			router.RegisterRoutes[IncludeTestPost](b, "/posts",
				router.AllWithOwnershipUnless([]string{"OwnerID"}, "admin"),
				router.WithRelationName("Posts"),
			)
		},
	)

	// Reset auto-increment
	_, _ = db.Exec("DELETE FROM sqlite_sequence WHERE name IN ('include_test_authors', 'include_test_posts')")

	// Create test data
	author := &IncludeTestAuthor{Name: "Test Author"}
	_, err = db.NewInsert().Model(author).Returning("*").Exec(ctx)
	if err != nil {
		t.Fatalf("failed to create author: %v", err)
	}

	// Create posts - one owned by alice, one by bob
	posts := []*IncludeTestPost{
		{AuthorID: author.ID, OwnerID: "alice", Title: "Alice's Post"},
		{AuthorID: author.ID, OwnerID: "bob", Title: "Bob's Post"},
	}
	for _, post := range posts {
		_, err = db.NewInsert().Model(post).Exec(ctx)
		if err != nil {
			t.Fatalf("failed to create post: %v", err)
		}
	}

	// Test: GET /authors/{id}?include=Posts as Alice
	// This should exercise the ChildAuth path in wrapWithAuth
	url := "/authors/" + strconv.Itoa(author.ID) + "?include=Posts"
	req := httptest.NewRequest("GET", url, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify response contains author
	var response IncludeTestAuthor
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v - body: %s", err, w.Body.String())
	}

	if response.Name != "Test Author" {
		t.Errorf("expected author name 'Test Author', got %v", response.Name)
	}

	// The ChildAuth path is exercised - AllowedIncludes is set with ApplyOwnership=true
	// Since parent is public and doesn't set ownership context, filtering is a no-op
	// The key test is that the include works at all (relation is authorized)
	if len(response.Posts) != 2 {
		t.Errorf("expected 2 posts (parent is public, no ownership context), got %d", len(response.Posts))
	}
}

// TestAuth_ChildAuthWithBypass tests that admin bypass works for includes
func TestAuth_ChildAuthWithBypass(t *testing.T) {
	ds, err := datastore.Get()
	if err != nil {
		t.Fatalf("failed to get datastore: %v", err)
	}

	db := ds.GetDB()
	ctx := context.Background()

	// Clean tables and reset auto-increment
	_, _ = db.NewDelete().Model((*IncludeTestPost)(nil)).Where("1=1").Exec(ctx)
	_, _ = db.NewDelete().Model((*IncludeTestAuthor)(nil)).Where("1=1").Exec(ctx)
	_, _ = db.Exec("DELETE FROM sqlite_sequence WHERE name IN ('include_test_authors', 'include_test_posts')")

	// Create router with admin user
	r := chi.NewRouter()

	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			authInfo := &router.AuthInfo{
				UserID: "admin",
				Scopes: []string{"user", "admin"}, // Admin has bypass scope
			}
			ctx := context.WithValue(req.Context(), router.AuthInfoKey, authInfo)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})

	b := router.NewBuilder(r)

	router.RegisterRoutes[IncludeTestAuthor](b, "/authors",
		router.AllPublic(),
		func(b *router.Builder) {
			router.RegisterRoutes[IncludeTestPost](b, "/posts",
				router.AllWithOwnershipUnless([]string{"OwnerID"}, "admin"),
				router.WithRelationName("Posts"),
			)
		},
	)

	// Create test data
	author := &IncludeTestAuthor{Name: "Test Author"}
	_, err = db.NewInsert().Model(author).Returning("*").Exec(ctx)
	if err != nil {
		t.Fatalf("failed to create author: %v", err)
	}

	posts := []*IncludeTestPost{
		{AuthorID: author.ID, OwnerID: "alice", Title: "Alice's Post"},
		{AuthorID: author.ID, OwnerID: "bob", Title: "Bob's Post"},
	}
	for _, post := range posts {
		_, err = db.NewInsert().Model(post).Exec(ctx)
		if err != nil {
			t.Fatalf("failed to create post: %v", err)
		}
	}

	// Test: GET /authors/{id}?include=Posts as Admin
	url := "/authors/" + strconv.Itoa(author.ID) + "?include=Posts"
	req := httptest.NewRequest("GET", url, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestAuth_ChildAuthNoAuth tests that unauthenticated users can't include protected relations
func TestAuth_ChildAuthNoAuth(t *testing.T) {
	ds, err := datastore.Get()
	if err != nil {
		t.Fatalf("failed to get datastore: %v", err)
	}

	db := ds.GetDB()
	ctx := context.Background()

	// Clean tables and reset auto-increment
	_, _ = db.NewDelete().Model((*IncludeTestPost)(nil)).Where("1=1").Exec(ctx)
	_, _ = db.NewDelete().Model((*IncludeTestAuthor)(nil)).Where("1=1").Exec(ctx)
	_, _ = db.Exec("DELETE FROM sqlite_sequence WHERE name IN ('include_test_authors', 'include_test_posts')")

	// Create router WITHOUT auth middleware (unauthenticated)
	r := chi.NewRouter()
	b := router.NewBuilder(r)

	router.RegisterRoutes[IncludeTestAuthor](b, "/authors",
		router.AllPublic(),
		func(b *router.Builder) {
			router.RegisterRoutes[IncludeTestPost](b, "/posts",
				router.AllWithOwnershipUnless([]string{"OwnerID"}, "admin"),
				router.WithRelationName("Posts"),
			)
		},
	)

	// Create test data
	author := &IncludeTestAuthor{Name: "Test Author"}
	_, err = db.NewInsert().Model(author).Returning("*").Exec(ctx)
	if err != nil {
		t.Fatalf("failed to create author: %v", err)
	}

	post := &IncludeTestPost{AuthorID: author.ID, OwnerID: "alice", Title: "Alice's Post"}
	_, err = db.NewInsert().Model(post).Exec(ctx)
	if err != nil {
		t.Fatalf("failed to create post: %v", err)
	}

	// Test: GET /authors/{id}?include=Posts without auth
	// Parent is public so request succeeds, but Posts shouldn't be included
	url := "/authors/" + strconv.Itoa(author.ID) + "?include=Posts"
	req := httptest.NewRequest("GET", url, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 (parent is public), got %d: %s", w.Code, w.Body.String())
	}

	// Posts should not be in response (not authorized)
	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// The posts field should be nil/empty since user isn't authorized for includes
	if posts, ok := response["posts"]; ok && posts != nil {
		postsSlice, isSlice := posts.([]interface{})
		if isSlice && len(postsSlice) > 0 {
			t.Errorf("expected no posts for unauthenticated user, got %v", posts)
		}
	}
}

// =============================================================================
// Issue #24 Tests: Empty UserID with ownership configured should return 401
// =============================================================================

// Test model for nested ownership tests
type OwnershipTestProject struct {
	bun.BaseModel `bun:"table:ownership_test_projects"`
	ID            int    `bun:"id,pk,autoincrement"`
	OwnerID       string `bun:"owner_id,notnull"`
	Name          string `bun:"name"`
}

type OwnershipTestTask struct {
	bun.BaseModel `bun:"table:ownership_test_tasks"`
	ID            int                   `bun:"id,pk,autoincrement"`
	ProjectID     int                   `bun:"project_id,notnull"`
	Project       *OwnershipTestProject `bun:"rel:belongs-to,join:project_id=id"`
	Title         string                `bun:"title"`
}

// addAuthMiddlewareEmptyUserID simulates a middleware that sets AuthInfo with empty UserID
// This is the pattern used by dhe and causes issue #24
func addAuthMiddlewareEmptyUserID(r *chi.Mux) *chi.Mux {
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			// Simulate middleware that sets AuthInfo but with empty UserID
			authInfo := &router.AuthInfo{
				UserID: "",
				Scopes: []string{router.ScopePublic},
			}
			ctx := context.WithValue(req.Context(), router.AuthInfoKey, authInfo)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	return r
}

// TestAuth_Issue24_OwnershipWithEmptyUserID tests that ownership + empty UserID returns 401, not 500
func TestAuth_Issue24_OwnershipWithEmptyUserID(t *testing.T) {
	ds, err := datastore.Get()
	if err != nil {
		t.Fatalf("failed to get datastore: %v", err)
	}

	db := ds.GetDB()
	ctx := context.Background()

	// Create table
	if _, err := db.NewCreateTable().Model((*AuthTestPost)(nil)).IfNotExists().Exec(ctx); err != nil {
		t.Fatalf("failed to create posts table: %v", err)
	}
	_, _ = db.NewDelete().Model((*AuthTestPost)(nil)).Where("1=1").Exec(ctx)

	// Create router with middleware that sets empty UserID (simulating dhe pattern)
	r := addAuthMiddlewareEmptyUserID(chi.NewRouter())
	b := router.NewBuilder(r)

	// Register route with ownership but no explicit scopes (the problematic pattern)
	router.RegisterRoutes[AuthTestPost](b, "/posts", router.AuthConfig{
		Methods: []string{router.MethodAll},
		Ownership: &router.OwnershipConfig{
			Fields: []string{"UserID"},
		},
	})

	// GET /posts with empty UserID should return 401, not 500
	req := httptest.NewRequest("GET", "/posts", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code == http.StatusInternalServerError {
		t.Errorf("Issue #24: got 500 instead of 401 - ownership check failed in datastore instead of auth middleware")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 for ownership with empty UserID, got %d: %s", w.Code, w.Body.String())
	}
}

// TestAuth_Issue24_ScopeAuthOnlyWithEmptyUserID tests that ScopeAuthOnly + empty UserID returns 401
func TestAuth_Issue24_ScopeAuthOnlyWithEmptyUserID(t *testing.T) {
	ds, err := datastore.Get()
	if err != nil {
		t.Fatalf("failed to get datastore: %v", err)
	}

	db := ds.GetDB()
	ctx := context.Background()

	// Create table
	if _, err := db.NewCreateTable().Model((*AuthTestUser)(nil)).IfNotExists().Exec(ctx); err != nil {
		t.Fatalf("failed to create users table: %v", err)
	}

	// Create router with middleware that sets empty UserID
	r := addAuthMiddlewareEmptyUserID(chi.NewRouter())
	b := router.NewBuilder(r)

	// Register route with ScopeAuthOnly
	router.RegisterRoutes[AuthTestUser](b, "/users", router.AuthConfig{
		Methods: []string{router.MethodAll},
		Scopes:  []string{router.ScopeAuthOnly},
	})

	// GET /users with empty UserID should return 401
	req := httptest.NewRequest("GET", "/users", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 for ScopeAuthOnly with empty UserID, got %d: %s", w.Code, w.Body.String())
	}
}

// =============================================================================
// Issue #28 Tests: Parent ownership should be checked for nested routes
// =============================================================================

// setupNestedOwnershipTest creates tables for nested ownership tests
func setupNestedOwnershipTest(t *testing.T) {
	ds, err := datastore.Get()
	if err != nil {
		t.Fatalf("failed to get datastore: %v", err)
	}

	db := ds.GetDB()
	ctx := context.Background()

	// Create tables
	if _, err := db.NewCreateTable().Model((*OwnershipTestProject)(nil)).IfNotExists().Exec(ctx); err != nil {
		t.Fatalf("failed to create projects table: %v", err)
	}
	if _, err := db.NewCreateTable().Model((*OwnershipTestTask)(nil)).IfNotExists().Exec(ctx); err != nil {
		t.Fatalf("failed to create tasks table: %v", err)
	}

	// Clean tables
	_, _ = db.NewDelete().Model((*OwnershipTestTask)(nil)).Where("1=1").Exec(ctx)
	_, _ = db.NewDelete().Model((*OwnershipTestProject)(nil)).Where("1=1").Exec(ctx)
	_, _ = db.Exec("DELETE FROM sqlite_sequence WHERE name IN ('ownership_test_projects', 'ownership_test_tasks')")
}

// TestAuth_Issue28_ParentOwnershipNoAuth tests that child routes under owned parent require auth
func TestAuth_Issue28_ParentOwnershipNoAuth(t *testing.T) {
	setupNestedOwnershipTest(t)

	ds, _ := datastore.Get()
	db := ds.GetDB()
	ctx := context.Background()

	// Create a project owned by alice
	project := &OwnershipTestProject{OwnerID: "alice", Name: "Alice's Project"}
	_, err := db.NewInsert().Model(project).Returning("*").Exec(ctx)
	if err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	// Create a task under the project
	task := &OwnershipTestTask{ProjectID: project.ID, Title: "Task 1"}
	_, err = db.NewInsert().Model(task).Returning("*").Exec(ctx)
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	// Create router WITHOUT auth (simulating public child under owned parent)
	r := chi.NewRouter()
	b := router.NewBuilder(r)

	router.RegisterRoutes[OwnershipTestProject](b, "/projects",
		router.AuthConfig{
			Methods: []string{router.MethodAll},
			Ownership: &router.OwnershipConfig{
				Fields: []string{"OwnerID"},
			},
		},
		func(b *router.Builder) {
			// Child route is "public" - no auth configured
			router.RegisterRoutes[OwnershipTestTask](b, "/tasks",
				router.AllPublic(),
			)
		},
	)

	// GET /projects/1/tasks without auth should return 401
	// because parent (project) has ownership configured
	req := httptest.NewRequest("GET", "/projects/1/tasks", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Errorf("Issue #28: child route accessible without auth even though parent has ownership")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 for public child under owned parent without auth, got %d: %s", w.Code, w.Body.String())
	}
}

// TestAuth_Issue28_ParentOwnershipFiltering tests that child routes filter by parent ownership
func TestAuth_Issue28_ParentOwnershipFiltering(t *testing.T) {
	tests := []struct {
		name          string
		authUser      string
		expectedTasks int
	}{
		{"wrong user sees nothing", "bob", 0},
		{"owner sees tasks", "alice", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupNestedOwnershipTest(t)

			ds, _ := datastore.Get()
			db := ds.GetDB()
			ctx := context.Background()

			project := &OwnershipTestProject{OwnerID: "alice", Name: "Alice's Project"}
			_, err := db.NewInsert().Model(project).Returning("*").Exec(ctx)
			if err != nil {
				t.Fatalf("failed to create project: %v", err)
			}

			task := &OwnershipTestTask{ProjectID: project.ID, Title: "Task 1"}
			_, err = db.NewInsert().Model(task).Returning("*").Exec(ctx)
			if err != nil {
				t.Fatalf("failed to create task: %v", err)
			}

			r := addAuthMiddleware(chi.NewRouter(), tt.authUser, []string{"user"})
			b := router.NewBuilder(r)

			router.RegisterRoutes[OwnershipTestProject](b, "/projects",
				router.AuthConfig{
					Methods: []string{router.MethodAll},
					Ownership: &router.OwnershipConfig{
						Fields: []string{"OwnerID"},
					},
				},
				func(b *router.Builder) {
					router.RegisterRoutes[OwnershipTestTask](b, "/tasks",
						router.IsAuthenticated(),
					)
				},
			)

			req := httptest.NewRequest("GET", "/projects/1/tasks", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
			}

			var tasks []OwnershipTestTask
			if err := json.Unmarshal(w.Body.Bytes(), &tasks); err != nil {
				t.Fatalf("failed to parse response: %v", err)
			}

			if len(tasks) != tt.expectedTasks {
				t.Errorf("expected %d tasks, got %d", tt.expectedTasks, len(tasks))
			}
		})
	}
}

// TestAuth_Issue28_ParentOwnershipBypass tests that bypass scope works for parent ownership
func TestAuth_Issue28_ParentOwnershipBypass(t *testing.T) {
	setupNestedOwnershipTest(t)

	ds, _ := datastore.Get()
	db := ds.GetDB()
	ctx := context.Background()

	// Create a project owned by alice
	project := &OwnershipTestProject{OwnerID: "alice", Name: "Alice's Project"}
	_, err := db.NewInsert().Model(project).Returning("*").Exec(ctx)
	if err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	// Create a task under the project
	task := &OwnershipTestTask{ProjectID: project.ID, Title: "Task 1"}
	_, err = db.NewInsert().Model(task).Returning("*").Exec(ctx)
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	// Create router with Admin (has bypass scope)
	r := addAuthMiddleware(chi.NewRouter(), "admin", []string{"user", "admin"})
	b := router.NewBuilder(r)

	router.RegisterRoutes[OwnershipTestProject](b, "/projects",
		router.AuthConfig{
			Methods: []string{router.MethodAll},
			Ownership: &router.OwnershipConfig{
				Fields:       []string{"OwnerID"},
				BypassScopes: []string{"admin"},
			},
		},
		func(b *router.Builder) {
			router.RegisterRoutes[OwnershipTestTask](b, "/tasks",
				router.IsAuthenticated(),
			)
		},
	)

	// GET /projects/1/tasks as Admin should succeed (bypass)
	req := httptest.NewRequest("GET", "/projects/1/tasks", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for admin with bypass scope, got %d: %s", w.Code, w.Body.String())
	}
}
