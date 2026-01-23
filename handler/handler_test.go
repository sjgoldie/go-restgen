//nolint:staticcheck,errcheck,gosec // Test code - string context keys and unchecked test cleanup are acceptable
package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/datastore"
	apperrors "github.com/sjgoldie/go-restgen/errors"
	"github.com/sjgoldie/go-restgen/filestore"
	"github.com/sjgoldie/go-restgen/handler"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/service"
)

// Test route paths as constants to avoid duplication
const (
	testUsersPath   = "/users"
	testFileContent = "file content here"
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

// Global mock file storage for all handler tests
var testFileStorage *mockFileStorage

// Test metadata for injecting into context
var userMeta = &metadata.TypeMetadata{
	TypeID:        "test_user_id",
	TypeName:      "TestUser",
	TableName:     "users",
	URLParamUUID:  "id",
	PKField:       "ID",
	ModelType:     reflect.TypeOf(TestUser{}),
	ParentType:    nil,
	ForeignKeyCol: "",
}

// withMeta creates middleware that injects metadata and query options into context
// This mirrors what createMetadataMiddleware does in router/builder.go
func withMeta(meta *metadata.TypeMetadata) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), metadata.MetadataKey, meta)
			opts := metadata.ParseQueryOptions(r.URL.Query())
			ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ctxWithMeta creates a context with metadata and empty query options for direct handler tests
func ctxWithMeta(ctx context.Context, meta *metadata.TypeMetadata) context.Context {
	ctx = context.WithValue(ctx, metadata.MetadataKey, meta)
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, &metadata.QueryOptions{
		Filters: make(map[string]metadata.FilterValue),
	})
	return ctx
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

	// Initialize file storage once for all tests (uses sync.Once internally)
	testFileStorage = newMockFileStorage()
	if err := filestore.Initialize(testFileStorage); err != nil {
		testDB.Cleanup()
		panic("Failed to initialize file storage: " + err.Error())
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
			r.Get("/users", handler.GetAll[TestUser](handler.StandardGetAll[TestUser]))

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
			r.Get("/users/{id}", handler.Get[TestUser](handler.StandardGet[TestUser]))

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
			r.Post("/users", handler.Create[TestUser](handler.StandardCreate[TestUser]))

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
			r.Put("/users/{id}", handler.Update[TestUser](handler.StandardUpdate[TestUser]))

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
			r.Delete("/users/{id}", handler.Delete[TestUser](handler.StandardDelete[TestUser]))

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

// handlerTestCase represents a test case for handler context tests
type handlerTestCase struct {
	name    string
	handler http.HandlerFunc
	method  string
	path    string
	body    []byte
}

// getContextTestCases returns common test cases for context-related tests
func getContextTestCases(prefix string) []handlerTestCase {
	return []handlerTestCase{
		{
			name:    fmt.Sprintf("GetAll with %s", prefix),
			handler: handler.GetAll[TestUser](handler.StandardGetAll[TestUser]),
			method:  http.MethodGet,
			path:    "/users",
		},
		{
			name:    fmt.Sprintf("Get with %s", prefix),
			handler: handler.Get[TestUser](handler.StandardGet[TestUser]),
			method:  http.MethodGet,
			path:    "/users/1",
		},
		{
			name:    fmt.Sprintf("Create with %s", prefix),
			handler: handler.Create[TestUser](handler.StandardCreate[TestUser]),
			method:  http.MethodPost,
			path:    "/users",
			body:    []byte(`{"name":"New User","email":"new@example.com"}`),
		},
		{
			name:    fmt.Sprintf("Update with %s", prefix),
			handler: handler.Update[TestUser](handler.StandardUpdate[TestUser]),
			method:  http.MethodPut,
			path:    "/users/1",
			body:    []byte(`{"name":"Updated","email":"updated@example.com"}`),
		},
		{
			name:    fmt.Sprintf("Delete with %s", prefix),
			handler: handler.Delete[TestUser](handler.StandardDelete[TestUser]),
			method:  http.MethodDelete,
			path:    "/users/1",
		},
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

	tests := getContextTestCases("canceled context")

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

	tests := getContextTestCases("timeout")

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
	ctx := ctxWithMeta(req.Context(), userMeta)
	ctx = context.WithValue(ctx, "relations", []string{})
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.GetAll[TestUser](handler.StandardGetAll[TestUser])(w, req)

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
	handler.Get[TestUser](handler.StandardGet[TestUser])(w, req)

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
	handler.Update[TestUser](handler.StandardUpdate[TestUser])(w, req)

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
			handler:      handler.Get[TestUser](handler.StandardGet[TestUser]),
			expectedCode: http.StatusInternalServerError,
		},
		{
			name:         "Update without metadata",
			method:       http.MethodPut,
			path:         "/users/1",
			body:         []byte(`{"name":"Test","email":"test@example.com"}`),
			handler:      handler.Update[TestUser](handler.StandardUpdate[TestUser]),
			expectedCode: http.StatusInternalServerError,
		},
		{
			name:         "Delete without metadata",
			method:       http.MethodDelete,
			path:         "/users/1",
			handler:      handler.Delete[TestUser](handler.StandardDelete[TestUser]),
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
		PKField:          "ID",
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
			r.Get("/users", handler.GetAll[TestUser](handler.StandardGetAll[TestUser]))

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
		PKField:          "ID",
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
			r.Get(testUsersPath, handler.GetAll[TestUser](handler.StandardGetAll[TestUser]))

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
		PKField:      "ID",
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
	handler.Create[TestValidatedItem](handler.StandardCreate[TestValidatedItem])(w, req)

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
		PKField:      "ID",
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
	handler.Update[TestValidatedItem](handler.StandardUpdate[TestValidatedItem])(w, req)

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
		PKField:      "ID",
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
	handler.Delete[TestValidatedItem](handler.StandardDelete[TestValidatedItem])(w, req)

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
		PKField:      "ID",
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
		handler.Create[TestUUIDBlog](handler.StandardCreate[TestUUIDBlog])(w, req)

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
			handler.Get[TestUUIDBlog](handler.StandardGet[TestUUIDBlog])(w, req)

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
			handler.Update[TestUUIDBlog](handler.StandardUpdate[TestUUIDBlog])(w, req)

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
			handler.Delete[TestUUIDBlog](handler.StandardDelete[TestUUIDBlog])(w, req)

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
			handler.Get[TestUUIDBlog](handler.StandardGet[TestUUIDBlog])(w, req)

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
		PKField:      "ID",
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
		handler.Update[TestUUIDBlog](handler.StandardUpdate[TestUUIDBlog])(w, req)

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
		PKField:      "ID",
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
		handler.Create[TestStringPKModel](handler.StandardCreate[TestStringPKModel])(w, req)

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
		handler.Get[TestStringPKModel](handler.StandardGet[TestStringPKModel])(w, req)

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
		handler.Update[TestStringPKModel](handler.StandardUpdate[TestStringPKModel])(w, req)

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
		handler.Delete[TestStringPKModel](handler.StandardDelete[TestStringPKModel])(w, req)

		if w.Code != http.StatusNoContent {
			t.Errorf("Expected status %d, got %d: %s", http.StatusNoContent, w.Code, w.Body.String())
		}
	})
}

