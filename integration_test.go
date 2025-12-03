//nolint:dupl,errcheck,gosec // Test code - duplicate test patterns and unchecked test assertions are acceptable
package restgen_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/router"
	"github.com/uptrace/bun"
)

// User model for simple integration tests
type User struct {
	bun.BaseModel `bun:"table:users"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	Name          string    `bun:"name" json:"name"`
	Email         string    `bun:"email" json:"email"`
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
}

func (u *User) BeforeAppendModel(ctx context.Context, query bun.Query) error {
	now := time.Now()
	switch query.(type) {
	case *bun.InsertQuery:
		u.CreatedAt = now
		u.UpdatedAt = now
	case *bun.UpdateQuery:
		u.UpdatedAt = now
	}
	return nil
}

// Nested resource models for integration tests
type Author struct {
	bun.BaseModel `bun:"table:authors"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	Name          string    `bun:"name,notnull" json:"name"`
	Email         string    `bun:"email,notnull" json:"email"`
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
}

func (a *Author) BeforeAppendModel(ctx context.Context, query bun.Query) error {
	now := time.Now()
	switch query.(type) {
	case *bun.InsertQuery:
		a.CreatedAt = now
		a.UpdatedAt = now
	case *bun.UpdateQuery:
		a.UpdatedAt = now
	}
	return nil
}

type Article struct {
	bun.BaseModel `bun:"table:articles"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	AuthorID      int       `bun:"author_id,notnull,skipupdate" json:"author_id"`
	Title         string    `bun:"title,notnull" json:"title"`
	Content       string    `bun:"content" json:"content"`
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`

	// Relation field for router to detect parent
	AuthorRel *Author `bun:"rel:belongs-to,join:author_id=id" json:"-"`
}

func (a *Article) BeforeAppendModel(ctx context.Context, query bun.Query) error {
	now := time.Now()
	switch query.(type) {
	case *bun.InsertQuery:
		a.CreatedAt = now
		a.UpdatedAt = now
	case *bun.UpdateQuery:
		a.UpdatedAt = now
	}
	return nil
}

type Comment struct {
	bun.BaseModel `bun:"table:comments"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	ArticleID     int       `bun:"article_id,notnull,skipupdate" json:"article_id"`
	Text          string    `bun:"text,notnull" json:"text"`
	Author        string    `bun:"author,notnull" json:"author"`
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`

	// Relation field for router to detect parent
	ArticleRel *Article `bun:"rel:belongs-to,join:article_id=id" json:"-"`
}

func (c *Comment) BeforeAppendModel(ctx context.Context, query bun.Query) error {
	now := time.Now()
	switch query.(type) {
	case *bun.InsertQuery:
		c.CreatedAt = now
		c.UpdatedAt = now
	case *bun.UpdateQuery:
		c.UpdatedAt = now
	}
	return nil
}

var testDB *datastore.SQLite

func TestMain(m *testing.M) {
	// Enable logging for tests
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))
	slog.SetDefault(logger)

	var err error
	testDB, err = datastore.NewSQLite(":memory:")
	if err != nil {
		panic("Failed to create test database: " + err.Error())
	}

	if err := datastore.Initialize(testDB); err != nil {
		testDB.Cleanup()
		panic("Failed to initialize datastore: " + err.Error())
	}

	// Create tables
	// Note: Metadata is now created automatically by RegisterRoutes via context injection
	_, err = testDB.GetDB().NewCreateTable().Model((*User)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		testDB.Cleanup()
		panic("Failed to create users table: " + err.Error())
	}

	_, err = testDB.GetDB().NewCreateTable().Model((*Author)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		testDB.Cleanup()
		panic("Failed to create authors table: " + err.Error())
	}

	_, err = testDB.GetDB().NewCreateTable().Model((*Article)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		testDB.Cleanup()
		panic("Failed to create articles table: " + err.Error())
	}

	_, err = testDB.GetDB().NewCreateTable().Model((*Comment)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		testDB.Cleanup()
		panic("Failed to create comments table: " + err.Error())
	}

	code := m.Run()

	testDB.GetDB().NewDropTable().Model((*Comment)(nil)).IfExists().Exec(context.Background()) //nolint:errcheck // Test cleanup
	testDB.GetDB().NewDropTable().Model((*Article)(nil)).IfExists().Exec(context.Background()) //nolint:errcheck // Test cleanup
	testDB.GetDB().NewDropTable().Model((*Author)(nil)).IfExists().Exec(context.Background())  //nolint:errcheck // Test cleanup
	testDB.GetDB().NewDropTable().Model((*User)(nil)).IfExists().Exec(context.Background())
	datastore.Cleanup()
	testDB.Cleanup()

	os.Exit(code)
}

