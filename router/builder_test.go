//nolint:dupl,errcheck,gosec,goconst // Test code - duplicate test patterns, unchecked test cleanup, and repeated test strings are acceptable
package router_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/router"
	"github.com/uptrace/bun"
)

func TestMain(m *testing.M) {
	// Initialize datastore for all tests
	db, err := datastore.NewSQLite(":memory:")
	if err != nil {
		panic("failed to create test database: " + err.Error())
	}

	if err := datastore.Initialize(db); err != nil {
		db.Cleanup()
		panic("failed to initialize datastore: " + err.Error())
	}

	// Run tests
	code := m.Run()

	// Cleanup
	datastore.Cleanup()
	db.Cleanup()

	os.Exit(code)
}

// Test models with proper relationships
type TestUser struct {
	bun.BaseModel `bun:"table:test_users"`
	ID            int         `bun:"id,pk,autoincrement"`
	Name          string      `bun:"name"`
	Posts         []*TestPost `bun:"rel:has-many,join:id=user_id"`
}

type TestPost struct {
	bun.BaseModel `bun:"table:test_posts"`
	ID            int            `bun:"id,pk,autoincrement"`
	UserID        int            `bun:"user_id,notnull"`
	User          *TestUser      `bun:"rel:belongs-to,join:user_id=id"`
	Title         string         `bun:"title"`
	Comments      []*TestComment `bun:"rel:has-many,join:id=post_id"`
}

type TestComment struct {
	bun.BaseModel `bun:"table:test_comments"`
	ID            int       `bun:"id,pk,autoincrement"`
	PostID        int       `bun:"post_id,notnull"`
	Post          *TestPost `bun:"rel:belongs-to,join:post_id=id"`
	Text          string    `bun:"text"`
}

func setupBuilderTestTables() {
	// Enable slog warnings for debugging
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))
	slog.SetDefault(logger)

	// Get the shared test database
	ds, err := datastore.Get()
	if err != nil {
		panic("failed to get datastore: " + err.Error())
	}

	db := ds.GetDB()
	ctx := context.Background()

	// Create tables
	if _, err := db.NewCreateTable().Model((*TestUser)(nil)).IfNotExists().Exec(ctx); err != nil {
		panic("failed to create users table: " + err.Error())
	}
	if _, err := db.NewCreateTable().Model((*TestPost)(nil)).IfNotExists().Exec(ctx); err != nil {
		panic("failed to create posts table: " + err.Error())
	}
	if _, err := db.NewCreateTable().Model((*TestComment)(nil)).IfNotExists().Exec(ctx); err != nil {
		panic("failed to create comments table: " + err.Error())
	}
}

var builderTablesOnce sync.Once

func setupBuilderTest(t *testing.T) (*chi.Mux, *bun.DB) {
	t.Helper()

	// Create tables once
	builderTablesOnce.Do(setupBuilderTestTables)

	// Get database
	ds, _ := datastore.Get()
	db := ds.GetDB()

	// Clean tables for each test
	ctx := context.Background()
	db.NewDelete().Model((*TestComment)(nil)).Where("1=1").Exec(ctx)
	db.NewDelete().Model((*TestPost)(nil)).Where("1=1").Exec(ctx)
	db.NewDelete().Model((*TestUser)(nil)).Where("1=1").Exec(ctx)
	// Reset autoincrement counters
	db.Exec("DELETE FROM sqlite_sequence WHERE name IN ('test_users', 'test_posts', 'test_comments')")

	// Create router with nested routes using Builder API
	r := chi.NewRouter()
	b := router.NewBuilder(r)
	router.RegisterRoutes[TestUser](b, "/users", router.AuthConfig{
		Methods: []string{router.MethodAll},
		Scopes:  []string{router.ScopePublic},
	}, func(b *router.Builder) {
		router.RegisterRoutes[TestPost](b, "/posts", router.AuthConfig{
			Methods: []string{router.MethodAll},
			Scopes:  []string{router.ScopePublic},
		}, func(b *router.Builder) {
			router.RegisterRoutes[TestComment](b, "/comments", router.AuthConfig{
				Methods: []string{router.MethodAll},
				Scopes:  []string{router.ScopePublic},
			})
		})
	})

	return r, db
}

func TestBuilder_BasicRoutes(t *testing.T) {
	r, db := setupBuilderTest(t)
	ctx := context.Background()

	// Create test user
	user := &TestUser{Name: "Alice"}
	if _, err := db.NewInsert().Model(user).Returning("*").Exec(ctx); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	tests := []struct {
		name           string
		method         string
		path           string
		body           interface{}
		expectedStatus int
		checkResponse  func(t *testing.T, body []byte)
	}{
		{
			name:           "GET all users",
			method:         "GET",
			path:           "/users",
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, body []byte) {
				var users []*TestUser
				if err := json.Unmarshal(body, &users); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}
				if len(users) != 1 {
					t.Errorf("expected 1 user, got %d", len(users))
				}
			},
		},
		{
			name:           "GET specific user",
			method:         "GET",
			path:           "/users/1",
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, body []byte) {
				var user TestUser
				if err := json.Unmarshal(body, &user); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}
				if user.Name != "Alice" {
					t.Errorf("expected name 'Alice', got %q", user.Name)
				}
			},
		},
		{
			name:           "POST new user",
			method:         "POST",
			path:           "/users",
			body:           map[string]interface{}{"name": "Bob"},
			expectedStatus: http.StatusCreated,
			checkResponse: func(t *testing.T, body []byte) {
				var user TestUser
				if err := json.Unmarshal(body, &user); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}
				if user.Name != "Bob" {
					t.Errorf("expected name 'Bob', got %q", user.Name)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req *http.Request
			if tt.body != nil {
				bodyBytes, _ := json.Marshal(tt.body)
				req = httptest.NewRequest(tt.method, tt.path, bytes.NewReader(bodyBytes))
				req.Header.Set("Content-Type", "application/json")
			} else {
				req = httptest.NewRequest(tt.method, tt.path, nil)
			}

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d. Body: %s", tt.expectedStatus, w.Code, w.Body.String())
			}

			if tt.checkResponse != nil && w.Code < 300 {
				tt.checkResponse(t, w.Body.Bytes())
			}
		})
	}
}