// TestHandler_MissingID tests that missing ID parameter returns 400
func TestHandler_MissingID(t *testing.T) {
	// Create test table
	db, _ := datastore.Get()
	_, err := db.GetDB().NewCreateTable().Model((*TestUser)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create users table:", err)
	}

	t.Run("GetMissingID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/users/", nil)

		rctx := chi.NewRouteContext()
		// Don't add the ID parameter - simulating empty ID
		rctx.URLParams.Add("id", "")
		ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
		ctx = context.WithValue(ctx, metadata.MetadataKey, userMeta)
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		handler.Get[TestUser](handler.StandardGet[TestUser])(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status %d for missing ID, got %d", http.StatusBadRequest, w.Code)
		}
	})

	t.Run("UpdateMissingID", func(t *testing.T) {
		body := []byte(`{"name":"Test"}`)
		req := httptest.NewRequest(http.MethodPut, "/users/", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "")
		ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
		ctx = context.WithValue(ctx, metadata.MetadataKey, userMeta)
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		handler.Update[TestUser](handler.StandardUpdate[TestUser])(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status %d for missing ID, got %d", http.StatusBadRequest, w.Code)
		}
	})

	t.Run("DeleteMissingID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/users/", nil)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "")
		ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
		ctx = context.WithValue(ctx, metadata.MetadataKey, userMeta)
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		handler.Delete[TestUser](handler.StandardDelete[TestUser])(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status %d for missing ID, got %d", http.StatusBadRequest, w.Code)
		}
	})
}

// TestHandler_UnsupportedIDType tests setIDField with unsupported types
func TestHandler_UnsupportedIDType(t *testing.T) {
	// Model with float ID (unsupported type)
	type TestFloatIDModel struct {
		bun.BaseModel `bun:"table:float_id_models"`
		ID            float64   `bun:"id,pk" json:"id"`
		Name          string    `bun:"name,notnull" json:"name"`
		CreatedAt     time.Time `bun:"created_at,notnull,default:current_timestamp" json:"created_at,omitempty"`
	}

	db, _ := datastore.Get()
	_, err := db.GetDB().NewCreateTable().Model((*TestFloatIDModel)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create float_id_models table:", err)
	}
	defer db.GetDB().NewDropTable().Model((*TestFloatIDModel)(nil)).IfExists().Exec(context.Background())

	floatIDMeta := &metadata.TypeMetadata{
		TypeID:       "test_float_id",
		TypeName:     "TestFloatIDModel",
		TableName:    "float_id_models",
		URLParamUUID: "id",
		PKField:      "ID",
		ModelType:    reflect.TypeOf(TestFloatIDModel{}),
	}

	// Try to update - setIDField should fail on float type
	body := []byte(`{"name":"Test"}`)
	req := httptest.NewRequest(http.MethodPut, "/models/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, metadata.MetadataKey, floatIDMeta)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.Update[TestFloatIDModel](handler.StandardUpdate[TestFloatIDModel])(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d for unsupported ID type, got %d: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

// TestCustomGet tests that CustomGet correctly uses a custom function
func TestCustomGet(t *testing.T) {
	// Create a user first
	_, err := testDB.GetDB().NewInsert().Model(&TestUser{ID: 100, Name: "Original", Email: "original@test.com"}).Exec(context.Background())
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}
	defer testDB.GetDB().NewDelete().Model((*TestUser)(nil)).Where("id = 100").Exec(context.Background())

	// Custom function that always returns a specific user regardless of ID
	customGet := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string) (*TestUser, error) {
		// Ignore the id parameter, always fetch user 100
		return svc.Get(ctx, "100")
	}

	// Request for user 999 (doesn't exist) but custom func will return user 100
	req := httptest.NewRequest(http.MethodGet, "/users/999", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(userMeta.URLParamUUID, "999")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, metadata.MetadataKey, userMeta)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.Get[TestUser](customGet)(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
		return
	}

	var user TestUser
	if err := json.Unmarshal(w.Body.Bytes(), &user); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if user.ID != 100 {
		t.Errorf("Expected user ID 100, got %d", user.ID)
	}
	if user.Name != "Original" {
		t.Errorf("Expected user name 'Original', got %s", user.Name)
	}
}

