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
	"github.com/sjgoldie/go-restgen/datastore"
	apperrors "github.com/sjgoldie/go-restgen/errors"
	"github.com/sjgoldie/go-restgen/handler"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/uptrace/bun"
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
			name:         "invalid id",
			setupUser:    nil,
			requestID:    "invalid",
			expectedCode: http.StatusBadRequest,
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
			name:         "invalid id",
			setupUser:    nil,
			requestID:    "invalid",
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
			if tt.method != http.MethodGet || tt.path != "/users" {
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
			if tt.method != http.MethodGet || tt.path != "/users" {
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
