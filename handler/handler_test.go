//nolint:staticcheck,errcheck,gosec // Test code - string context keys and unchecked test cleanup are acceptable
package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sjgoldie/go-restgen/datastore"
	apperrors "github.com/sjgoldie/go-restgen/errors"
	"github.com/sjgoldie/go-restgen/handler"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/uptrace/bun"
)

// Test route paths as constants to avoid duplication
const (
	testUsersPath = "/users"
)

// TestUser is a test model
type TestUser struct {
	bun.BaseModel `bun:"table:users"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	Name          string    `bun:"name,notnull" json:"name"`
	Email         string    `bun:"email,unique" json:"email"`
	CreatedAt     time.Time `bun:"created_at,notnull,default:current_timestamp" json:"created_at,omitempty"`
}

// TestPost is a child model for testing foreign key errors
type TestPost struct {
	bun.BaseModel `bun:"table:posts"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	UserID        int       `bun:"user_id,notnull" json:"user_id"`
	Title         string    `bun:"title,notnull" json:"title"`
	CreatedAt     time.Time `bun:"created_at,notnull,default:current_timestamp" json:"created_at,omitempty"`
}

// Global test database for all handler tests
var testDB *datastore.SQLite

// Test metadata for injecting into context
var userMeta = &metadata.TypeMetadata{
	TypeID:        "test_user_id",
	TypeName:      "TestUser",
	TableName:     "users",
	URLParamUUID:  "id",
	ModelType:     reflect.TypeOf(TestUser{}),
	ParentType:    nil,
	ForeignKeyCol: "",
}

// withMeta creates middleware that injects metadata into context
func withMeta(meta *metadata.TypeMetadata) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), metadata.MetadataKey, meta)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func TestMain(m *testing.M) {
	// Setup: create database once for all tests
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
	_, err = testDB.GetDB().NewCreateTable().Model((*TestUser)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		testDB.Cleanup()
		panic("Failed to create users table: " + err.Error())
	}

	// Create posts table with foreign key
	_, err = testDB.GetDB().NewCreateTable().
		Model((*TestPost)(nil)).
		ForeignKey(`("user_id") REFERENCES "users" ("id") ON DELETE CASCADE`).
		IfNotExists().
		Exec(context.Background())
	if err != nil {
		testDB.Cleanup()
		panic("Failed to create posts table: " + err.Error())
	}

	// Run tests
	code := m.Run()

	// Teardown
	testDB.GetDB().NewDropTable().Model((*TestPost)(nil)).IfExists().Exec(context.Background())
	testDB.GetDB().NewDropTable().Model((*TestUser)(nil)).IfExists().Exec(context.Background())
	datastore.Cleanup()
	testDB.Cleanup()

	os.Exit(code)
}

func cleanTable(t *testing.T) {
	t.Helper()
	db, _ := datastore.Get()

	// Delete all posts first (due to foreign key constraint)
	_, err := db.GetDB().NewDelete().Model((*TestPost)(nil)).Where("1=1").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to clean posts table:", err)
	}

	// Delete all users
	_, err = db.GetDB().NewDelete().Model((*TestUser)(nil)).Where("1=1").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to clean users table:", err)
	}

	// Reset SQLite auto-increment sequences (ignore errors if sequence table doesn't exist)
	_, _ = db.GetDB().Exec("DELETE FROM sqlite_sequence WHERE name = 'posts'")
	_, _ = db.GetDB().Exec("DELETE FROM sqlite_sequence WHERE name = 'users'")
}

func TestHandler_GetAll(t *testing.T) {
	tests := []struct {
		name          string
		setupData     []TestUser
		expectedCount int
		expectedCode  int
	}{
		{
			name:          "empty table",
			setupData:     []TestUser{},
			expectedCount: 0,
			expectedCode:  http.StatusOK,
		},
		{
			name: "multiple users",
			setupData: []TestUser{
				{Name: "User 1", Email: "user1@example.com"},
				{Name: "User 2", Email: "user2@example.com"},
			},
			expectedCount: 2,
			expectedCode:  http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanTable(t)

			// Setup test data
			db, _ := datastore.Get()
			for _, user := range tt.setupData {
				_, err := db.GetDB().NewInsert().Model(&user).Exec(context.Background())
				if err != nil {
					t.Fatal("Failed to insert test user:", err)
				}
			}

			// Make request
			r := chi.NewRouter()
			r.Use(withMeta(userMeta))
			r.Get("/users", handler.GetAll[TestUser]())

			req := httptest.NewRequest(http.MethodGet, "/users", nil)
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != tt.expectedCode {
				t.Errorf("Expected status %d, got %d", tt.expectedCode, w.Code)
			}

			var result []TestUser
			if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
				t.Fatal("Failed to decode response:", err)
			}

			if len(result) != tt.expectedCount {
				t.Errorf("Expected %d users, got %d", tt.expectedCount, len(result))
			}
		})
	}
}