func TestBuilder_NestedRoutes(t *testing.T) {
	r, db := setupBuilderTest(t)
	ctx := context.Background()

	// Create test data
	user := &TestUser{Name: "Alice"}
	if _, err := db.NewInsert().Model(user).Returning("*").Exec(ctx); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	post := &TestPost{UserID: user.ID, Title: "Test Post"}
	if _, err := db.NewInsert().Model(post).Returning("*").Exec(ctx); err != nil {
		t.Fatalf("failed to create post: %v", err)
	}

	tests := []struct {
		name           string
		method         string
		path           string
		body           interface{}
		expectedStatus int
		checkResponse  func(t *testing.T, body []byte)
	}{
		{
			name:           "GET user's posts",
			method:         "GET",
			path:           "/users/1/posts",
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, body []byte) {
				var posts []*TestPost
				if err := json.Unmarshal(body, &posts); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}
				if len(posts) != 1 {
					t.Errorf("expected 1 post, got %d", len(posts))
				}
				if posts[0].Title != "Test Post" {
					t.Errorf("expected title 'Test Post', got %q", posts[0].Title)
				}
			},
		},
		{
			name:           "GET specific post for user",
			method:         "GET",
			path:           "/users/1/posts/1",
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, body []byte) {
				var post TestPost
				if err := json.Unmarshal(body, &post); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}
				if post.Title != "Test Post" {
					t.Errorf("expected title 'Test Post', got %q", post.Title)
				}
			},
		},
		{
			name:           "POST new post for user",
			method:         "POST",
			path:           "/users/1/posts",
			body:           map[string]interface{}{"user_id": 1, "title": "New Post"},
			expectedStatus: http.StatusCreated,
			checkResponse: func(t *testing.T, body []byte) {
				var post TestPost
				if err := json.Unmarshal(body, &post); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}
				if post.Title != "New Post" {
					t.Errorf("expected title 'New Post', got %q", post.Title)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req *http.Request
			if tt.body != nil {
				bodyBytes, _ := json.Marshal(tt.body)
				req = httptest.NewRequest(tt.method, tt.path, bytes.NewReader(bodyBytes))
				req.Header.Set("Content-Type", "application/json")
			} else {
				req = httptest.NewRequest(tt.method, tt.path, nil)
			}

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d. Body: %s", tt.expectedStatus, w.Code, w.Body.String())
			}

			if tt.checkResponse != nil && w.Code < 300 {
				tt.checkResponse(t, w.Body.Bytes())
			}
		})
	}
}