func cleanTable(t *testing.T) {
	t.Helper()
	db, _ := datastore.Get()
	_, err := db.GetDB().NewDelete().Model((*User)(nil)).Where("1=1").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to clean table:", err)
	}
	_, _ = db.GetDB().Exec("DELETE FROM sqlite_sequence WHERE name = 'users'")
}

func cleanNestedTables(t *testing.T) {
	t.Helper()
	db, _ := datastore.Get()

	// Clean in reverse dependency order
	_, err := db.GetDB().NewDelete().Model((*Comment)(nil)).Where("1=1").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to clean comments table:", err)
	}

	_, err = db.GetDB().NewDelete().Model((*Article)(nil)).Where("1=1").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to clean articles table:", err)
	}

	_, err = db.GetDB().NewDelete().Model((*Author)(nil)).Where("1=1").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to clean authors table:", err)
	}

	// Reset auto-increment sequences
	_, _ = db.GetDB().Exec("DELETE FROM sqlite_sequence WHERE name = 'comments'")
	_, _ = db.GetDB().Exec("DELETE FROM sqlite_sequence WHERE name = 'articles'")
	_, _ = db.GetDB().Exec("DELETE FROM sqlite_sequence WHERE name = 'authors'")
}

func TestIntegration_FullCRUDWorkflow(t *testing.T) {
	cleanTable(t)

	// Setup router
	r := chi.NewRouter()
	b := router.NewBuilder(r)
	router.RegisterRoutes[User](b, "/users", router.AuthConfig{
		Methods: []string{router.MethodAll},
		Scopes:  []string{router.ScopePublic},
	})

	t.Run("Create user", func(t *testing.T) {
		user := User{
			Name:  "John Doe",
			Email: "john@example.com",
		}
		body, _ := json.Marshal(user)

		req := httptest.NewRequest(http.MethodPost, "/users/", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected status %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
		}

		var created User
		if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
			t.Fatal("Failed to decode response:", err)
		}

		if created.ID == 0 {
			t.Error("Expected ID to be set")
		}
		if created.Name != user.Name {
			t.Errorf("Expected name '%s', got '%s'", user.Name, created.Name)
		}
	})

	t.Run("Get user by ID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/users/1", nil)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		var user User
		if err := json.NewDecoder(w.Body).Decode(&user); err != nil {
			t.Fatal("Failed to decode response:", err)
		}

		if user.Name != "John Doe" {
			t.Errorf("Expected name 'John Doe', got '%s'", user.Name)
		}
	})

	t.Run("Update user", func(t *testing.T) {
		user := User{
			ID:    1,
			Name:  "Jane Doe",
			Email: "jane@example.com",
		}
		body, _ := json.Marshal(user)

		req := httptest.NewRequest(http.MethodPut, "/users/1", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
		}

		var updated User
		if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
			t.Fatal("Failed to decode response:", err)
		}

		if updated.Name != "Jane Doe" {
			t.Errorf("Expected name 'Jane Doe', got '%s'", updated.Name)
		}
	})

	t.Run("Get all users", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/users/", nil)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		var users []User
		if err := json.NewDecoder(w.Body).Decode(&users); err != nil {
			t.Fatal("Failed to decode response:", err)
		}

		if len(users) != 1 {
			t.Errorf("Expected 1 user, got %d", len(users))
		}
	})

	t.Run("Delete user", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/users/1", nil)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Fatalf("Expected status %d, got %d", http.StatusNoContent, w.Code)
		}
	})

	t.Run("Verify deletion", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/users/1", nil)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status %d, got %d", http.StatusNotFound, w.Code)
		}
	})
}