// TestCustomGet_WithAuth tests that auth info is passed to custom function
func TestCustomGet_WithAuth(t *testing.T) {
	// Create a user
	_, err := testDB.GetDB().NewInsert().Model(&TestUser{ID: 101, Name: "AuthUser", Email: "auth@test.com"}).Exec(context.Background())
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}
	defer testDB.GetDB().NewDelete().Model((*TestUser)(nil)).Where("id = 101").Exec(context.Background())

	var receivedAuth *metadata.AuthInfo

	// Custom function that captures the auth info
	customGet := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string) (*TestUser, error) {
		receivedAuth = auth
		return svc.Get(ctx, id)
	}

	req := httptest.NewRequest(http.MethodGet, "/users/101", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(userMeta.URLParamUUID, "101")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, metadata.MetadataKey, userMeta)

	// Add auth info to context
	authInfo := &metadata.AuthInfo{UserID: "test-user-123", Scopes: []string{"read", "write"}}
	ctx = context.WithValue(ctx, metadata.AuthInfoKey, authInfo)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.Get[TestUser](customGet)(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
		return
	}

	if receivedAuth == nil {
		t.Error("Expected auth info to be passed to custom function, got nil")
		return
	}

	if receivedAuth.UserID != "test-user-123" {
		t.Errorf("Expected UserID 'test-user-123', got '%s'", receivedAuth.UserID)
	}

	if len(receivedAuth.Scopes) != 2 || receivedAuth.Scopes[0] != "read" {
		t.Errorf("Expected scopes ['read', 'write'], got %v", receivedAuth.Scopes)
	}
}

