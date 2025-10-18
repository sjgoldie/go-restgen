//nolint:dupl,errcheck,gosec // Test code - duplicate test patterns and unchecked test cleanup are acceptable
package router_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/sjgoldie/go-restgen/datastore"
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