func TestHandler_Get(t *testing.T) {
	tests := []struct {
		name         string
		setupUser    *TestUser
		requestID    string
		expectedCode int
		expectedName string
	}{
		{
			name:         "existing user",
			setupUser:    &TestUser{Name: "Test User", Email: "test@example.com"},
			requestID:    "1",
			expectedCode: http.StatusOK,
			expectedName: "Test User",
		},
		{
			name:         "not found",
			setupUser:    nil,
			requestID:    "999",
			expectedCode: http.StatusNotFound,
		},
		{
			name:         "invalid id (string IDs return not found)",
			setupUser:    nil,
			requestID:    "invalid",
			expectedCode: http.StatusNotFound, // With string IDs, any string is valid but won't be found
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanTable(t)

			// Setup test data
			if tt.setupUser != nil {
				db, _ := datastore.Get()
				_, err := db.GetDB().NewInsert().Model(tt.setupUser).Returning("*").Exec(context.Background())
				if err != nil {
					t.Fatal("Failed to insert test user:", err)
				}
			}

			// Make request
			r := chi.NewRouter()
			r.Use(withMeta(userMeta))
			r.Get("/users/{id}", handler.Get[TestUser]())

			req := httptest.NewRequest(http.MethodGet, "/users/"+tt.requestID, nil)
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("id", tt.requestID)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.expectedCode {
				t.Errorf("Expected status %d, got %d", tt.expectedCode, w.Code)
			}

			if tt.expectedCode == http.StatusOK {
				var result TestUser
				if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
					t.Fatal("Failed to decode response:", err)
				}

				if result.Name != tt.expectedName {
					t.Errorf("Expected name '%s', got '%s'", tt.expectedName, result.Name)
				}
			}
		})
	}
}

func TestHandler_Create(t *testing.T) {
	tests := []struct {
		name         string
		requestBody  interface{}
		setupUser    *TestUser
		expectedCode int
		checkName    string
	}{
		{
			name:         "valid user",
			requestBody:  TestUser{Name: "New User", Email: "new@example.com"},
			expectedCode: http.StatusCreated,
			checkName:    "New User",
		},
		{
			name:         "duplicate email",
			requestBody:  TestUser{Name: "User 2", Email: "duplicate@example.com"},
			setupUser:    &TestUser{Name: "User 1", Email: "duplicate@example.com"},
			expectedCode: http.StatusBadRequest,
		},
		{
			name:         "invalid json",
			requestBody:  "invalid json",
			expectedCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanTable(t)

			// Setup existing user if needed
			if tt.setupUser != nil {
				db, _ := datastore.Get()
				_, err := db.GetDB().NewInsert().Model(tt.setupUser).Exec(context.Background())
				if err != nil {
					t.Fatal("Failed to insert test user:", err)
				}
			}

			// Prepare request body
			var body []byte
			if str, ok := tt.requestBody.(string); ok {
				body = []byte(str)
			} else {
				body, _ = json.Marshal(tt.requestBody)
			}

			// Make request
			r := chi.NewRouter()
			r.Use(withMeta(userMeta))
			r.Post("/users", handler.Create[TestUser]())

			req := httptest.NewRequest(http.MethodPost, "/users", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != tt.expectedCode {
				t.Errorf("Expected status %d, got %d: %s", tt.expectedCode, w.Code, w.Body.String())
			}

			if tt.expectedCode == http.StatusCreated {
				var result TestUser
				if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
					t.Fatal("Failed to decode response:", err)
				}

				if result.ID == 0 {
					t.Error("Expected ID to be set")
				}

				if result.Name != tt.checkName {
					t.Errorf("Expected name '%s', got '%s'", tt.checkName, result.Name)
				}
			}
		})
	}
}

func TestHandler_Update(t *testing.T) {
	tests := []struct {
		name         string
		setupUser    *TestUser
		updateData   interface{}
		requestID    string
		expectedCode int
		expectedName string
	}{
		{
			name:         "valid update",
			setupUser:    &TestUser{Name: "Original", Email: "original@example.com"},
			updateData:   TestUser{ID: 1, Name: "Updated", Email: "updated@example.com"},
			requestID:    "1",
			expectedCode: http.StatusOK,
			expectedName: "Updated",
		},
		{
			name:         "not found",
			setupUser:    nil,
			updateData:   TestUser{ID: 999, Name: "Name", Email: "email@example.com"},
			requestID:    "999",
			expectedCode: http.StatusNotFound,
		},
		{
			name:         "invalid json",
			setupUser:    &TestUser{Name: "User", Email: "user@example.com"},
			updateData:   "invalid",
			requestID:    "1",
			expectedCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanTable(t)

			// Setup user
			if tt.setupUser != nil {
				db, _ := datastore.Get()
				_, err := db.GetDB().NewInsert().Model(tt.setupUser).Returning("*").Exec(context.Background())
				if err != nil {
					t.Fatal("Failed to insert test user:", err)
				}
			}

			// Prepare request body
			var body []byte
			if str, ok := tt.updateData.(string); ok {
				body = []byte(str)
			} else {
				body, _ = json.Marshal(tt.updateData)
			}

			// Make request
			r := chi.NewRouter()
			r.Use(withMeta(userMeta))
			r.Put("/users/{id}", handler.Update[TestUser]())

			req := httptest.NewRequest(http.MethodPut, "/users/"+tt.requestID, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("id", tt.requestID)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.expectedCode {
				t.Errorf("Expected status %d, got %d: %s", tt.expectedCode, w.Code, w.Body.String())
			}

			if tt.expectedCode == http.StatusOK {
				var result TestUser
				if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
					t.Fatal("Failed to decode response:", err)
				}

				if result.Name != tt.expectedName {
					t.Errorf("Expected name '%s', got '%s'", tt.expectedName, result.Name)
				}
			}
		})
	}
}