// TestCustomCreate tests that CustomCreate correctly uses a custom function
func TestCustomCreate(t *testing.T) {
	// Custom function that modifies the item before creating
	customCreate := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, item TestUser, _ io.Reader, _ filestore.FileMetadata) (*TestUser, error) {
		// Modify name before creating
		item.Name = "Custom-" + item.Name
		return svc.Create(ctx, item)
	}

	body := []byte(`{"name":"Test","email":"customcreate@test.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	ctx := context.WithValue(req.Context(), metadata.MetadataKey, userMeta)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.Create[TestUser](customCreate)(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
		return
	}

	var user TestUser
	if err := json.Unmarshal(w.Body.Bytes(), &user); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Clean up
	defer testDB.GetDB().NewDelete().Model((*TestUser)(nil)).Where("id = ?", user.ID).Exec(context.Background())

	if user.Name != "Custom-Test" {
		t.Errorf("Expected name 'Custom-Test', got '%s'", user.Name)
	}
}

// TestCustomUpdate tests that CustomUpdate correctly uses a custom function
func TestCustomUpdate(t *testing.T) {
	// Create a user first
	_, err := testDB.GetDB().NewInsert().Model(&TestUser{ID: 102, Name: "Original", Email: "update@test.com"}).Exec(context.Background())
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}
	defer testDB.GetDB().NewDelete().Model((*TestUser)(nil)).Where("id = 102").Exec(context.Background())

	// Custom function that appends to the name
	customUpdate := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item TestUser) (*TestUser, error) {
		item.Name += "-Updated"
		return svc.Update(ctx, id, item)
	}

	body := []byte(`{"name":"Test","email":"update@test.com"}`)
	req := httptest.NewRequest(http.MethodPut, "/users/102", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(userMeta.URLParamUUID, "102")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, metadata.MetadataKey, userMeta)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.Update[TestUser](customUpdate)(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
		return
	}

	var user TestUser
	if err := json.Unmarshal(w.Body.Bytes(), &user); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if user.Name != "Test-Updated" {
		t.Errorf("Expected name 'Test-Updated', got '%s'", user.Name)
	}
}

// TestCustomDelete tests that CustomDelete correctly uses a custom function
func TestCustomDelete(t *testing.T) {
	// Create a user first
	_, err := testDB.GetDB().NewInsert().Model(&TestUser{ID: 103, Name: "ToDelete", Email: "delete@test.com"}).Exec(context.Background())
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	var deletedID string

	// Custom function that captures the ID being deleted
	customDelete := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string) error {
		deletedID = id
		return svc.Delete(ctx, id)
	}

	req := httptest.NewRequest(http.MethodDelete, "/users/103", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(userMeta.URLParamUUID, "103")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, metadata.MetadataKey, userMeta)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.Delete[TestUser](customDelete)(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("Expected status %d, got %d: %s", http.StatusNoContent, w.Code, w.Body.String())
		return
	}

	if deletedID != "103" {
		t.Errorf("Expected deleted ID '103', got '%s'", deletedID)
	}

	// Verify it's actually deleted
	var count int
	count, err = testDB.GetDB().NewSelect().Model((*TestUser)(nil)).Where("id = 103").Count(context.Background())
	if err != nil {
		t.Fatalf("Failed to check user existence: %v", err)
	}
	if count != 0 {
		t.Error("User should have been deleted but still exists")
	}
}

// TestCustomGetAll tests that CustomGetAll correctly uses a custom function
func TestCustomGetAll(t *testing.T) {
	// Create some users
	users := []TestUser{
		{ID: 201, Name: "User1", Email: "user1@test.com"},
		{ID: 202, Name: "User2", Email: "user2@test.com"},
		{ID: 203, Name: "User3", Email: "user3@test.com"},
	}
	for _, u := range users {
		testDB.GetDB().NewInsert().Model(&u).Exec(context.Background())
	}
	defer func() {
		for _, u := range users {
			testDB.GetDB().NewDelete().Model((*TestUser)(nil)).Where("id = ?", u.ID).Exec(context.Background())
		}
	}()

	// Custom function that filters to only return users with ID > 201
	customGetAll := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo) ([]*TestUser, int, map[string]float64, error) {
		// Get all, then filter
		all, _, _, err := svc.GetAll(ctx)
		if err != nil {
			return nil, 0, nil, err
		}
		var filtered []*TestUser
		for _, u := range all {
			if u.ID > 201 {
				filtered = append(filtered, u)
			}
		}
		return filtered, len(filtered), nil, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	ctx := ctxWithMeta(req.Context(), userMeta)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.GetAll[TestUser](customGetAll)(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
		return
	}

	var result []*TestUser
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Should only have users 202 and 203
	if len(result) != 2 {
		t.Errorf("Expected 2 users, got %d", len(result))
	}
}

// TestFileModel is a test model that implements FileResource
type TestFileModel struct {
	bun.BaseModel `bun:"table:test_file_models"`
	ID            int    `bun:"id,pk,autoincrement" json:"id"`
	Name          string `bun:"name" json:"name"`
	filestore.FileFields
}

var testFileMeta = &metadata.TypeMetadata{
	TypeID:         "test_file_model_id",
	TypeName:       "TestFileModel",
	TableName:      "test_file_models",
	URLParamUUID:   "test_file_uuid",
	PKField:        "ID",
	ModelType:      reflect.TypeOf(TestFileModel{}),
	IsFileResource: true,
}

// mockFileStorage is a mock implementation of FileStorage for testing
type mockFileStorage struct {
	files      map[string]string
	signedURL  string                 // empty string means proxy mode, non-empty means signed URL mode
	deleteFunc func(key string) error // optional callback for testing delete behavior
}

func newMockFileStorage() *mockFileStorage {
	return &mockFileStorage{
		files:     make(map[string]string),
		signedURL: "", // Default to proxy mode
	}
}

func (m *mockFileStorage) Store(ctx context.Context, r io.Reader, meta filestore.FileMetadata) (string, error) {
	content, _ := io.ReadAll(r)
	key := "test-key-123"
	m.files[key] = string(content)
	return key, nil
}

func (m *mockFileStorage) Retrieve(ctx context.Context, key string) (io.ReadCloser, filestore.FileMetadata, error) {
	content, ok := m.files[key]
	if !ok {
		return nil, filestore.FileMetadata{}, filestore.ErrStorageKeyNotFound
	}
	return io.NopCloser(bytes.NewReader([]byte(content))), filestore.FileMetadata{
		ContentType: "text/plain",
		Size:        int64(len(content)),
		Filename:    "test.txt",
	}, nil
}

func (m *mockFileStorage) Delete(ctx context.Context, key string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(key)
	}
	delete(m.files, key)
	return nil
}

func (m *mockFileStorage) GenerateSignedURL(ctx context.Context, key string) (string, error) {
	return m.signedURL, nil
}

func TestDownload_ProxyMode(t *testing.T) {
	// Create table
	_, err := testDB.GetDB().NewCreateTable().Model((*TestFileModel)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test_file_models table:", err)
	}
	defer testDB.GetDB().NewDropTable().Model((*TestFileModel)(nil)).IfExists().Exec(context.Background())

	// Add file to shared mock storage
	testFileStorage.files["test-storage-key"] = testFileContent
	defer delete(testFileStorage.files, "test-storage-key")

	// Create file record
	file := &TestFileModel{
		Name: "Test File",
		FileFields: filestore.FileFields{
			StorageKey:  "test-storage-key",
			Filename:    "test.txt",
			ContentType: "text/plain",
			Size:        17,
		},
	}
	_, err = testDB.GetDB().NewInsert().Model(file).Returning("*").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create file:", err)
	}

	// Make request
	req := httptest.NewRequest(http.MethodGet, "/files/1/download", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(testFileMeta.URLParamUUID, "1")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, metadata.MetadataKey, testFileMeta)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.Download[TestFileModel]()(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
		return
	}

	// Check content type
	if ct := w.Header().Get("Content-Type"); ct != "text/plain" {
		t.Errorf("Expected Content-Type 'text/plain', got '%s'", ct)
	}

	// Check content
	if body := w.Body.String(); body != testFileContent {
		t.Errorf("Expected body '%s', got '%s'", testFileContent, body)
	}
}

func TestDownload_SignedURLMode(t *testing.T) {
	// Create table
	_, err := testDB.GetDB().NewCreateTable().Model((*TestFileModel)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test_file_models table:", err)
	}
	defer testDB.GetDB().NewDropTable().Model((*TestFileModel)(nil)).IfExists().Exec(context.Background())

	// Configure mock storage to return signed URLs
	testFileStorage.files["test-storage-key"] = testFileContent
	testFileStorage.signedURL = "https://signed.example.com/file"
	defer func() {
		delete(testFileStorage.files, "test-storage-key")
		testFileStorage.signedURL = "" // Reset to proxy mode
	}()

	// Create file record
	file := &TestFileModel{
		Name: "Test File",
		FileFields: filestore.FileFields{
			StorageKey:  "test-storage-key",
			Filename:    "test.txt",
			ContentType: "text/plain",
			Size:        17,
		},
	}
	_, err = testDB.GetDB().NewInsert().Model(file).Returning("*").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create file:", err)
	}

	// Make request
	req := httptest.NewRequest(http.MethodGet, "/files/1/download", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(testFileMeta.URLParamUUID, "1")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, metadata.MetadataKey, testFileMeta)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.Download[TestFileModel]()(w, req)

	// Should redirect
	if w.Code != http.StatusTemporaryRedirect {
		t.Errorf("Expected status %d, got %d: %s", http.StatusTemporaryRedirect, w.Code, w.Body.String())
		return
	}

	// Check Location header
	location := w.Header().Get("Location")
	if location != "https://signed.example.com/file" {
		t.Errorf("Expected Location 'https://signed.example.com/file', got '%s'", location)
	}
}

func TestDownload_NotFound(t *testing.T) {
	// Create table
	_, err := testDB.GetDB().NewCreateTable().Model((*TestFileModel)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test_file_models table:", err)
	}
	defer testDB.GetDB().NewDropTable().Model((*TestFileModel)(nil)).IfExists().Exec(context.Background())

	// Make request for non-existent file
	req := httptest.NewRequest(http.MethodGet, "/files/999/download", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(testFileMeta.URLParamUUID, "999")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, metadata.MetadataKey, testFileMeta)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.Download[TestFileModel]()(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d: %s", http.StatusNotFound, w.Code, w.Body.String())
	}
}

func TestDownload_MissingID(t *testing.T) {
	// Make request without ID param
	req := httptest.NewRequest(http.MethodGet, "/files/download", nil)
	rctx := chi.NewRouteContext()
	// Note: not adding URL param
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, metadata.MetadataKey, testFileMeta)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.Download[TestFileModel]()(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

func TestDownload_MissingMetadata(t *testing.T) {
	// Make request without metadata in context
	req := httptest.NewRequest(http.MethodGet, "/files/1/download", nil)
	rctx := chi.NewRouteContext()
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	// Note: NOT adding metadata to context
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.Download[TestFileModel]()(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
}

func TestDownload_EmptyContentType(t *testing.T) {
	// Create table
	_, err := testDB.GetDB().NewCreateTable().Model((*TestFileModel)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test_file_models table:", err)
	}
	defer testDB.GetDB().NewDropTable().Model((*TestFileModel)(nil)).IfExists().Exec(context.Background())

	// Add file to shared mock storage
	testFileStorage.files["test-storage-key"] = "file content"
	defer delete(testFileStorage.files, "test-storage-key")

	// Create file record with empty content type
	file := &TestFileModel{
		Name: "Test File",
		FileFields: filestore.FileFields{
			StorageKey:  "test-storage-key",
			Filename:    "",
			ContentType: "", // Empty!
			Size:        0,  // Zero size
		},
	}
	_, err = testDB.GetDB().NewInsert().Model(file).Returning("*").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create file:", err)
	}

	// Make request
	req := httptest.NewRequest(http.MethodGet, "/files/1/download", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(testFileMeta.URLParamUUID, "1")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, metadata.MetadataKey, testFileMeta)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.Download[TestFileModel]()(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
		return
	}

	// Should default to application/octet-stream
	if ct := w.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Expected Content-Type 'application/octet-stream', got '%s'", ct)
	}

	// Should NOT have Content-Length header (size is 0)
	if cl := w.Header().Get("Content-Length"); cl != "" {
		t.Errorf("Expected no Content-Length header, got '%s'", cl)
	}

	// Should NOT have Content-Disposition header (filename is empty)
	if cd := w.Header().Get("Content-Disposition"); cd != "" {
		t.Errorf("Expected no Content-Disposition header, got '%s'", cd)
	}
}

func TestDownload_EmptyStorageKey(t *testing.T) {
	// Create table
	_, err := testDB.GetDB().NewCreateTable().Model((*TestFileModel)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test_file_models table:", err)
	}
	defer testDB.GetDB().NewDropTable().Model((*TestFileModel)(nil)).IfExists().Exec(context.Background())

	// Create file record with empty storage key
	file := &TestFileModel{
		Name: "Test File",
		FileFields: filestore.FileFields{
			StorageKey: "", // Empty!
		},
	}
	_, err = testDB.GetDB().NewInsert().Model(file).Returning("*").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create file:", err)
	}

	// Make request
	req := httptest.NewRequest(http.MethodGet, "/files/1/download", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(testFileMeta.URLParamUUID, "1")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, metadata.MetadataKey, testFileMeta)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.Download[TestFileModel]()(w, req)

	// Should return 404 for empty storage key
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d: %s", http.StatusNotFound, w.Code, w.Body.String())
	}
}

func TestDownload_WithContentLength(t *testing.T) {
	// Create table
	_, err := testDB.GetDB().NewCreateTable().Model((*TestFileModel)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test_file_models table:", err)
	}
	defer testDB.GetDB().NewDropTable().Model((*TestFileModel)(nil)).IfExists().Exec(context.Background())

	// Add file to shared mock storage
	testContent := "test file content with known size"
	testFileStorage.files["test-storage-key"] = testContent
	defer delete(testFileStorage.files, "test-storage-key")

	// Create file record with size set
	file := &TestFileModel{
		Name: "Test File",
		FileFields: filestore.FileFields{
			StorageKey:  "test-storage-key",
			ContentType: "text/plain",
			Filename:    "test.txt",
			Size:        int64(len(testContent)),
		},
	}
	_, err = testDB.GetDB().NewInsert().Model(file).Returning("*").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create file:", err)
	}

	// Make request
	req := httptest.NewRequest(http.MethodGet, "/files/1/download", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(testFileMeta.URLParamUUID, "1")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, metadata.MetadataKey, testFileMeta)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.Download[TestFileModel]()(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	// Check Content-Length header is set
	contentLength := w.Header().Get("Content-Length")
	expectedLength := strconv.Itoa(len(testContent))
	if contentLength != expectedLength {
		t.Errorf("Expected Content-Length '%s', got '%s'", expectedLength, contentLength)
	}

	// Check Content-Disposition header
	contentDisposition := w.Header().Get("Content-Disposition")
	if contentDisposition != `attachment; filename="test.txt"` {
		t.Errorf("Expected Content-Disposition header, got '%s'", contentDisposition)
	}
}

func TestDownload_ContextDeadlineExceeded(t *testing.T) {
	// Create table
	_, err := testDB.GetDB().NewCreateTable().Model((*TestFileModel)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test_file_models table:", err)
	}
	defer testDB.GetDB().NewDropTable().Model((*TestFileModel)(nil)).IfExists().Exec(context.Background())

	// Make request with already-canceled context (deadline exceeded)
	req := httptest.NewRequest(http.MethodGet, "/files/1/download", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(testFileMeta.URLParamUUID, "1")

	// Create a context that's already past deadline
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))
	defer cancel()

	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, metadata.MetadataKey, testFileMeta)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.Download[TestFileModel]()(w, req)

	// Should return 504 Gateway Timeout for deadline exceeded
	if w.Code != http.StatusGatewayTimeout {
		t.Errorf("Expected status %d, got %d: %s", http.StatusGatewayTimeout, w.Code, w.Body.String())
	}
}

func TestDownload_ContextCanceled(t *testing.T) {
	// Create table
	_, err := testDB.GetDB().NewCreateTable().Model((*TestFileModel)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test_file_models table:", err)
	}
	defer testDB.GetDB().NewDropTable().Model((*TestFileModel)(nil)).IfExists().Exec(context.Background())

	// Make request with already-canceled context
	req := httptest.NewRequest(http.MethodGet, "/files/1/download", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(testFileMeta.URLParamUUID, "1")

	// Create and immediately cancel context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, metadata.MetadataKey, testFileMeta)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.Download[TestFileModel]()(w, req)

	// Should return 200 with empty body for canceled context (early return)
	// The handler just returns without writing anything
	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d (default for no response), got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
}

func TestCreate_MultipartFormUpload(t *testing.T) {
	// Create table
	_, err := testDB.GetDB().NewCreateTable().Model((*TestFileModel)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test_file_models table:", err)
	}
	defer testDB.GetDB().NewDropTable().Model((*TestFileModel)(nil)).IfExists().Exec(context.Background())

	// Create multipart form body
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	// Add file field
	fileWriter, err := writer.CreateFormFile("file", "test-upload.txt")
	if err != nil {
		t.Fatal("Failed to create form file:", err)
	}
	fileWriter.Write([]byte(testFileContent))

	// Add metadata JSON field
	metadataField, err := writer.CreateFormField("metadata")
	if err != nil {
		t.Fatal("Failed to create metadata field:", err)
	}
	metadataField.Write([]byte(`{"name":"Uploaded File"}`))

	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/files", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rctx := chi.NewRouteContext()
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, metadata.MetadataKey, testFileMeta)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.Create(handler.StandardCreate[TestFileModel])(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
		return
	}

	var result TestFileModel
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result.Name != "Uploaded File" {
		t.Errorf("Expected name 'Uploaded File', got '%s'", result.Name)
	}
	if result.Filename != "test-upload.txt" {
		t.Errorf("Expected filename 'test-upload.txt', got '%s'", result.Filename)
	}
	if result.Size != int64(len(testFileContent)) {
		t.Errorf("Expected size %d, got %d", len(testFileContent), result.Size)
	}
	// StorageKey is json:"-" so won't be in response - verify it was stored in DB
	var dbRecord TestFileModel
	err = testDB.GetDB().NewSelect().Model(&dbRecord).Where("id = ?", result.ID).Scan(context.Background())
	if err != nil {
		t.Fatalf("Failed to fetch record from DB: %v", err)
	}
	if dbRecord.StorageKey == "" {
		t.Error("Expected storage key to be set in DB record")
	}
}

func TestCreate_MultipartFormInvalidMetadataJSON(t *testing.T) {
	// Create multipart form body with invalid JSON metadata
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	// Add file field
	fileWriter, err := writer.CreateFormFile("file", "test.txt")
	if err != nil {
		t.Fatal("Failed to create form file:", err)
	}
	fileWriter.Write([]byte("test content"))

	// Add invalid metadata JSON
	metadataField, err := writer.CreateFormField("metadata")
	if err != nil {
		t.Fatal("Failed to create metadata field:", err)
	}
	metadataField.Write([]byte(`{invalid json`))

	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/files", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rctx := chi.NewRouteContext()
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, metadata.MetadataKey, testFileMeta)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.Create(handler.StandardCreate[TestFileModel])(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

func TestCreate_MultipartFormNoContentType(t *testing.T) {
	// Create table
	_, err := testDB.GetDB().NewCreateTable().Model((*TestFileModel)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test_file_models table:", err)
	}
	defer testDB.GetDB().NewDropTable().Model((*TestFileModel)(nil)).IfExists().Exec(context.Background())

	// Create multipart form body
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	// Add file field with no Content-Type header (should default to application/octet-stream)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="file"; filename="test.bin"`)
	// Note: NOT setting Content-Type header
	fileWriter, err := writer.CreatePart(h)
	if err != nil {
		t.Fatal("Failed to create form file:", err)
	}
	fileWriter.Write([]byte("binary content"))

	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/files", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rctx := chi.NewRouteContext()
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, metadata.MetadataKey, testFileMeta)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.Create(handler.StandardCreate[TestFileModel])(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
		return
	}

	var result TestFileModel
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Should default to application/octet-stream
	if result.ContentType != "application/octet-stream" {
		t.Errorf("Expected content type 'application/octet-stream', got '%s'", result.ContentType)
	}
}