func TestIntegration_MultipleUsers(t *testing.T) {
	cleanTable(t)

	r := chi.NewRouter()
	b := router.NewBuilder(r)
	router.RegisterRoutes[User](b, "/users", router.AuthConfig{
		Methods: []string{router.MethodAll},
		Scopes:  []string{router.ScopePublic},
	})

	// Create multiple users
	users := []User{
		{Name: "User 1", Email: "user1@example.com"},
		{Name: "User 2", Email: "user2@example.com"},
		{Name: "User 3", Email: "user3@example.com"},
	}

	for i, user := range users {
		body, _ := json.Marshal(user)
		req := httptest.NewRequest(http.MethodPost, "/users/", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("Failed to create user %d: status %d", i+1, w.Code)
		}
	}

	// Get all users
	req := httptest.NewRequest(http.MethodGet, "/users/", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var retrieved []User
	if err := json.NewDecoder(w.Body).Decode(&retrieved); err != nil {
		t.Fatal("Failed to decode response:", err)
	}

	if len(retrieved) != len(users) {
		t.Errorf("Expected %d users, got %d", len(users), len(retrieved))
	}

	// Verify each user
	for i, user := range retrieved {
		if user.Name != users[i].Name {
			t.Errorf("User %d: expected name '%s', got '%s'", i+1, users[i].Name, user.Name)
		}
	}
}

func TestIntegration_ErrorHandling(t *testing.T) {
	cleanTable(t)

	r := chi.NewRouter()
	b := router.NewBuilder(r)
	router.RegisterRoutes[User](b, "/users", router.AuthConfig{
		Methods: []string{router.MethodAll},
		Scopes:  []string{router.ScopePublic},
	})

	t.Run("Get non-existent user", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/users/999", nil)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status %d, got %d", http.StatusNotFound, w.Code)
		}
	})

	t.Run("Update non-existent user", func(t *testing.T) {
		user := User{
			ID:    999,
			Name:  "Does Not Exist",
			Email: "notexist@example.com",
		}
		body, _ := json.Marshal(user)

		req := httptest.NewRequest(http.MethodPut, "/users/999", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status %d, got %d", http.StatusNotFound, w.Code)
		}
	})

	t.Run("Delete non-existent user", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/users/999", nil)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status %d, got %d", http.StatusNotFound, w.Code)
		}
	})

	t.Run("Create with invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/users/", bytes.NewReader([]byte("invalid json")))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
		}
	})

	t.Run("Update with invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/users/1", bytes.NewReader([]byte("invalid json")))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
		}
	})
}