func TestHandler_Delete(t *testing.T) {
	tests := []struct {
		name         string
		setupUser    *TestUser
		requestID    string
		expectedCode int
	}{
		{
			name:         "valid delete",
			setupUser:    &TestUser{Name: "To Delete", Email: "delete@example.com"},
			requestID:    "1",
			expectedCode: http.StatusNoContent,
		},
		{
			name:         "not found",
			setupUser:    nil,
			requestID:    "999",
			expectedCode: http.StatusNotFound,
		},
		{
			name:         "invalid id (string IDs return not found)",
			setupUser:    nil,
			requestID:    "invalid",
			expectedCode: http.StatusNotFound, // With string IDs, any string is valid but won't be found
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanTable(t)

			// Setup user
			if tt.setupUser != nil {
				db, _ := datastore.Get()
				_, err := db.GetDB().NewInsert().Model(tt.setupUser).Returning("*").Exec(context.Background())
				if err != nil {
					t.Fatal("Failed to insert test user:", err)
				}
			}

			// Make request
			r := chi.NewRouter()
			r.Use(withMeta(userMeta))
			r.Delete("/users/{id}", handler.Delete[TestUser]())

			req := httptest.NewRequest(http.MethodDelete, "/users/"+tt.requestID, nil)
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("id", tt.requestID)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.expectedCode {
				t.Errorf("Expected status %d, got %d", tt.expectedCode, w.Code)
			}

			if tt.expectedCode == http.StatusNoContent {
				// Verify deletion
				db, _ := datastore.Get()
				var checkUser TestUser
				err := db.GetDB().NewSelect().Model(&checkUser).Where("id = ?", 1).Scan(context.Background())
				if err == nil {
					t.Error("Expected user to be deleted")
				}
			}
		})
	}
}

func TestHandler_ErrorTypes(t *testing.T) {
	// Verify error constants exist and are distinct
	errors := []error{
		apperrors.ErrNotFound,
		apperrors.ErrDuplicate,
		apperrors.ErrInvalidReference,
		apperrors.ErrUnavailable,
	}

	for i, err1 := range errors {
		for j, err2 := range errors {
			if i != j && err1 == err2 {
				t.Errorf("Errors at index %d and %d are the same", i, j)
			}
		}
	}
}

// TestHandler_ContextCancellation tests context cancellation handling
func TestHandler_ContextCancellation(t *testing.T) {
	cleanTable(t)

	// Setup test user
	db, _ := datastore.Get()
	user := &TestUser{Name: "Test User", Email: "test@example.com"}
	_, err := db.GetDB().NewInsert().Model(user).Returning("*").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to insert test user:", err)
	}

	tests := []struct {
		name    string
		handler http.HandlerFunc
		method  string
		path    string
		body    []byte
	}{
		{
			name:    "GetAll with canceled context",
			handler: handler.GetAll[TestUser](),
			method:  http.MethodGet,
			path:    "/users",
		},
		{
			name:    "Get with canceled context",
			handler: handler.Get[TestUser](),
			method:  http.MethodGet,
			path:    "/users/1",
		},
		{
			name:    "Create with canceled context",
			handler: handler.Create[TestUser](),
			method:  http.MethodPost,
			path:    "/users",
			body:    []byte(`{"name":"New User","email":"new@example.com"}`),
		},
		{
			name:    "Update with canceled context",
			handler: handler.Update[TestUser](),
			method:  http.MethodPut,
			path:    "/users/1",
			body:    []byte(`{"name":"Updated","email":"updated@example.com"}`),
		},
		{
			name:    "Delete with canceled context",
			handler: handler.Delete[TestUser](),
			method:  http.MethodDelete,
			path:    "/users/1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a canceled context
			ctx, cancel := context.WithCancel(context.Background())
			cancel() // Cancel immediately

			var req *http.Request
			if tt.body != nil {
				req = httptest.NewRequest(tt.method, tt.path, bytes.NewReader(tt.body))
			} else {
				req = httptest.NewRequest(tt.method, tt.path, nil)
			}

			// Add metadata to context
			ctx = context.WithValue(ctx, metadata.MetadataKey, userMeta)

			// Add route context for handlers that need ID parameter
			if tt.method != http.MethodGet || tt.path != testUsersPath {
				rctx := chi.NewRouteContext()
				rctx.URLParams.Add("id", "1")
				ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
			}

			req = req.WithContext(ctx)
			w := httptest.NewRecorder()

			tt.handler(w, req)

			// Context cancellation should not write any response
			if w.Code != 0 && w.Code != http.StatusOK {
				t.Errorf("Expected no response or 200 for canceled context, got %d", w.Code)
			}
		})
	}
}