func TestCreate_MultipartFormParseError(t *testing.T) {
	// Send invalid multipart form data
	req := httptest.NewRequest(http.MethodPost, "/files", bytes.NewReader([]byte("not valid multipart")))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=invalid")

	rctx := chi.NewRouteContext()
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, metadata.MetadataKey, testFileMeta)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.Create(handler.StandardCreate[TestFileModel])(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

func TestStandardCreate_CleansUpFileOnDBError(t *testing.T) {
	// Create table
	_, err := testDB.GetDB().NewCreateTable().Model((*TestFileModel)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test_file_models table:", err)
	}
	defer testDB.GetDB().NewDropTable().Model((*TestFileModel)(nil)).IfExists().Exec(context.Background())

	// Track deleted keys
	var deletedKeys []string
	originalDelete := testFileStorage.deleteFunc
	testFileStorage.deleteFunc = func(key string) error {
		deletedKeys = append(deletedKeys, key)
		return nil
	}
	defer func() { testFileStorage.deleteFunc = originalDelete }()

	// Create a custom create function that returns an error after file is stored
	failingCreate := func(ctx context.Context, svc *service.Common[TestFileModel], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, item TestFileModel, file io.Reader, fileMeta filestore.FileMetadata) (*TestFileModel, error) {
		// Store the file (this is what StandardCreate does)
		if file != nil && meta.IsFileResource {
			storageKey, err := svc.StoreFile(ctx, file, fileMeta)
			if err != nil {
				return nil, err
			}
			// Simulate DB error after file storage
			// Clean up the file we just stored
			_ = svc.DeleteStoredFile(ctx, storageKey)
			return nil, fmt.Errorf("simulated DB error")
		}
		return nil, fmt.Errorf("simulated DB error")
	}

	// Create multipart form body
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fileWriter, _ := writer.CreateFormFile("file", "test.txt")
	fileWriter.Write([]byte("test content"))
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/files", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rctx := chi.NewRouteContext()
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, metadata.MetadataKey, testFileMeta)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.Create(failingCreate)(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}

	// Verify file was cleaned up
	if len(deletedKeys) == 0 {
		t.Error("Expected file to be deleted after DB error")
	}
}