func TestBuilder_ThreeLevels(t *testing.T) {
	r, db := setupBuilderTest(t)
	ctx := context.Background()

	// Create test data
	user := &TestUser{Name: "Bob"}
	if _, err := db.NewInsert().Model(user).Returning("*").Exec(ctx); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	post := &TestPost{UserID: user.ID, Title: "Bob's Post"}
	if _, err := db.NewInsert().Model(post).Returning("*").Exec(ctx); err != nil {
		t.Fatalf("failed to create post: %v", err)
	}

	comment := &TestComment{PostID: post.ID, Text: "Great post!"}
	if _, err := db.NewInsert().Model(comment).Returning("*").Exec(ctx); err != nil {
		t.Fatalf("failed to create comment: %v", err)
	}

	// Test GET comments for a specific post
	req := httptest.NewRequest("GET", "/users/1/posts/1/comments", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var comments []*TestComment
	if err := json.Unmarshal(w.Body.Bytes(), &comments); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(comments) != 1 {
		t.Errorf("expected 1 comment, got %d", len(comments))
	}

	if len(comments) > 0 && comments[0].Text != "Great post!" {
		t.Errorf("expected text 'Great post!', got %q", comments[0].Text)
	}
}

// Models for multi-registration tests
// Item can be attached to both Project and User
type MultiRegProject struct {
	bun.BaseModel `bun:"table:multireg_projects"`
	ID            int             `bun:"id,pk,autoincrement" json:"id"`
	Name          string          `bun:"name" json:"name"`
	OwnerID       string          `bun:"owner_id" json:"owner_id"`
	Items         []*MultiRegItem `bun:"rel:has-many,join:id=project_id" json:"-"`
}

type MultiRegUser struct {
	bun.BaseModel `bun:"table:multireg_users"`
	ID            int             `bun:"id,pk,autoincrement" json:"id"`
	Name          string          `bun:"name" json:"name"`
	Items         []*MultiRegItem `bun:"rel:has-many,join:id=user_id" json:"-"`
}

type MultiRegItem struct {
	bun.BaseModel `bun:"table:multireg_items"`
	ID            int              `bun:"id,pk,autoincrement" json:"id"`
	ProjectID     int              `bun:"project_id" json:"project_id"`
	Project       *MultiRegProject `bun:"rel:belongs-to,join:project_id=id" json:"-"`
	UserID        int              `bun:"user_id" json:"user_id"`
	User          *MultiRegUser    `bun:"rel:belongs-to,join:user_id=id" json:"-"`
	Title         string           `bun:"title" json:"title"`
	CreatorID     string           `bun:"creator_id" json:"creator_id"`
}

var multiRegTablesOnce sync.Once

func setupMultiRegTables() {
	ds, err := datastore.Get()
	if err != nil {
		panic("failed to get datastore: " + err.Error())
	}

	db := ds.GetDB()
	ctx := context.Background()

	if _, err := db.NewCreateTable().Model((*MultiRegProject)(nil)).IfNotExists().Exec(ctx); err != nil {
		panic("failed to create projects table: " + err.Error())
	}
	if _, err := db.NewCreateTable().Model((*MultiRegUser)(nil)).IfNotExists().Exec(ctx); err != nil {
		panic("failed to create users table: " + err.Error())
	}
	if _, err := db.NewCreateTable().Model((*MultiRegItem)(nil)).IfNotExists().Exec(ctx); err != nil {
		panic("failed to create items table: " + err.Error())
	}
}

func cleanMultiRegTables(db *bun.DB) {
	ctx := context.Background()
	db.NewDelete().Model((*MultiRegItem)(nil)).Where("1=1").Exec(ctx)
	db.NewDelete().Model((*MultiRegProject)(nil)).Where("1=1").Exec(ctx)
	db.NewDelete().Model((*MultiRegUser)(nil)).Where("1=1").Exec(ctx)
	db.Exec("DELETE FROM sqlite_sequence WHERE name IN ('multireg_projects', 'multireg_users', 'multireg_items')")
}

// addMultiRegAuthMiddleware adds test auth middleware
func addMultiRegAuthMiddleware(r *chi.Mux, userID string, scopes []string) {
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			authInfo := &router.AuthInfo{
				UserID: userID,
				Scopes: scopes,
			}
			ctx := context.WithValue(req.Context(), router.AuthInfoKey, authInfo) //nolint:staticcheck // Test code using framework's string key
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
}

// TestMultiReg_SameModelRootAndNested tests registering the same model at root and nested
// This should allow both /items and /projects/{id}/items to work independently
func TestMultiReg_SameModelRootAndNested(t *testing.T) {
	multiRegTablesOnce.Do(setupMultiRegTables)

	ds, _ := datastore.Get()
	db := ds.GetDB()
	cleanMultiRegTables(db)
	ctx := context.Background()

	// Create test data
	project := &MultiRegProject{Name: "Project 1"}
	db.NewInsert().Model(project).Returning("*").Exec(ctx)

	// Create items - one with project, one without (root level)
	item1 := &MultiRegItem{ProjectID: project.ID, Title: "Project Item"}
	db.NewInsert().Model(item1).Returning("*").Exec(ctx)

	item2 := &MultiRegItem{Title: "Root Item"}
	db.NewInsert().Model(item2).Returning("*").Exec(ctx)

	// Setup router with Item at root AND nested under Project
	r := chi.NewRouter()
	b := router.NewBuilder(r)

	// Root registration: /items (all items)
	router.RegisterRoutes[MultiRegItem](b, "/items", router.AuthConfig{
		Methods: []string{router.MethodAll},
		Scopes:  []string{router.ScopePublic},
	})

	// Nested registration: /projects/{id}/items (project's items only)
	router.RegisterRoutes[MultiRegProject](b, "/projects", router.AuthConfig{
		Methods: []string{router.MethodAll},
		Scopes:  []string{router.ScopePublic},
	}, func(b *router.Builder) {
		router.RegisterRoutes[MultiRegItem](b, "/items", router.AuthConfig{
			Methods: []string{router.MethodAll},
			Scopes:  []string{router.ScopePublic},
		})
	})

	t.Run("Root_ListAll", func(t *testing.T) {
		// GET /items should return ALL items
		req := httptest.NewRequest("GET", "/items", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var items []MultiRegItem
		json.Unmarshal(w.Body.Bytes(), &items)
		if len(items) != 2 {
			t.Errorf("expected 2 items from root, got %d", len(items))
		}
	})

	t.Run("Nested_ListFiltered", func(t *testing.T) {
		// GET /projects/1/items should return only project's items
		req := httptest.NewRequest("GET", "/projects/1/items", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var items []MultiRegItem
		json.Unmarshal(w.Body.Bytes(), &items)
		if len(items) != 1 {
			t.Errorf("expected 1 item from nested, got %d", len(items))
		}
		if len(items) > 0 && items[0].Title != "Project Item" {
			t.Errorf("expected 'Project Item', got %q", items[0].Title)
		}
	})

	t.Run("Root_GetSingle", func(t *testing.T) {
		// GET /items/1 should work
		req := httptest.NewRequest("GET", "/items/1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("Nested_GetSingle", func(t *testing.T) {
		// GET /projects/1/items/1 should work
		req := httptest.NewRequest("GET", "/projects/1/items/1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})
}

// TestMultiReg_SameModelDifferentParents tests same model under different parent types
// Item can be under /projects/{id}/items AND /users/{id}/items
func TestMultiReg_SameModelDifferentParents(t *testing.T) {
	multiRegTablesOnce.Do(setupMultiRegTables)

	ds, _ := datastore.Get()
	db := ds.GetDB()
	cleanMultiRegTables(db)
	ctx := context.Background()

	// Create test data
	project := &MultiRegProject{Name: "Project 1"}
	db.NewInsert().Model(project).Returning("*").Exec(ctx)

	user := &MultiRegUser{Name: "User 1"}
	db.NewInsert().Model(user).Returning("*").Exec(ctx)

	// Items belonging to project and user respectively
	projectItem := &MultiRegItem{ProjectID: project.ID, Title: "Project's Item"}
	db.NewInsert().Model(projectItem).Returning("*").Exec(ctx)

	userItem := &MultiRegItem{UserID: user.ID, Title: "User's Item"}
	db.NewInsert().Model(userItem).Returning("*").Exec(ctx)

	// Setup router with Item under both Project and User
	r := chi.NewRouter()
	b := router.NewBuilder(r)

	router.RegisterRoutes[MultiRegProject](b, "/projects", router.AuthConfig{
		Methods: []string{router.MethodAll},
		Scopes:  []string{router.ScopePublic},
	}, func(b *router.Builder) {
		router.RegisterRoutes[MultiRegItem](b, "/items", router.AuthConfig{
			Methods: []string{router.MethodAll},
			Scopes:  []string{router.ScopePublic},
		})
	})

	router.RegisterRoutes[MultiRegUser](b, "/users", router.AuthConfig{
		Methods: []string{router.MethodAll},
		Scopes:  []string{router.ScopePublic},
	}, func(b *router.Builder) {
		router.RegisterRoutes[MultiRegItem](b, "/items", router.AuthConfig{
			Methods: []string{router.MethodAll},
			Scopes:  []string{router.ScopePublic},
		})
	})

	t.Run("ProjectItems_ListFiltered", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/projects/1/items", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var items []MultiRegItem
		json.Unmarshal(w.Body.Bytes(), &items)
		if len(items) != 1 {
			t.Errorf("expected 1 item for project, got %d", len(items))
		}
		if len(items) > 0 && items[0].Title != "Project's Item" {
			t.Errorf("expected 'Project's Item', got %q", items[0].Title)
		}
	})

	t.Run("UserItems_ListFiltered", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/users/1/items", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var items []MultiRegItem
		json.Unmarshal(w.Body.Bytes(), &items)
		if len(items) != 1 {
			t.Errorf("expected 1 item for user, got %d", len(items))
		}
		if len(items) > 0 && items[0].Title != "User's Item" {
			t.Errorf("expected 'User's Item', got %q", items[0].Title)
		}
	})

	t.Run("CrossAccess_Blocked", func(t *testing.T) {
		// Project item (ID 1) should not be accessible via user route
		req := httptest.NewRequest("GET", "/users/1/items/1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404 for cross-access, got %d", w.Code)
		}
	})
}

// TestMultiReg_DifferentOwnershipPerRegistration tests different ownership configs
// Root /items has no ownership, nested /projects/{id}/items has ownership
func TestMultiReg_DifferentOwnershipPerRegistration(t *testing.T) {
	multiRegTablesOnce.Do(setupMultiRegTables)

	ds, _ := datastore.Get()
	db := ds.GetDB()
	cleanMultiRegTables(db)
	ctx := context.Background()

	// Create test data
	project := &MultiRegProject{Name: "Project 1", OwnerID: "alice"}
	db.NewInsert().Model(project).Returning("*").Exec(ctx)

	item1 := &MultiRegItem{ProjectID: project.ID, Title: "Alice's Item", CreatorID: "alice"}
	db.NewInsert().Model(item1).Returning("*").Exec(ctx)

	item2 := &MultiRegItem{ProjectID: project.ID, Title: "Bob's Item", CreatorID: "bob"}
	db.NewInsert().Model(item2).Returning("*").Exec(ctx)

	// Setup router:
	// - /items: public, no ownership (sees all)
	// - /projects/{id}/items: ownership enforced (sees only own items)
	r := chi.NewRouter()
	addMultiRegAuthMiddleware(r, "alice", []string{"user"})
	b := router.NewBuilder(r)

	// Root: public, no ownership
	router.RegisterRoutes[MultiRegItem](b, "/items", router.AuthConfig{
		Methods: []string{router.MethodAll},
		Scopes:  []string{router.ScopePublic},
	})

	// Nested: with ownership on CreatorID
	router.RegisterRoutes[MultiRegProject](b, "/projects", router.AuthConfig{
		Methods: []string{router.MethodAll},
		Scopes:  []string{router.ScopePublic},
	}, func(b *router.Builder) {
		router.RegisterRoutes[MultiRegItem](b, "/items",
			router.AuthConfig{
				Methods: []string{router.MethodAll},
				Scopes:  []string{router.ScopeAuthOnly},
			},
			router.AuthConfig{
				Methods: []string{router.MethodAll},
				Ownership: &router.OwnershipConfig{
					Fields: []string{"CreatorID"},
				},
			},
		)
	})

	t.Run("Root_NoOwnership_SeesAll", func(t *testing.T) {
		// GET /items should return all items (no ownership filtering)
		req := httptest.NewRequest("GET", "/items", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var items []MultiRegItem
		json.Unmarshal(w.Body.Bytes(), &items)
		if len(items) != 2 {
			t.Errorf("expected 2 items from root (no ownership), got %d", len(items))
		}
	})

	t.Run("Nested_WithOwnership_SeesOwn", func(t *testing.T) {
		// GET /projects/1/items as alice should only return alice's items
		req := httptest.NewRequest("GET", "/projects/1/items", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var items []MultiRegItem
		json.Unmarshal(w.Body.Bytes(), &items)
		if len(items) != 1 {
			t.Errorf("expected 1 item for alice (ownership filtered), got %d", len(items))
		}
		if len(items) > 0 && items[0].CreatorID != "alice" {
			t.Errorf("expected alice's item, got creator %q", items[0].CreatorID)
		}
	})
}

// TestMultiReg_DifferentBypassScopesPerRegistration tests different bypass scopes
// /projects/{id}/items: admin bypasses ownership
// /users/{id}/items: moderator bypasses ownership
func TestMultiReg_DifferentBypassScopesPerRegistration(t *testing.T) {
	multiRegTablesOnce.Do(setupMultiRegTables)

	ds, _ := datastore.Get()
	db := ds.GetDB()
	cleanMultiRegTables(db)
	ctx := context.Background()

	// Create test data
	project := &MultiRegProject{Name: "Project 1"}
	db.NewInsert().Model(project).Returning("*").Exec(ctx)

	user := &MultiRegUser{Name: "User 1"}
	db.NewInsert().Model(user).Returning("*").Exec(ctx)

	projectItem := &MultiRegItem{ProjectID: project.ID, Title: "Project Item", CreatorID: "charlie"}
	db.NewInsert().Model(projectItem).Returning("*").Exec(ctx)

	userItem := &MultiRegItem{UserID: user.ID, Title: "User Item", CreatorID: "charlie"}
	db.NewInsert().Model(userItem).Returning("*").Exec(ctx)

	t.Run("Admin_BypassesProjectOwnership", func(t *testing.T) {
		// Admin can see all project items (bypass)
		r := chi.NewRouter()
		addMultiRegAuthMiddleware(r, "admin_user", []string{"admin"})
		b := router.NewBuilder(r)

		router.RegisterRoutes[MultiRegProject](b, "/projects", router.AuthConfig{
			Methods: []string{router.MethodAll},
			Scopes:  []string{router.ScopePublic},
		}, func(b *router.Builder) {
			router.RegisterRoutes[MultiRegItem](b, "/items",
				router.AuthConfig{
					Methods: []string{router.MethodAll},
					Ownership: &router.OwnershipConfig{
						Fields:       []string{"CreatorID"},
						BypassScopes: []string{"admin"}, // Admin bypasses
					},
				},
			)
		})

		req := httptest.NewRequest("GET", "/projects/1/items", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var items []MultiRegItem
		json.Unmarshal(w.Body.Bytes(), &items)
		if len(items) != 1 {
			t.Errorf("admin should see project item, got %d items", len(items))
		}
	})

	t.Run("Admin_DoesNotBypassUserOwnership", func(t *testing.T) {
		// Admin does NOT bypass user items (different bypass scope)
		r := chi.NewRouter()
		addMultiRegAuthMiddleware(r, "admin_user", []string{"admin"})
		b := router.NewBuilder(r)

		router.RegisterRoutes[MultiRegUser](b, "/users", router.AuthConfig{
			Methods: []string{router.MethodAll},
			Scopes:  []string{router.ScopePublic},
		}, func(b *router.Builder) {
			router.RegisterRoutes[MultiRegItem](b, "/items",
				router.AuthConfig{
					Methods: []string{router.MethodAll},
					Ownership: &router.OwnershipConfig{
						Fields:       []string{"CreatorID"},
						BypassScopes: []string{"moderator"}, // Only moderator bypasses
					},
				},
			)
		})

		req := httptest.NewRequest("GET", "/users/1/items", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var items []MultiRegItem
		json.Unmarshal(w.Body.Bytes(), &items)
		// Admin should see 0 items (ownership enforced, admin_user != charlie)
		if len(items) != 0 {
			t.Errorf("admin should NOT bypass user items (no moderator scope), got %d items", len(items))
		}
	})

	t.Run("Moderator_BypassesUserOwnership", func(t *testing.T) {
		// Moderator can see all user items (bypass)
		r := chi.NewRouter()
		addMultiRegAuthMiddleware(r, "mod_user", []string{"moderator"})
		b := router.NewBuilder(r)

		router.RegisterRoutes[MultiRegUser](b, "/users", router.AuthConfig{
			Methods: []string{router.MethodAll},
			Scopes:  []string{router.ScopePublic},
		}, func(b *router.Builder) {
			router.RegisterRoutes[MultiRegItem](b, "/items",
				router.AuthConfig{
					Methods: []string{router.MethodAll},
					Ownership: &router.OwnershipConfig{
						Fields:       []string{"CreatorID"},
						BypassScopes: []string{"moderator"}, // Moderator bypasses
					},
				},
			)
		})

		req := httptest.NewRequest("GET", "/users/1/items", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var items []MultiRegItem
		json.Unmarshal(w.Body.Bytes(), &items)
		if len(items) != 1 {
			t.Errorf("moderator should see user item, got %d items", len(items))
		}
	})
}

// TestBuilder_QueryConfigOptions tests that query config options are properly merged
func TestBuilder_QueryConfigOptions(t *testing.T) {
	multiRegTablesOnce.Do(setupMultiRegTables)

	ds, _ := datastore.Get()
	db := ds.GetDB()
	cleanMultiRegTables(db)
	ctx := context.Background()

	// Create test items with different prices
	items := []MultiRegItem{
		{Title: "Cheap Item", CreatorID: "alice"},
		{Title: "Mid Item", CreatorID: "bob"},
		{Title: "Expensive Item", CreatorID: "charlie"},
	}
	for i := range items {
		db.NewInsert().Model(&items[i]).Returning("*").Exec(ctx)
	}

	// Setup router with query config options
	r := chi.NewRouter()
	b := router.NewBuilder(r)

	router.RegisterRoutes[MultiRegItem](b, "/items",
		router.AuthConfig{
			Methods: []string{router.MethodAll},
			Scopes:  []string{router.ScopePublic},
		},
		router.WithFilters("Title", "CreatorID"),
		router.WithSorts("Title", "ID"),
		router.WithDefaultSort("-ID"),
		router.WithPagination(10, 50),
	)

	t.Run("Filter_ByTitle", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/items?filter[Title]=Cheap%20Item", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var result []MultiRegItem
		json.Unmarshal(w.Body.Bytes(), &result)
		if len(result) != 1 {
			t.Errorf("expected 1 item filtered by title, got %d", len(result))
		}
		if len(result) > 0 && result[0].Title != "Cheap Item" {
			t.Errorf("expected 'Cheap Item', got %q", result[0].Title)
		}
	})

	t.Run("Sort_Ascending", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/items?sort=Title", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var result []MultiRegItem
		json.Unmarshal(w.Body.Bytes(), &result)
		if len(result) != 3 {
			t.Errorf("expected 3 items, got %d", len(result))
		}
		// Alphabetically: Cheap, Expensive, Mid
		if len(result) >= 3 {
			if result[0].Title != "Cheap Item" {
				t.Errorf("expected first item 'Cheap Item', got %q", result[0].Title)
			}
			if result[1].Title != "Expensive Item" {
				t.Errorf("expected second item 'Expensive Item', got %q", result[1].Title)
			}
		}
	})

	t.Run("Pagination_Limit", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/items?limit=2", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var result []MultiRegItem
		json.Unmarshal(w.Body.Bytes(), &result)
		if len(result) != 2 {
			t.Errorf("expected 2 items with limit=2, got %d", len(result))
		}
	})

	t.Run("InvalidFilter_Ignored", func(t *testing.T) {
		// Filter by ID which is not in FilterableFields
		req := httptest.NewRequest("GET", "/items?filter[ID]=1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var result []MultiRegItem
		json.Unmarshal(w.Body.Bytes(), &result)
		// Should return all 3 items (invalid filter ignored)
		if len(result) != 3 {
			t.Errorf("expected 3 items (invalid filter ignored), got %d", len(result))
		}
	})
}

func TestBuilder_ParentValidation(t *testing.T) {
	r, db := setupBuilderTest(t)
	ctx := context.Background()

	// Create two users with posts
	user1 := &TestUser{Name: "User1"}
	if _, err := db.NewInsert().Model(user1).Returning("*").Exec(ctx); err != nil {
		t.Fatalf("failed to create user1: %v", err)
	}

	user2 := &TestUser{Name: "User2"}
	if _, err := db.NewInsert().Model(user2).Returning("*").Exec(ctx); err != nil {
		t.Fatalf("failed to create user2: %v", err)
	}

	post1 := &TestPost{UserID: user1.ID, Title: "User1's Post"}
	if _, err := db.NewInsert().Model(post1).Returning("*").Exec(ctx); err != nil {
		t.Fatalf("failed to create post1: %v", err)
	}

	post2 := &TestPost{UserID: user2.ID, Title: "User2's Post"}
	if _, err := db.NewInsert().Model(post2).Returning("*").Exec(ctx); err != nil {
		t.Fatalf("failed to create post2: %v", err)
	}

	// Request User1's posts
	req := httptest.NewRequest("GET", "/users/1/posts", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var posts []*TestPost
	if err := json.Unmarshal(w.Body.Bytes(), &posts); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Should only get User1's post
	if len(posts) != 1 {
		t.Errorf("expected 1 post for user1, got %d", len(posts))
	}

	if len(posts) > 0 && posts[0].Title != "User1's Post" {
		t.Errorf("expected 'User1's Post', got %q", posts[0].Title)
	}

	// Verify User2's post with User1's ID returns 404
	req = httptest.NewRequest("GET", "/users/1/posts/2", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404 for wrong user's post, got %d", w.Code)
	}
}

// Job is a test model for validation testing with status transitions
type Job struct {
	bun.BaseModel `bun:"table:test_jobs"`
	ID            int    `bun:"id,pk,autoincrement" json:"id"`
	Title         string `bun:"title,notnull" json:"title"`
	Status        string `bun:"status,notnull" json:"status"`
	Priority      int    `bun:"priority" json:"priority"`
}

func setupJobTable() {
	ds, err := datastore.Get()
	if err != nil {
		panic("failed to get datastore: " + err.Error())
	}
	db := ds.GetDB()
	ctx := context.Background()

	// Drop and recreate table
	_, _ = db.NewDropTable().Model((*Job)(nil)).IfExists().Exec(ctx)
	_, err = db.NewCreateTable().Model((*Job)(nil)).IfNotExists().Exec(ctx)
	if err != nil {
		panic("failed to create jobs table: " + err.Error())
	}
}

// TestBuilder_ValidationCreate tests validation on create operations
func TestBuilder_ValidationCreate(t *testing.T) {
	setupJobTable()

	r := chi.NewRouter()
	b := router.NewBuilder(r)

	// Register with validator: new jobs must have status "pending" and priority 1-5
	router.RegisterRoutes[Job](b, "/jobs",
		router.AuthConfig{
			Methods: []string{router.MethodAll},
			Scopes:  []string{router.ScopePublic},
		},
		router.WithValidator(func(vc metadata.ValidationContext[Job]) error {
			if vc.Operation == metadata.OpCreate {
				if vc.New.Status != "pending" {
					return errors.New("new jobs must have status 'pending'")
				}
				if vc.New.Priority < 1 || vc.New.Priority > 5 {
					return errors.New("priority must be between 1 and 5")
				}
			}
			return nil
		}),
	)

	t.Run("valid create", func(t *testing.T) {
		body := `{"title":"Test Job","status":"pending","priority":3}`
		req := httptest.NewRequest("POST", "/jobs", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("invalid status rejected", func(t *testing.T) {
		body := `{"title":"Test Job","status":"complete","priority":3}`
		req := httptest.NewRequest("POST", "/jobs", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
		if !bytes.Contains(w.Body.Bytes(), []byte("pending")) {
			t.Errorf("expected error about pending status, got: %s", w.Body.String())
		}
	})

	t.Run("invalid priority rejected", func(t *testing.T) {
		body := `{"title":"Test Job","status":"pending","priority":10}`
		req := httptest.NewRequest("POST", "/jobs", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
		if !bytes.Contains(w.Body.Bytes(), []byte("priority")) {
			t.Errorf("expected error about priority, got: %s", w.Body.String())
		}
	})
}

// TestBuilder_ValidationUpdate tests validation on update with state transitions
func TestBuilder_ValidationUpdate(t *testing.T) {
	setupJobTable()

	// Insert a job directly for testing updates
	ds, _ := datastore.Get()
	db := ds.GetDB()
	job := &Job{Title: "Existing Job", Status: "pending", Priority: 2}
	_, err := db.NewInsert().Model(job).Returning("*").Exec(context.Background())
	if err != nil {
		t.Fatal("failed to insert test job:", err)
	}

	r := chi.NewRouter()
	b := router.NewBuilder(r)

	// Validator: can only move to "in_progress" from "pending", and to "complete" from "in_progress"
	router.RegisterRoutes[Job](b, "/jobs",
		router.AuthConfig{
			Methods: []string{router.MethodAll},
			Scopes:  []string{router.ScopePublic},
		},
		router.WithValidator(func(vc metadata.ValidationContext[Job]) error {
			if vc.Operation == metadata.OpUpdate {
				oldStatus := vc.Old.Status
				newStatus := vc.New.Status

				// Define valid transitions
				validTransitions := map[string][]string{
					"pending":     {"pending", "in_progress"},
					"in_progress": {"in_progress", "complete"},
					"complete":    {"complete"}, // can't change once complete
				}

				allowed := validTransitions[oldStatus]
				valid := false
				for _, s := range allowed {
					if s == newStatus {
						valid = true
						break
					}
				}
				if !valid {
					return errors.New("invalid status transition from " + oldStatus + " to " + newStatus)
				}
			}
			return nil
		}),
	)

	t.Run("valid transition pending to in_progress", func(t *testing.T) {
		body := `{"id":1,"title":"Existing Job","status":"in_progress","priority":2}`
		req := httptest.NewRequest("PUT", "/jobs/1", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("invalid transition pending to complete", func(t *testing.T) {
		// Reset job to pending
		_, _ = db.NewUpdate().Model(&Job{ID: 1, Status: "pending"}).Column("status").WherePK().Exec(context.Background())

		body := `{"id":1,"title":"Existing Job","status":"complete","priority":2}`
		req := httptest.NewRequest("PUT", "/jobs/1", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
		if !bytes.Contains(w.Body.Bytes(), []byte("invalid status transition")) {
			t.Errorf("expected transition error, got: %s", w.Body.String())
		}
	})
}

// TestBuilder_ValidationDelete tests validation on delete operations
func TestBuilder_ValidationDelete(t *testing.T) {
	setupJobTable()

	// Insert jobs with different statuses
	ds, _ := datastore.Get()
	db := ds.GetDB()
	pendingJob := &Job{Title: "Pending Job", Status: "pending", Priority: 1}
	inProgressJob := &Job{Title: "In Progress Job", Status: "in_progress", Priority: 2}
	_, _ = db.NewInsert().Model(pendingJob).Returning("*").Exec(context.Background())
	_, _ = db.NewInsert().Model(inProgressJob).Returning("*").Exec(context.Background())

	r := chi.NewRouter()
	b := router.NewBuilder(r)

	// Validator: can't delete jobs that are in_progress
	router.RegisterRoutes[Job](b, "/jobs",
		router.AuthConfig{
			Methods: []string{router.MethodAll},
			Scopes:  []string{router.ScopePublic},
		},
		router.WithValidator(func(vc metadata.ValidationContext[Job]) error {
			if vc.Operation == metadata.OpDelete {
				if vc.Old.Status == "in_progress" {
					return errors.New("cannot delete jobs that are in progress")
				}
			}
			return nil
		}),
	)

	t.Run("can delete pending job", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/jobs/1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("cannot delete in_progress job", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/jobs/2", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
		if !bytes.Contains(w.Body.Bytes(), []byte("in progress")) {
			t.Errorf("expected error about in progress, got: %s", w.Body.String())
		}
	})
}

// AuditedTask is a model for testing audit functionality
type AuditedTask struct {
	bun.BaseModel `bun:"table:audited_tasks"`
	ID            int    `bun:"id,pk,autoincrement" json:"id"`
	Title         string `bun:"title,notnull" json:"title"`
	Status        string `bun:"status,notnull" json:"status"`
}

// TaskAuditLog is the audit log for AuditedTask
type TaskAuditLog struct {
	bun.BaseModel `bun:"table:task_audit_logs"`
	ID            int    `bun:"id,pk,autoincrement" json:"id"`
	TaskID        int    `bun:"task_id" json:"task_id"`
	Operation     string `bun:"operation,notnull" json:"operation"`
	OldStatus     string `bun:"old_status" json:"old_status"`
	NewStatus     string `bun:"new_status" json:"new_status"`
}

func TestWithAudit(t *testing.T) {
	// Get the global datastore (initialized in TestMain)
	db, err := datastore.Get()
	if err != nil {
		t.Fatal("failed to get datastore:", err)
	}

	// Create tables
	_, err = db.GetDB().NewCreateTable().Model((*AuditedTask)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("failed to create tasks table:", err)
	}
	_, err = db.GetDB().NewCreateTable().Model((*TaskAuditLog)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("failed to create audit logs table:", err)
	}
	defer func() {
		db.GetDB().NewDropTable().Model((*TaskAuditLog)(nil)).IfExists().Exec(context.Background())
		db.GetDB().NewDropTable().Model((*AuditedTask)(nil)).IfExists().Exec(context.Background())
	}()

	// Setup router with audit
	r := chi.NewRouter()
	b := router.NewBuilder(r)

	router.RegisterRoutes[AuditedTask](b, "/tasks",
		router.AuthConfig{
			Methods: []string{router.MethodAll},
			Scopes:  []string{router.ScopePublic},
		},
		router.WithAudit(func(ac metadata.AuditContext[AuditedTask]) any {
			oldStatus := ""
			if ac.Old != nil {
				oldStatus = ac.Old.Status
			}
			newStatus := ""
			if ac.New != nil {
				newStatus = ac.New.Status
			}
			taskID := 0
			if ac.New != nil {
				taskID = ac.New.ID
			} else if ac.Old != nil {
				taskID = ac.Old.ID
			}
			return &TaskAuditLog{
				TaskID:    taskID,
				Operation: string(ac.Operation),
				OldStatus: oldStatus,
				NewStatus: newStatus,
			}
		}),
	)

	t.Run("create generates audit log", func(t *testing.T) {
		// Clear any existing data
		db.GetDB().NewDelete().Model((*TaskAuditLog)(nil)).Where("1=1").Exec(context.Background())
		db.GetDB().NewDelete().Model((*AuditedTask)(nil)).Where("1=1").Exec(context.Background())

		body := `{"title":"Test Task","status":"pending"}`
		req := httptest.NewRequest("POST", "/tasks", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}

		// Verify audit log was created
		var logs []TaskAuditLog
		err := db.GetDB().NewSelect().Model(&logs).Scan(context.Background())
		if err != nil {
			t.Fatal("failed to query audit logs:", err)
		}

		if len(logs) != 1 {
			t.Fatalf("expected 1 audit log, got %d", len(logs))
		}

		if logs[0].Operation != "create" {
			t.Errorf("expected operation 'create', got '%s'", logs[0].Operation)
		}
		if logs[0].NewStatus != "pending" {
			t.Errorf("expected NewStatus 'pending', got '%s'", logs[0].NewStatus)
		}
	})

	t.Run("update generates audit log with old and new", func(t *testing.T) {
		// Clear and create fresh data
		db.GetDB().NewDelete().Model((*TaskAuditLog)(nil)).Where("1=1").Exec(context.Background())
		db.GetDB().NewDelete().Model((*AuditedTask)(nil)).Where("1=1").Exec(context.Background())

		// Create a task first
		task := &AuditedTask{Title: "Update Test", Status: "pending"}
		_, err := db.GetDB().NewInsert().Model(task).Returning("*").Exec(context.Background())
		if err != nil {
			t.Fatal("failed to create task:", err)
		}

		// Update the task
		body := fmt.Sprintf(`{"id":%d,"title":"Update Test","status":"active"}`, task.ID)
		req := httptest.NewRequest("PUT", fmt.Sprintf("/tasks/%d", task.ID), bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		// Verify audit log
		var logs []TaskAuditLog
		err = db.GetDB().NewSelect().Model(&logs).Scan(context.Background())
		if err != nil {
			t.Fatal("failed to query audit logs:", err)
		}

		if len(logs) != 1 {
			t.Fatalf("expected 1 audit log, got %d", len(logs))
		}

		if logs[0].Operation != "update" {
			t.Errorf("expected operation 'update', got '%s'", logs[0].Operation)
		}
		if logs[0].OldStatus != "pending" {
			t.Errorf("expected OldStatus 'pending', got '%s'", logs[0].OldStatus)
		}
		if logs[0].NewStatus != "active" {
			t.Errorf("expected NewStatus 'active', got '%s'", logs[0].NewStatus)
		}
	})

	t.Run("delete generates audit log", func(t *testing.T) {
		// Clear and create fresh data
		db.GetDB().NewDelete().Model((*TaskAuditLog)(nil)).Where("1=1").Exec(context.Background())
		db.GetDB().NewDelete().Model((*AuditedTask)(nil)).Where("1=1").Exec(context.Background())

		// Create a task first
		task := &AuditedTask{Title: "Delete Test", Status: "completed"}
		_, err := db.GetDB().NewInsert().Model(task).Returning("*").Exec(context.Background())
		if err != nil {
			t.Fatal("failed to create task:", err)
		}

		// Delete the task
		req := httptest.NewRequest("DELETE", fmt.Sprintf("/tasks/%d", task.ID), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
		}

		// Verify audit log
		var logs []TaskAuditLog
		err = db.GetDB().NewSelect().Model(&logs).Scan(context.Background())
		if err != nil {
			t.Fatal("failed to query audit logs:", err)
		}

		if len(logs) != 1 {
			t.Fatalf("expected 1 audit log, got %d", len(logs))
		}

		if logs[0].Operation != "delete" {
			t.Errorf("expected operation 'delete', got '%s'", logs[0].Operation)
		}
		if logs[0].OldStatus != "completed" {
			t.Errorf("expected OldStatus 'completed', got '%s'", logs[0].OldStatus)
		}
	})
}