// TestHandler_ContextTimeout tests context timeout handling
func TestHandler_ContextTimeout(t *testing.T) {
	cleanTable(t)

	// Setup test user
	db, _ := datastore.Get()
	user := &TestUser{Name: "Test User", Email: "test@example.com"}
	_, err := db.GetDB().NewInsert().Model(user).Returning("*").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to insert test user:", err)
	}

	tests := []struct {
		name    string
		handler http.HandlerFunc
		method  string
		path    string
		body    []byte
	}{
		{
			name:    "GetAll with timeout",
			handler: handler.GetAll[TestUser](),
			method:  http.MethodGet,
			path:    "/users",
		},
		{
			name:    "Get with timeout",
			handler: handler.Get[TestUser](),
			method:  http.MethodGet,
			path:    "/users/1",
		},
		{
			name:    "Create with timeout",
			handler: handler.Create[TestUser](),
			method:  http.MethodPost,
			path:    "/users",
			body:    []byte(`{"name":"New User","email":"new@example.com"}`),
		},
		{
			name:    "Update with timeout",
			handler: handler.Update[TestUser](),
			method:  http.MethodPut,
			path:    "/users/1",
			body:    []byte(`{"name":"Updated","email":"updated@example.com"}`),
		},
		{
			name:    "Delete with timeout",
			handler: handler.Delete[TestUser](),
			method:  http.MethodDelete,
			path:    "/users/1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a context that's already timed out
			ctx, cancel := context.WithTimeout(context.Background(), 0)
			defer cancel()
			time.Sleep(1 * time.Millisecond) // Ensure timeout has occurred

			var req *http.Request
			if tt.body != nil {
				req = httptest.NewRequest(tt.method, tt.path, bytes.NewReader(tt.body))
			} else {
				req = httptest.NewRequest(tt.method, tt.path, nil)
			}

			// Add metadata to context
			ctx = context.WithValue(ctx, metadata.MetadataKey, userMeta)

			// Add route context for handlers that need ID parameter
			if tt.method != http.MethodGet || tt.path != testUsersPath {
				rctx := chi.NewRouteContext()
				rctx.URLParams.Add("id", "1")
				ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
			}

			req = req.WithContext(ctx)
			w := httptest.NewRecorder()

			tt.handler(w, req)

			// Should return 504 Gateway Timeout
			if w.Code != http.StatusGatewayTimeout {
				t.Errorf("Expected status %d, got %d", http.StatusGatewayTimeout, w.Code)
			}
		})
	}
}

// TestHandler_GetAllWithRelations tests GetAll with relations in context
func TestHandler_GetAllWithRelations(t *testing.T) {
	cleanTable(t)

	// Setup test data
	db, _ := datastore.Get()
	user := &TestUser{Name: "Test User", Email: "test@example.com"}
	_, err := db.GetDB().NewInsert().Model(user).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to insert test user:", err)
	}

	// Create request with empty relations array - just testing that context extraction works
	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	ctx := context.WithValue(req.Context(), metadata.MetadataKey, userMeta)
	ctx = context.WithValue(ctx, "relations", []string{})
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.GetAll[TestUser]()(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var result []TestUser
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatal("Failed to decode response:", err)
	}

	if len(result) != 1 {
		t.Errorf("Expected 1 user, got %d", len(result))
	}
}

// TestHandler_GetWithRelations tests Get with relations in context
func TestHandler_GetWithRelations(t *testing.T) {
	cleanTable(t)

	// Setup test data
	db, _ := datastore.Get()
	user := &TestUser{Name: "Test User", Email: "test@example.com"}
	_, err := db.GetDB().NewInsert().Model(user).Returning("*").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to insert test user:", err)
	}

	// Create request with empty relations array - just testing that context extraction works
	req := httptest.NewRequest(http.MethodGet, "/users/1", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, metadata.MetadataKey, userMeta)
	ctx = context.WithValue(ctx, "relations", []string{})
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.Get[TestUser]()(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var result TestUser
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatal("Failed to decode response:", err)
	}

	if result.Name != "Test User" {
		t.Errorf("Expected name 'Test User', got '%s'", result.Name)
	}
}

// TestHandler_UpdateInvalidID tests Update with invalid ID parameter
func TestHandler_UpdateInvalidID(t *testing.T) {
	cleanTable(t)

	body := []byte(`{"name":"Updated","email":"updated@example.com"}`)
	req := httptest.NewRequest(http.MethodPut, "/users/invalid", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "invalid")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, metadata.MetadataKey, userMeta)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.Update[TestUser]()(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

// TestHandler_MissingMetadata tests handlers when metadata is not in context
func TestHandler_MissingMetadata(t *testing.T) {
	cleanTable(t)

	tests := []struct {
		name         string
		method       string
		path         string
		body         []byte
		handler      http.HandlerFunc
		expectedCode int
	}{
		{
			name:         "Get without metadata",
			method:       http.MethodGet,
			path:         "/users/1",
			handler:      handler.Get[TestUser](),
			expectedCode: http.StatusInternalServerError,
		},
		{
			name:         "Update without metadata",
			method:       http.MethodPut,
			path:         "/users/1",
			body:         []byte(`{"name":"Test","email":"test@example.com"}`),
			handler:      handler.Update[TestUser](),
			expectedCode: http.StatusInternalServerError,
		},
		{
			name:         "Delete without metadata",
			method:       http.MethodDelete,
			path:         "/users/1",
			handler:      handler.Delete[TestUser](),
			expectedCode: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req *http.Request
			if tt.body != nil {
				req = httptest.NewRequest(tt.method, tt.path, bytes.NewReader(tt.body))
				req.Header.Set("Content-Type", "application/json")
			} else {
				req = httptest.NewRequest(tt.method, tt.path, nil)
			}

			// Add route context with ID but NO metadata
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("id", "1")
			ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
			req = req.WithContext(ctx)

			w := httptest.NewRecorder()
			tt.handler(w, req)

			if w.Code != tt.expectedCode {
				t.Errorf("Expected status %d, got %d: %s", tt.expectedCode, w.Code, w.Body.String())
			}
		})
	}
}