func TestIntegration_CompleteLifecycle(t *testing.T) {
	cleanTable(t)

	r := chi.NewRouter()
	b := router.NewBuilder(r)
	router.RegisterRoutes[User](b, "/users", router.AuthConfig{
		Methods: []string{router.MethodAll},
		Scopes:  []string{router.ScopePublic},
	})

	// Create first user
	user1 := User{Name: "User 1", Email: "user1@example.com"}
	body, _ := json.Marshal(user1)
	req := httptest.NewRequest(http.MethodPost, "/users/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatal("Failed to create first user")
	}

	// Create second user
	user2 := User{Name: "User 2", Email: "user2@example.com"}
	body, _ = json.Marshal(user2)
	req = httptest.NewRequest(http.MethodPost, "/users/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatal("Failed to create second user")
	}

	// Get all users - should have 2
	req = httptest.NewRequest(http.MethodGet, "/users/", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var users []User
	json.NewDecoder(w.Body).Decode(&users) //nolint:errcheck // Test assertion
	if len(users) != 2 {
		t.Errorf("Expected 2 users, got %d", len(users))
	}

	// Update first user
	user1Update := User{ID: 1, Name: "Updated User 1", Email: "updated1@example.com"}
	body, _ = json.Marshal(user1Update)
	req = httptest.NewRequest(http.MethodPut, "/users/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatal("Failed to update user")
	}

	// Delete second user
	req = httptest.NewRequest(http.MethodDelete, "/users/2", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatal("Failed to delete user")
	}

	// Get all users - should have 1
	req = httptest.NewRequest(http.MethodGet, "/users/", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	users = []User{}
	json.NewDecoder(w.Body).Decode(&users) //nolint:errcheck // Test assertion
	if len(users) != 1 {
		t.Errorf("Expected 1 user after deletion, got %d", len(users))
	}
	if users[0].Name != "Updated User 1" {
		t.Errorf("Expected updated name, got '%s'", users[0].Name)
	}
}

// Nested resource integration tests

func TestIntegration_NestedResources_TwoLevels(t *testing.T) {
	cleanNestedTables(t)

	// Setup router with nested routes
	r := chi.NewRouter()
	b := router.NewBuilder(r)

	// Register root resource (Authors) with nested Articles
	router.RegisterRoutes[Author](b, "/authors", router.AuthConfig{
		Methods: []string{router.MethodAll},
		Scopes:  []string{router.ScopePublic},
	}, func(b *router.Builder) {
		router.RegisterRoutes[Article](b, "/articles", router.AuthConfig{
			Methods: []string{router.MethodAll},
			Scopes:  []string{router.ScopePublic},
		})
	})

	t.Run("Create author", func(t *testing.T) {
		author := Author{
			Name:  "John Doe",
			Email: "john@example.com",
		}
		body, _ := json.Marshal(author)

		req := httptest.NewRequest(http.MethodPost, "/authors/", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected status %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
		}

		var created Author
		json.NewDecoder(w.Body).Decode(&created)
		if created.ID == 0 {
			t.Error("Expected ID to be set")
		}
	})

	t.Run("Create article under author", func(t *testing.T) {
		article := Article{
			Title:   "Test Article",
			Content: "Article content here",
		}
		body, _ := json.Marshal(article)

		req := httptest.NewRequest(http.MethodPost, "/authors/1/articles/", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected status %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
		}

		var created Article
		json.NewDecoder(w.Body).Decode(&created)
		if created.ID == 0 {
			t.Error("Expected ID to be set")
		}
		if created.AuthorID != 1 {
			t.Errorf("Expected AuthorID to be 1, got %d", created.AuthorID)
		}
	})

	t.Run("Get article by ID with correct parent", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/authors/1/articles/1", nil)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		var article Article
		json.NewDecoder(w.Body).Decode(&article)
		if article.Title != "Test Article" {
			t.Errorf("Expected title 'Test Article', got '%s'", article.Title)
		}
	})

	t.Run("Get article with wrong parent fails", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/authors/999/articles/1", nil)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status %d (not found with wrong parent), got %d", http.StatusNotFound, w.Code)
		}
	})

	t.Run("GetAll articles for author", func(t *testing.T) {
		// Create another article
		article := Article{Title: "Second Article", Content: "More content"}
		body, _ := json.Marshal(article)
		req := httptest.NewRequest(http.MethodPost, "/authors/1/articles/", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Get all articles
		req = httptest.NewRequest(http.MethodGet, "/authors/1/articles/", nil)
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		var articles []Article
		json.NewDecoder(w.Body).Decode(&articles)
		if len(articles) != 2 {
			t.Errorf("Expected 2 articles, got %d", len(articles))
		}
	})

	t.Run("Update article with correct parent", func(t *testing.T) {
		article := Article{
			ID:      1,
			Title:   "Updated Title",
			Content: "Updated content",
		}
		body, _ := json.Marshal(article)

		req := httptest.NewRequest(http.MethodPut, "/authors/1/articles/1", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
		}

		var updated Article
		json.NewDecoder(w.Body).Decode(&updated)
		if updated.Title != "Updated Title" {
			t.Errorf("Expected title 'Updated Title', got '%s'", updated.Title)
		}
	})

	t.Run("Update article with wrong parent fails", func(t *testing.T) {
		article := Article{ID: 1, Title: "Should Fail", Content: "content"}
		body, _ := json.Marshal(article)

		req := httptest.NewRequest(http.MethodPut, "/authors/999/articles/1", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status %d (update with wrong parent should fail), got %d", http.StatusNotFound, w.Code)
		}
	})

	t.Run("Delete article with correct parent", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/authors/1/articles/1", nil)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Fatalf("Expected status %d, got %d", http.StatusNoContent, w.Code)
		}
	})

	t.Run("Delete article with wrong parent fails", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/authors/999/articles/2", nil)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status %d (delete with wrong parent should fail), got %d", http.StatusNotFound, w.Code)
		}
	})
}