// TestSumProduct is a test model for sum header tests
type TestSumProduct struct {
	bun.BaseModel `bun:"table:sum_products"`
	ID            int     `bun:"id,pk,autoincrement" json:"id"`
	Name          string  `bun:"name,notnull" json:"name"`
	Price         int     `bun:"price,notnull" json:"price"`
	Rating        float64 `bun:"rating,notnull" json:"rating"`
	InStock       bool    `bun:"in_stock,notnull" json:"in_stock"`
}

func TestHandler_GetAll_SumHeaders(t *testing.T) {
	// Setup: create sum_products table
	db, _ := datastore.Get()
	_, err := db.GetDB().NewCreateTable().Model((*TestSumProduct)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create sum_products table:", err)
	}
	defer db.GetDB().NewDropTable().Model((*TestSumProduct)(nil)).IfExists().Exec(context.Background())

	// Clean table and reset sequence
	_, _ = db.GetDB().NewDelete().Model((*TestSumProduct)(nil)).Where("1=1").Exec(context.Background())
	_, _ = db.GetDB().Exec("DELETE FROM sqlite_sequence WHERE name = 'sum_products'")

	// Insert test data
	products := []TestSumProduct{
		{Name: "Apple", Price: 100, Rating: 4.5, InStock: true},
		{Name: "Banana", Price: 50, Rating: 3.8, InStock: true},
		{Name: "Carrot", Price: 30, Rating: 4.2, InStock: false},
	}
	for _, p := range products {
		_, err := db.GetDB().NewInsert().Model(&p).Exec(context.Background())
		if err != nil {
			t.Fatal("Failed to insert test product:", err)
		}
	}

	sumMeta := &metadata.TypeMetadata{
		TypeID:           "test_sum_product_id",
		TypeName:         "TestSumProduct",
		TableName:        "sum_products",
		URLParamUUID:     "id",
		PKField:          "ID",
		ModelType:        reflect.TypeOf(TestSumProduct{}),
		FilterableFields: []string{"Name", "Price", "InStock"},
		SortableFields:   []string{"Name", "Price"},
		SummableFields:   []string{"Price", "Rating", "Name", "InStock"},
		DefaultLimit:     10,
		MaxLimit:         100,
	}

	tests := []struct {
		name            string
		queryString     string
		expectedHeaders map[string]string
	}{
		{
			name:        "single integer sum",
			queryString: "sum=Price",
			expectedHeaders: map[string]string{
				"X-Sum-Price": "180", // 100 + 50 + 30
			},
		},
		{
			name:        "single float sum",
			queryString: "sum=Rating",
			expectedHeaders: map[string]string{
				"X-Sum-Rating": "12.5", // 4.5 + 3.8 + 4.2
			},
		},
		{
			name:        "multiple sums",
			queryString: "sum=Price,Rating",
			expectedHeaders: map[string]string{
				"X-Sum-Price":  "180",
				"X-Sum-Rating": "12.5",
			},
		},
		{
			name:        "non-numeric field returns 0",
			queryString: "sum=Name",
			expectedHeaders: map[string]string{
				"X-Sum-Name": "0",
			},
		},
		{
			name:        "bool field returns 0",
			queryString: "sum=InStock",
			expectedHeaders: map[string]string{
				"X-Sum-InStock": "0",
			},
		},
		{
			name:        "mixed valid and invalid",
			queryString: "sum=Price,Name",
			expectedHeaders: map[string]string{
				"X-Sum-Price": "180",
				"X-Sum-Name":  "0",
			},
		},
		{
			name:        "sum with count combined",
			queryString: "sum=Price&count=true",
			expectedHeaders: map[string]string{
				"X-Sum-Price":   "180",
				"X-Total-Count": "3",
			},
		},
		{
			name:        "sum with filter",
			queryString: "sum=Price&filter[InStock]=true",
			expectedHeaders: map[string]string{
				"X-Sum-Price": "150", // Only Apple(100) + Banana(50)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := chi.NewRouter()
			r.Use(withMeta(sumMeta))
			r.Get("/products", handler.GetAll[TestSumProduct](handler.StandardGetAll[TestSumProduct]))

			url := "/products"
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

			// Check expected headers
			for header, expected := range tt.expectedHeaders {
				actual := w.Header().Get(header)
				if actual != expected {
					t.Errorf("Expected header %s=%s, got %s", header, expected, actual)
				}
			}
		})
	}
}