// TestHandler_GetAll_QueryParams tests query parameter parsing for GetAll
func TestHandler_GetAll_QueryParams(t *testing.T) {
	cleanTable(t)

	// Insert test data
	db, _ := datastore.Get()
	users := []TestUser{
		{Name: "Alice", Email: "alice@example.com"},
		{Name: "Bob", Email: "bob@example.com"},
		{Name: "Charlie", Email: "charlie@example.com"},
	}
	for _, user := range users {
		_, err := db.GetDB().NewInsert().Model(&user).Exec(context.Background())
		if err != nil {
			t.Fatal("Failed to insert test user:", err)
		}
	}

	// Update metadata to allow filtering and sorting
	queryMeta := &metadata.TypeMetadata{
		TypeID:           "test_user_query_id",
		TypeName:         "TestUser",
		TableName:        "users",
		URLParamUUID:     "id",
		ModelType:        reflect.TypeOf(TestUser{}),
		FilterableFields: []string{"Name", "Email"},
		SortableFields:   []string{"Name", "Email"},
		DefaultLimit:     10,
		MaxLimit:         100,
	}

	tests := []struct {
		name          string
		queryString   string
		expectedCount int
		checkHeaders  map[string]string
	}{
		{
			name:          "no query params",
			queryString:   "",
			expectedCount: 3,
		},
		{
			name:          "filter by name",
			queryString:   "filter[Name]=Alice",
			expectedCount: 1,
		},
		{
			name:          "limit",
			queryString:   "limit=2",
			expectedCount: 2,
			checkHeaders:  map[string]string{"X-Limit": "2"},
		},
		{
			name:          "offset",
			queryString:   "offset=1&sort=Name",
			expectedCount: 2,
			checkHeaders:  map[string]string{"X-Offset": "1"},
		},
		{
			name:          "count",
			queryString:   "count=true&limit=1",
			expectedCount: 1,
			checkHeaders:  map[string]string{"X-Total-Count": "3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := chi.NewRouter()
			r.Use(withMeta(queryMeta))
			r.Get("/users", handler.GetAll[TestUser]())

			url := "/users"
			if tt.queryString != "" {
				url += "?" + tt.queryString
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
				return
			}

			var results []TestUser
			if err := json.Unmarshal(w.Body.Bytes(), &results); err != nil {
				t.Fatal("Failed to unmarshal response:", err)
			}

			if len(results) != tt.expectedCount {
				t.Errorf("Expected %d results, got %d", tt.expectedCount, len(results))
			}

			// Check headers if specified
			for header, expected := range tt.checkHeaders {
				actual := w.Header().Get(header)
				if actual != expected {
					t.Errorf("Expected header %s=%s, got %s", header, expected, actual)
				}
			}
		})
	}
}

// TestHandler_GetAll_FilterOperators tests filter operator parsing (filter[field][op]=value)
func TestHandler_GetAll_FilterOperators(t *testing.T) {
	cleanTable(t)

	// Insert test data with different names for testing operators
	db, _ := datastore.Get()
	users := []TestUser{
		{Name: "Xander", Email: "xander@example.com"},
		{Name: "Yara", Email: "yara@example.com"},
		{Name: "Zoe", Email: "zoe@example.com"},
	}
	for _, user := range users {
		_, err := db.GetDB().NewInsert().Model(&user).Exec(context.Background())
		if err != nil {
			t.Fatal("Failed to insert test user:", err)
		}
	}

	// Update metadata to allow filtering
	queryMeta := &metadata.TypeMetadata{
		TypeID:           "test_user_filter_op_id",
		TypeName:         "TestUser",
		TableName:        "users",
		URLParamUUID:     "id",
		ModelType:        reflect.TypeOf(TestUser{}),
		FilterableFields: []string{"Name", "Email", "ID"},
		SortableFields:   []string{"Name", "Email"},
		DefaultLimit:     10,
		MaxLimit:         100,
	}

	tests := []struct {
		name          string
		queryString   string
		expectedCount int
		checkNames    []string
	}{
		{
			name:          "filter with eq operator",
			queryString:   "filter[Name][eq]=Xander",
			expectedCount: 1,
			checkNames:    []string{"Xander"},
		},
		{
			name:          "filter with neq operator",
			queryString:   "filter[Name][neq]=Xander",
			expectedCount: 2,
			checkNames:    []string{"Yara", "Zoe"},
		},
		{
			name:          "filter with gt operator on ID",
			queryString:   "filter[ID][gt]=1",
			expectedCount: 2,
		},
		{
			name:          "filter with gte operator on ID",
			queryString:   "filter[ID][gte]=2",
			expectedCount: 2,
		},
		{
			name:          "filter with lt operator on ID",
			queryString:   "filter[ID][lt]=3",
			expectedCount: 2,
		},
		{
			name:          "filter with lte operator on ID",
			queryString:   "filter[ID][lte]=2",
			expectedCount: 2,
		},
		{
			name:          "filter with like operator",
			queryString:   "filter[Name][like]=%25ander",
			expectedCount: 1,
			checkNames:    []string{"Xander"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := chi.NewRouter()
			r.Use(withMeta(queryMeta))
			r.Get(testUsersPath, handler.GetAll[TestUser]())

			url := testUsersPath + "?" + tt.queryString
			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
				return
			}

			var results []TestUser
			if err := json.Unmarshal(w.Body.Bytes(), &results); err != nil {
				t.Fatal("Failed to unmarshal response:", err)
			}

			if len(results) != tt.expectedCount {
				t.Errorf("Expected %d results, got %d", tt.expectedCount, len(results))
			}

			// Check specific names if provided
			if len(tt.checkNames) > 0 {
				names := make(map[string]bool)
				for _, r := range results {
					names[r.Name] = true
				}
				for _, expected := range tt.checkNames {
					if !names[expected] {
						t.Errorf("Expected result to contain %s", expected)
					}
				}
			}
		})
	}
}