func TestIntegration_NestedResources_ThreeLevels(t *testing.T) {
	cleanNestedTables(t)

	// Setup router with three-level nested routes
	r := chi.NewRouter()
	b := router.NewBuilder(r)

	// Register root resource (Authors) with nested Articles, and Articles with nested Comments
	router.RegisterRoutes[Author](b, "/authors", router.AuthConfig{
		Methods: []string{router.MethodAll},
		Scopes:  []string{router.ScopePublic},
	}, func(b *router.Builder) {
		router.RegisterRoutes[Article](b, "/articles", router.AuthConfig{
			Methods: []string{router.MethodAll},
			Scopes:  []string{router.ScopePublic},
		}, func(b *router.Builder) {
			router.RegisterRoutes[Comment](b, "/comments", router.AuthConfig{
				Methods: []string{router.MethodAll},
				Scopes:  []string{router.ScopePublic},
			})
		})
	})

	// Create test data
	t.Run("Setup test data", func(t *testing.T) {
		// Create author
		author := Author{Name: "Author", Email: "author@example.com"}
		body, _ := json.Marshal(author)
		req := httptest.NewRequest(http.MethodPost, "/authors/", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("Failed to create author: %d", w.Code)
		}

		// Create article
		article := Article{Title: "Article", Content: "Content"}
		body, _ = json.Marshal(article)
		req = httptest.NewRequest(http.MethodPost, "/authors/1/articles/", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("Failed to create article: %d", w.Code)
		}
	})

	t.Run("Create comment with full parent chain", func(t *testing.T) {
		comment := Comment{
			Text:   "Great article!",
			Author: "Reader",
		}
		body, _ := json.Marshal(comment)

		req := httptest.NewRequest(http.MethodPost, "/authors/1/articles/1/comments/", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected status %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
		}

		var created Comment
		json.NewDecoder(w.Body).Decode(&created)
		if created.ID == 0 {
			t.Error("Expected ID to be set")
		}
		if created.ArticleID != 1 {
			t.Errorf("Expected ArticleID to be 1, got %d", created.ArticleID)
		}
	})

	t.Run("Get comment with correct parent chain", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/authors/1/articles/1/comments/1", nil)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		var comment Comment
		json.NewDecoder(w.Body).Decode(&comment)
		if comment.Text != "Great article!" {
			t.Errorf("Expected text 'Great article!', got '%s'", comment.Text)
		}
	})

	t.Run("Get comment with wrong article fails", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/authors/1/articles/999/comments/1", nil)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status %d (wrong article), got %d", http.StatusNotFound, w.Code)
		}
	})

	t.Run("Get comment with wrong author fails", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/authors/999/articles/1/comments/1", nil)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status %d (wrong author), got %d", http.StatusNotFound, w.Code)
		}
	})

	t.Run("GetAll comments for article", func(t *testing.T) {
		// Create another comment
		comment := Comment{Text: "Another comment", Author: "Another Reader"}
		body, _ := json.Marshal(comment)
		req := httptest.NewRequest(http.MethodPost, "/authors/1/articles/1/comments/", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Get all comments
		req = httptest.NewRequest(http.MethodGet, "/authors/1/articles/1/comments/", nil)
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		var comments []Comment
		json.NewDecoder(w.Body).Decode(&comments)
		if len(comments) != 2 {
			t.Errorf("Expected 2 comments, got %d", len(comments))
		}
	})

	t.Run("Update comment with correct parent chain", func(t *testing.T) {
		comment := Comment{
			ID:     1,
			Text:   "Updated comment",
			Author: "Reader",
		}
		body, _ := json.Marshal(comment)

		req := httptest.NewRequest(http.MethodPut, "/authors/1/articles/1/comments/1", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
		}

		var updated Comment
		json.NewDecoder(w.Body).Decode(&updated)
		if updated.Text != "Updated comment" {
			t.Errorf("Expected text 'Updated comment', got '%s'", updated.Text)
		}
	})

	t.Run("Delete comment with correct parent chain", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/authors/1/articles/1/comments/1", nil)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Fatalf("Expected status %d, got %d", http.StatusNoContent, w.Code)
		}

		// Verify deletion
		req = httptest.NewRequest(http.MethodGet, "/authors/1/articles/1/comments/1", nil)
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status %d after deletion, got %d", http.StatusNotFound, w.Code)
		}
	})
}