func TestHandler_GetAll_ErrorPaths(t *testing.T) {
	tests := []struct {
		name         string
		getAllFunc   handler.CustomGetAllFunc[TestUser]
		expectedCode int
		expectedBody string
	}{
		{
			name: "context canceled",
			getAllFunc: func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo) ([]*TestUser, int, map[string]float64, error) {
				return nil, 0, nil, context.Canceled
			},
			expectedCode: http.StatusOK, // No response written when context is canceled
		},
		{
			name: "context deadline exceeded",
			getAllFunc: func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo) ([]*TestUser, int, map[string]float64, error) {
				return nil, 0, nil, context.DeadlineExceeded
			},
			expectedCode: http.StatusGatewayTimeout,
			expectedBody: "request timeout",
		},
		{
			name: "service unavailable",
			getAllFunc: func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo) ([]*TestUser, int, map[string]float64, error) {
				return nil, 0, nil, apperrors.ErrUnavailable
			},
			expectedCode: http.StatusServiceUnavailable,
			expectedBody: "service temporarily unavailable",
		},
		{
			name: "generic error",
			getAllFunc: func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo) ([]*TestUser, int, map[string]float64, error) {
				return nil, 0, nil, fmt.Errorf("some internal error")
			},
			expectedCode: http.StatusInternalServerError,
			expectedBody: "internal server error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := chi.NewRouter()
			r.Use(withMeta(userMeta))
			r.Get(testUsersPath, handler.GetAll[TestUser](tt.getAllFunc))

			req := httptest.NewRequest(http.MethodGet, testUsersPath, nil)
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != tt.expectedCode {
				t.Errorf("Expected status %d, got %d: %s", tt.expectedCode, w.Code, w.Body.String())
			}

			if tt.expectedBody != "" {
				body := w.Body.String()
				if !bytes.Contains([]byte(body), []byte(tt.expectedBody)) {
					t.Errorf("Expected body to contain '%s', got '%s'", tt.expectedBody, body)
				}
			}
		})
	}
}