// TestValidatedItem is a test model for validation tests
type TestValidatedItem struct {
	bun.BaseModel `bun:"table:test_validated_items"`
	ID            int    `bun:"id,pk,autoincrement" json:"id"`
	Name          string `bun:"name,notnull" json:"name"`
	Status        string `bun:"status,notnull" json:"status"`
	Priority      int    `bun:"priority,notnull" json:"priority"`
}

// TestHandler_ValidationError_Create tests that Create returns 400 with validation message
func TestHandler_ValidationError_Create(t *testing.T) {
	// Create table for validated items
	db, _ := datastore.Get()
	_, err := db.GetDB().NewCreateTable().Model((*TestValidatedItem)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test_validated_items table:", err)
	}
	defer func() {
		db.GetDB().NewDropTable().Model((*TestValidatedItem)(nil)).IfExists().Exec(context.Background())
	}()

	// Clean table
	db.GetDB().NewDelete().Model((*TestValidatedItem)(nil)).Where("1=1").Exec(context.Background())

	// Create validator that rejects priority > 5
	var validator metadata.ValidatorFunc[TestValidatedItem] = func(vc metadata.ValidationContext[TestValidatedItem]) error {
		if vc.New.Priority > 5 {
			return apperrors.NewValidationError("priority must be between 1 and 5")
		}
		return nil
	}

	// Create metadata with validator
	validatedMeta := &metadata.TypeMetadata{
		TypeID:       "test_validated_item_id",
		TypeName:     "TestValidatedItem",
		TableName:    "test_validated_items",
		URLParamUUID: "id",
		ModelType:    reflect.TypeOf(TestValidatedItem{}),
		Validator:    validator,
	}

	// Make request with invalid priority
	body := []byte(`{"name":"Test","status":"active","priority":10}`)
	req := httptest.NewRequest(http.MethodPost, "/items", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), metadata.MetadataKey, validatedMeta)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.Create[TestValidatedItem]()(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}

	// Check that the response contains our validation message
	if !bytes.Contains(w.Body.Bytes(), []byte("priority must be between 1 and 5")) {
		t.Errorf("Expected body to contain validation message, got: %s", w.Body.String())
	}
}

// TestHandler_ValidationError_Update tests that Update returns 400 with validation message
func TestHandler_ValidationError_Update(t *testing.T) {
	// Create table for validated items
	db, _ := datastore.Get()
	_, err := db.GetDB().NewCreateTable().Model((*TestValidatedItem)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test_validated_items table:", err)
	}
	defer func() {
		db.GetDB().NewDropTable().Model((*TestValidatedItem)(nil)).IfExists().Exec(context.Background())
	}()

	// Clean and insert test item
	db.GetDB().NewDelete().Model((*TestValidatedItem)(nil)).Where("1=1").Exec(context.Background())
	item := &TestValidatedItem{Name: "Test", Status: "pending", Priority: 3}
	_, err = db.GetDB().NewInsert().Model(item).Returning("*").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to insert test item:", err)
	}

	// Create validator that rejects status transitions from pending to completed
	var validator metadata.ValidatorFunc[TestValidatedItem] = func(vc metadata.ValidationContext[TestValidatedItem]) error {
		if vc.Operation == metadata.OpUpdate {
			if vc.Old.Status == "pending" && vc.New.Status == "completed" {
				return apperrors.NewValidationError("cannot skip from pending to completed")
			}
		}
		return nil
	}

	// Create metadata with validator
	validatedMeta := &metadata.TypeMetadata{
		TypeID:       "test_validated_item_update_id",
		TypeName:     "TestValidatedItem",
		TableName:    "test_validated_items",
		URLParamUUID: "id",
		ModelType:    reflect.TypeOf(TestValidatedItem{}),
		Validator:    validator,
	}

	// Make request with invalid transition
	body := []byte(`{"id":1,"name":"Test","status":"completed","priority":3}`)
	req := httptest.NewRequest(http.MethodPut, "/items/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, metadata.MetadataKey, validatedMeta)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.Update[TestValidatedItem]()(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}

	// Check that the response contains our validation message
	if !bytes.Contains(w.Body.Bytes(), []byte("cannot skip from pending to completed")) {
		t.Errorf("Expected body to contain validation message, got: %s", w.Body.String())
	}
}

// TestHandler_ValidationError_Delete tests that Delete returns 400 with validation message
func TestHandler_ValidationError_Delete(t *testing.T) {
	// Create table for validated items
	db, _ := datastore.Get()
	_, err := db.GetDB().NewCreateTable().Model((*TestValidatedItem)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test_validated_items table:", err)
	}
	defer func() {
		db.GetDB().NewDropTable().Model((*TestValidatedItem)(nil)).IfExists().Exec(context.Background())
	}()

	// Clean and insert test item
	db.GetDB().NewDelete().Model((*TestValidatedItem)(nil)).Where("1=1").Exec(context.Background())
	item := &TestValidatedItem{Name: "Test", Status: "completed", Priority: 3}
	_, err = db.GetDB().NewInsert().Model(item).Returning("*").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to insert test item:", err)
	}

	// Create validator that rejects deleting completed items
	var validator metadata.ValidatorFunc[TestValidatedItem] = func(vc metadata.ValidationContext[TestValidatedItem]) error {
		if vc.Operation == metadata.OpDelete {
			if vc.Old.Status == "completed" {
				return apperrors.NewValidationError("cannot delete completed items")
			}
		}
		return nil
	}

	// Create metadata with validator
	validatedMeta := &metadata.TypeMetadata{
		TypeID:       "test_validated_item_delete_id",
		TypeName:     "TestValidatedItem",
		TableName:    "test_validated_items",
		URLParamUUID: "id",
		ModelType:    reflect.TypeOf(TestValidatedItem{}),
		Validator:    validator,
	}

	// Make delete request
	req := httptest.NewRequest(http.MethodDelete, "/items/1", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, metadata.MetadataKey, validatedMeta)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.Delete[TestValidatedItem]()(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}

	// Check that the response contains our validation message
	if !bytes.Contains(w.Body.Bytes(), []byte("cannot delete completed items")) {
		t.Errorf("Expected body to contain validation message, got: %s", w.Body.String())
	}
}