func TestIntegration_NestedResources_Isolation(t *testing.T) {
	cleanNestedTables(t)

	r := chi.NewRouter()
	b := router.NewBuilder(r)
	router.RegisterRoutes[Author](b, "/authors", router.AuthConfig{
		Methods: []string{router.MethodAll},
		Scopes:  []string{router.ScopePublic},
	}, func(b *router.Builder) {
		router.RegisterRoutes[Article](b, "/articles", router.AuthConfig{
			Methods: []string{router.MethodAll},
			Scopes:  []string{router.ScopePublic},
		})
	})

	// Create two authors with articles
	t.Run("Setup isolated resources", func(t *testing.T) {
		// Author 1
		author1 := Author{Name: "Author 1", Email: "author1@example.com"}
		body, _ := json.Marshal(author1)
		req := httptest.NewRequest(http.MethodPost, "/authors/", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Author 2
		author2 := Author{Name: "Author 2", Email: "author2@example.com"}
		body, _ = json.Marshal(author2)
		req = httptest.NewRequest(http.MethodPost, "/authors/", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Articles for Author 1
		article := Article{Title: "Author 1 Article 1", Content: "Content"}
		body, _ = json.Marshal(article)
		req = httptest.NewRequest(http.MethodPost, "/authors/1/articles/", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)

		article = Article{Title: "Author 1 Article 2", Content: "Content"}
		body, _ = json.Marshal(article)
		req = httptest.NewRequest(http.MethodPost, "/authors/1/articles/", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Articles for Author 2
		article = Article{Title: "Author 2 Article 1", Content: "Content"}
		body, _ = json.Marshal(article)
		req = httptest.NewRequest(http.MethodPost, "/authors/2/articles/", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)
	})

	t.Run("GetAll articles for Author 1 returns only Author 1 articles", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/authors/1/articles/", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		var articles []Article
		json.NewDecoder(w.Body).Decode(&articles)
		if len(articles) != 2 {
			t.Errorf("Expected 2 articles for Author 1, got %d", len(articles))
		}
		for _, article := range articles {
			if article.AuthorID != 1 {
				t.Errorf("Expected all articles to belong to Author 1, found AuthorID %d", article.AuthorID)
			}
		}
	})

	t.Run("GetAll articles for Author 2 returns only Author 2 articles", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/authors/2/articles/", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		var articles []Article
		json.NewDecoder(w.Body).Decode(&articles)
		if len(articles) != 1 {
			t.Errorf("Expected 1 article for Author 2, got %d", len(articles))
		}
		if articles[0].AuthorID != 2 {
			t.Errorf("Expected article to belong to Author 2, got AuthorID %d", articles[0].AuthorID)
		}
	})

	t.Run("Cannot access Author 1 article via Author 2 route", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/authors/2/articles/1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status %d (article 1 belongs to Author 1, not 2), got %d", http.StatusNotFound, w.Code)
		}
	})
}