// ============================================================================
// UUID Primary Key Tests
// ============================================================================

// TestUUIDBlog is a test model with UUID primary key
type TestUUIDBlog struct {
	bun.BaseModel `bun:"table:uuid_blogs"`
	ID            uuid.UUID `bun:"id,pk,type:uuid" json:"id"`
	Name          string    `bun:"name,notnull" json:"name"`
	CreatedAt     time.Time `bun:"created_at,notnull,default:current_timestamp" json:"created_at,omitempty"`
}

// BeforeAppendModel generates UUID for new blogs
func (b *TestUUIDBlog) BeforeAppendModel(_ context.Context, query bun.Query) error {
	if _, ok := query.(*bun.InsertQuery); ok {
		if b.ID == uuid.Nil {
			b.ID = uuid.New()
		}
	}
	return nil
}

// TestUUIDPost is a child model with UUID primary key and UUID foreign key
type TestUUIDPost struct {
	bun.BaseModel `bun:"table:uuid_posts"`
	ID            uuid.UUID     `bun:"id,pk,type:uuid" json:"id"`
	BlogID        uuid.UUID     `bun:"blog_id,notnull,type:uuid" json:"blog_id"`
	Blog          *TestUUIDBlog `bun:"rel:belongs-to,join:blog_id=id" json:"-"`
	Title         string        `bun:"title,notnull" json:"title"`
	CreatedAt     time.Time     `bun:"created_at,notnull,default:current_timestamp" json:"created_at,omitempty"`
}

// BeforeAppendModel generates UUID for new posts
func (p *TestUUIDPost) BeforeAppendModel(_ context.Context, query bun.Query) error {
	if _, ok := query.(*bun.InsertQuery); ok {
		if p.ID == uuid.Nil {
			p.ID = uuid.New()
		}
	}
	return nil
}

// TestHandler_UUID_CRUD tests full CRUD operations with UUID primary keys
func TestHandler_UUID_CRUD(t *testing.T) {
	// Create UUID tables
	db, _ := datastore.Get()
	_, err := db.GetDB().NewCreateTable().Model((*TestUUIDBlog)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create uuid_blogs table:", err)
	}
	defer db.GetDB().NewDropTable().Model((*TestUUIDBlog)(nil)).IfExists().Exec(context.Background())

	uuidBlogMeta := &metadata.TypeMetadata{
		TypeID:       "test_uuid_blog_id",
		TypeName:     "TestUUIDBlog",
		TableName:    "uuid_blogs",
		URLParamUUID: "blogId",
		ModelType:    reflect.TypeOf(TestUUIDBlog{}),
	}

	// Test Create
	t.Run("Create", func(t *testing.T) {
		body := []byte(`{"name":"Test UUID Blog"}`)
		req := httptest.NewRequest(http.MethodPost, "/blogs", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		ctx := context.WithValue(req.Context(), metadata.MetadataKey, uuidBlogMeta)
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		handler.Create[TestUUIDBlog]()(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected status %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
		}

		var created TestUUIDBlog
		if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		if created.ID == uuid.Nil {
			t.Error("Expected UUID to be generated, got nil UUID")
		}
		if created.Name != "Test UUID Blog" {
			t.Errorf("Expected name 'Test UUID Blog', got '%s'", created.Name)
		}

		// Test Get with UUID
		t.Run("Get", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/blogs/"+created.ID.String(), nil)

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("blogId", created.ID.String())
			ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
			ctx = context.WithValue(ctx, metadata.MetadataKey, uuidBlogMeta)
			req = req.WithContext(ctx)

			w := httptest.NewRecorder()
			handler.Get[TestUUIDBlog]()(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("Expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
			}

			var got TestUUIDBlog
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("Failed to unmarshal response: %v", err)
			}

			if got.ID != created.ID {
				t.Errorf("Expected ID %s, got %s", created.ID, got.ID)
			}
		})

		// Test Update with UUID
		t.Run("Update", func(t *testing.T) {
			body := []byte(`{"name":"Updated UUID Blog"}`)
			req := httptest.NewRequest(http.MethodPut, "/blogs/"+created.ID.String(), bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("blogId", created.ID.String())
			ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
			ctx = context.WithValue(ctx, metadata.MetadataKey, uuidBlogMeta)
			req = req.WithContext(ctx)

			w := httptest.NewRecorder()
			handler.Update[TestUUIDBlog]()(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("Expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
			}

			var updated TestUUIDBlog
			if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
				t.Fatalf("Failed to unmarshal response: %v", err)
			}

			if updated.Name != "Updated UUID Blog" {
				t.Errorf("Expected name 'Updated UUID Blog', got '%s'", updated.Name)
			}
		})

		// Test Delete with UUID
		t.Run("Delete", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, "/blogs/"+created.ID.String(), nil)

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("blogId", created.ID.String())
			ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
			ctx = context.WithValue(ctx, metadata.MetadataKey, uuidBlogMeta)
			req = req.WithContext(ctx)

			w := httptest.NewRecorder()
			handler.Delete[TestUUIDBlog]()(w, req)

			if w.Code != http.StatusNoContent {
				t.Fatalf("Expected status %d, got %d: %s", http.StatusNoContent, w.Code, w.Body.String())
			}
		})

		// Verify deletion
		t.Run("VerifyDeleted", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/blogs/"+created.ID.String(), nil)

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("blogId", created.ID.String())
			ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
			ctx = context.WithValue(ctx, metadata.MetadataKey, uuidBlogMeta)
			req = req.WithContext(ctx)

			w := httptest.NewRecorder()
			handler.Get[TestUUIDBlog]()(w, req)

			if w.Code != http.StatusNotFound {
				t.Errorf("Expected status %d, got %d", http.StatusNotFound, w.Code)
			}
		})
	})
}

// TestHandler_UUID_InvalidFormat tests that invalid UUID format returns 400
func TestHandler_UUID_InvalidFormat(t *testing.T) {
	// Create UUID table
	db, _ := datastore.Get()
	_, err := db.GetDB().NewCreateTable().Model((*TestUUIDBlog)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create uuid_blogs table:", err)
	}
	defer db.GetDB().NewDropTable().Model((*TestUUIDBlog)(nil)).IfExists().Exec(context.Background())

	uuidBlogMeta := &metadata.TypeMetadata{
		TypeID:       "test_uuid_blog_invalid_id",
		TypeName:     "TestUUIDBlog",
		TableName:    "uuid_blogs",
		URLParamUUID: "blogId",
		ModelType:    reflect.TypeOf(TestUUIDBlog{}),
	}

	// Test Update with invalid UUID format
	t.Run("UpdateInvalidUUID", func(t *testing.T) {
		body := []byte(`{"name":"Test"}`)
		req := httptest.NewRequest(http.MethodPut, "/blogs/not-a-valid-uuid", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("blogId", "not-a-valid-uuid")
		ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
		ctx = context.WithValue(ctx, metadata.MetadataKey, uuidBlogMeta)
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		handler.Update[TestUUIDBlog]()(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status %d for invalid UUID, got %d: %s", http.StatusBadRequest, w.Code, w.Body.String())
		}
	})
}

// TestHandler_UUID_StringPK tests models with string primary keys
func TestHandler_UUID_StringPK(t *testing.T) {
	// TestStringPKModel uses string as primary key
	type TestStringPKModel struct {
		bun.BaseModel `bun:"table:string_pk_models"`
		ID            string    `bun:"id,pk" json:"id"`
		Name          string    `bun:"name,notnull" json:"name"`
		CreatedAt     time.Time `bun:"created_at,notnull,default:current_timestamp" json:"created_at,omitempty"`
	}

	// Create table
	db, _ := datastore.Get()
	_, err := db.GetDB().NewCreateTable().Model((*TestStringPKModel)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create string_pk_models table:", err)
	}
	defer db.GetDB().NewDropTable().Model((*TestStringPKModel)(nil)).IfExists().Exec(context.Background())

	stringPKMeta := &metadata.TypeMetadata{
		TypeID:       "test_string_pk_id",
		TypeName:     "TestStringPKModel",
		TableName:    "string_pk_models",
		URLParamUUID: "id",
		ModelType:    reflect.TypeOf(TestStringPKModel{}),
	}

	// Create with string ID
	t.Run("CreateWithStringID", func(t *testing.T) {
		body := []byte(`{"id":"custom-string-id","name":"Test String PK"}`)
		req := httptest.NewRequest(http.MethodPost, "/models", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		ctx := context.WithValue(req.Context(), metadata.MetadataKey, stringPKMeta)
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		handler.Create[TestStringPKModel]()(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected status %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
		}

		var created TestStringPKModel
		if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		if created.ID != "custom-string-id" {
			t.Errorf("Expected ID 'custom-string-id', got '%s'", created.ID)
		}
	})

	// Get with string ID
	t.Run("GetWithStringID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/models/custom-string-id", nil)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "custom-string-id")
		ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
		ctx = context.WithValue(ctx, metadata.MetadataKey, stringPKMeta)
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		handler.Get[TestStringPKModel]()(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
		}
	})

	// Update with string ID
	t.Run("UpdateWithStringID", func(t *testing.T) {
		body := []byte(`{"name":"Updated String PK"}`)
		req := httptest.NewRequest(http.MethodPut, "/models/custom-string-id", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "custom-string-id")
		ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
		ctx = context.WithValue(ctx, metadata.MetadataKey, stringPKMeta)
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		handler.Update[TestStringPKModel]()(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
		}

		var updated TestStringPKModel
		if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		if updated.ID != "custom-string-id" {
			t.Errorf("Expected ID 'custom-string-id', got '%s'", updated.ID)
		}
		if updated.Name != "Updated String PK" {
			t.Errorf("Expected name 'Updated String PK', got '%s'", updated.Name)
		}
	})

	// Delete with string ID
	t.Run("DeleteWithStringID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/models/custom-string-id", nil)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "custom-string-id")
		ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
		ctx = context.WithValue(ctx, metadata.MetadataKey, stringPKMeta)
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		handler.Delete[TestStringPKModel]()(w, req)

		if w.Code != http.StatusNoContent {
			t.Errorf("Expected status %d, got %d: %s", http.StatusNoContent, w.Code, w.Body.String())
		}
	})
}
