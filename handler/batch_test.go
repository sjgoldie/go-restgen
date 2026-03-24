//nolint:staticcheck,errcheck,gosec // Test code - string context keys and unchecked test cleanup are acceptable
package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	apperrors "github.com/sjgoldie/go-restgen/errors"
	"github.com/sjgoldie/go-restgen/handler"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/service"
)

// Common test bodies
const testBatchSingleUserBody = `[{"name": "User 1", "email": "user1@example.com"}]`

// testFileResourceRejection is a helper for testing that batch operations reject file resources
func testFileResourceRejection(t *testing.T, method, path, body string, setupHandler func(chi.Router, *metadata.TypeMetadata)) {
	t.Helper()

	fileMeta := &metadata.TypeMetadata{
		TypeID:         userMeta.TypeID,
		TypeName:       userMeta.TypeName,
		TableName:      userMeta.TableName,
		URLParamUUID:   userMeta.URLParamUUID,
		ModelType:      userMeta.ModelType,
		PKField:        "ID",
		IsFileResource: true,
	}

	r := chi.NewRouter()
	r.Route(path, func(r chi.Router) {
		r.Use(withMeta(fileMeta))
		setupHandler(r, fileMeta)
	})

	req := httptest.NewRequest(method, path+"/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("Expected status 501, got %d: %s", w.Code, w.Body.String())
	}
}

// TestBatchCreate_Success tests successful batch creation
func TestBatchCreate_Success(t *testing.T) {
	cleanTable(t)

	r := chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Post("/batch", handler.BatchCreate[TestUser](handler.StandardBatchCreate[TestUser]))
	})

	body := `[{"name": "User 1", "email": "user1@example.com"}, {"name": "User 2", "email": "user2@example.com"}]`
	req := httptest.NewRequest("POST", "/users/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status 201, got %d: %s", w.Code, w.Body.String())
	}

	var results []TestUser
	if err := json.Unmarshal(w.Body.Bytes(), &results); err != nil {
		t.Fatal("Failed to unmarshal response:", err)
	}

	if len(results) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(results))
	}

	if results[0].Name != "User 1" {
		t.Errorf("Expected name 'User 1', got %q", results[0].Name)
	}
	if results[1].Name != "User 2" {
		t.Errorf("Expected name 'User 2', got %q", results[1].Name)
	}
	if results[0].ID == 0 || results[1].ID == 0 {
		t.Error("Expected IDs to be populated")
	}
}

// TestBatchCreate_EmptyArray tests batch create with empty array
func TestBatchCreate_EmptyArray(t *testing.T) {
	cleanTable(t)

	r := chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Post("/batch", handler.BatchCreate[TestUser](handler.StandardBatchCreate[TestUser]))
	})

	body := `[]`
	req := httptest.NewRequest("POST", "/users/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestBatchCreate_InvalidJSON tests batch create with invalid JSON
func TestBatchCreate_InvalidJSON(t *testing.T) {
	cleanTable(t)

	r := chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Post("/batch", handler.BatchCreate[TestUser](handler.StandardBatchCreate[TestUser]))
	})

	body := `not valid json`
	req := httptest.NewRequest("POST", "/users/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestBatchCreate_ExceedsLimit tests batch create exceeding batch limit
func TestBatchCreate_ExceedsLimit(t *testing.T) {
	cleanTable(t)

	// Create metadata with batch limit of 2
	limitedMeta := &metadata.TypeMetadata{
		TypeID:       userMeta.TypeID,
		TypeName:     userMeta.TypeName,
		TableName:    userMeta.TableName,
		URLParamUUID: userMeta.URLParamUUID,
		ModelType:    userMeta.ModelType,
		BatchLimit:   2,
	}

	r := chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		r.Use(withMeta(limitedMeta))
		r.Post("/batch", handler.BatchCreate[TestUser](handler.StandardBatchCreate[TestUser]))
	})

	body := `[{"name": "User 1", "email": "user1@example.com"}, {"name": "User 2", "email": "user2@example.com"}, {"name": "User 3", "email": "user3@example.com"}]`
	req := httptest.NewRequest("POST", "/users/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d: %s", w.Code, w.Body.String())
	}

	if !strings.Contains(w.Body.String(), "bad_request") {
		t.Errorf("Expected error code bad_request, got %s", w.Body.String())
	}
}

// TestBatchCreate_FileResource tests batch create returns 501 for file resources
func TestBatchCreate_FileResource(t *testing.T) {
	cleanTable(t)
	testFileResourceRejection(t, "POST", "/files", `[{"name": "File 1", "email": "file1@example.com"}]`,
		func(r chi.Router, _ *metadata.TypeMetadata) {
			r.Post("/batch", handler.BatchCreate[TestUser](handler.StandardBatchCreate[TestUser]))
		})
}

// TestBatchUpdate_Success tests successful batch update
func TestBatchUpdate_Success(t *testing.T) {
	cleanTable(t)

	// Create test users
	db := testDB.GetDB()
	user1 := &TestUser{Name: "User 1", Email: "user1@example.com"}
	user2 := &TestUser{Name: "User 2", Email: "user2@example.com"}
	_, err := db.NewInsert().Model(user1).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test user 1:", err)
	}
	_, err = db.NewInsert().Model(user2).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test user 2:", err)
	}

	r := chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Put("/batch", handler.BatchUpdate[TestUser](handler.StandardBatchUpdate[TestUser]))
	})

	body := `[{"id": 1, "name": "Updated User 1", "email": "user1@example.com"}, {"id": 2, "name": "Updated User 2", "email": "user2@example.com"}]`
	req := httptest.NewRequest("PUT", "/users/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var results []TestUser
	if err := json.Unmarshal(w.Body.Bytes(), &results); err != nil {
		t.Fatal("Failed to unmarshal response:", err)
	}

	if len(results) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(results))
	}

	if results[0].Name != "Updated User 1" {
		t.Errorf("Expected name 'Updated User 1', got %q", results[0].Name)
	}
	if results[1].Name != "Updated User 2" {
		t.Errorf("Expected name 'Updated User 2', got %q", results[1].Name)
	}
}

// TestBatchUpdate_EmptyArray tests batch update with empty array
func TestBatchUpdate_EmptyArray(t *testing.T) {
	cleanTable(t)

	r := chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Put("/batch", handler.BatchUpdate[TestUser](handler.StandardBatchUpdate[TestUser]))
	})

	body := `[]`
	req := httptest.NewRequest("PUT", "/users/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestBatchUpdate_FileResource tests batch update returns 501 for file resources
func TestBatchUpdate_FileResource(t *testing.T) {
	cleanTable(t)
	testFileResourceRejection(t, "PUT", "/files", `[{"id": 1, "name": "Updated File 1", "email": "file1@example.com"}]`,
		func(r chi.Router, _ *metadata.TypeMetadata) {
			r.Put("/batch", handler.BatchUpdate[TestUser](handler.StandardBatchUpdate[TestUser]))
		})
}

// TestBatchDelete_Success tests successful batch delete
func TestBatchDelete_Success(t *testing.T) {
	cleanTable(t)

	// Create test users
	db := testDB.GetDB()
	user1 := &TestUser{Name: "User 1", Email: "user1@example.com"}
	user2 := &TestUser{Name: "User 2", Email: "user2@example.com"}
	_, err := db.NewInsert().Model(user1).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test user 1:", err)
	}
	_, err = db.NewInsert().Model(user2).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test user 2:", err)
	}

	r := chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Delete("/batch", handler.BatchDelete[TestUser](handler.StandardBatchDelete[TestUser]))
	})

	body := `[{"id": 1}, {"id": 2}]`
	req := httptest.NewRequest("DELETE", "/users/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("Expected status 204, got %d: %s", w.Code, w.Body.String())
	}

	// Verify users are deleted
	var count int
	count, err = db.NewSelect().Model((*TestUser)(nil)).Count(context.Background())
	if err != nil {
		t.Fatal("Failed to count users:", err)
	}
	if count != 0 {
		t.Errorf("Expected 0 users, got %d", count)
	}
}

// TestBatchDelete_EmptyArray tests batch delete with empty array
func TestBatchDelete_EmptyArray(t *testing.T) {
	cleanTable(t)

	r := chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Delete("/batch", handler.BatchDelete[TestUser](handler.StandardBatchDelete[TestUser]))
	})

	body := `[]`
	req := httptest.NewRequest("DELETE", "/users/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestBatchDelete_ExceedsLimit tests batch delete exceeding batch limit
func TestBatchDelete_ExceedsLimit(t *testing.T) {
	cleanTable(t)

	limitedMeta := &metadata.TypeMetadata{
		TypeID:       userMeta.TypeID,
		TypeName:     userMeta.TypeName,
		TableName:    userMeta.TableName,
		URLParamUUID: userMeta.URLParamUUID,
		ModelType:    userMeta.ModelType,
		BatchLimit:   2,
	}

	r := chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		r.Use(withMeta(limitedMeta))
		r.Delete("/batch", handler.BatchDelete[TestUser](handler.StandardBatchDelete[TestUser]))
	})

	body := `[{"id": 1}, {"id": 2}, {"id": 3}]`
	req := httptest.NewRequest("DELETE", "/users/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestBatchCreate_CustomHandler tests batch create with custom handler
func TestBatchCreate_CustomHandler(t *testing.T) {
	cleanTable(t)

	customFn := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, items []TestUser) ([]*TestUser, error) {
		// Modify all names before creating
		for i := range items {
			items[i].Name = "Custom: " + items[i].Name
		}
		return svc.BatchCreate(ctx, items)
	}

	r := chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Post("/batch", handler.BatchCreate[TestUser](customFn))
	})

	req := httptest.NewRequest("POST", "/users/batch", strings.NewReader(testBatchSingleUserBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status 201, got %d: %s", w.Code, w.Body.String())
	}

	var results []TestUser
	if err := json.Unmarshal(w.Body.Bytes(), &results); err != nil {
		t.Fatal("Failed to unmarshal response:", err)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	expected := "Custom: User 1"
	if results[0].Name != expected {
		t.Errorf("Expected name %q, got %q", expected, results[0].Name)
	}
}

// TestBatchUpdate_NotFound tests batch update with non-existent item
func TestBatchUpdate_NotFound(t *testing.T) {
	cleanTable(t)

	r := chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Put("/batch", handler.BatchUpdate[TestUser](handler.StandardBatchUpdate[TestUser]))
	})

	body := `[{"id": 999, "name": "Non-existent", "email": "none@example.com"}]`
	req := httptest.NewRequest("PUT", "/users/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestBatchDelete_NotFound tests batch delete with non-existent item
func TestBatchDelete_NotFound(t *testing.T) {
	cleanTable(t)

	r := chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Delete("/batch", handler.BatchDelete[TestUser](handler.StandardBatchDelete[TestUser]))
	})

	body := `[{"id": 999}]`
	req := httptest.NewRequest("DELETE", "/users/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestBatchCreate_WithAuth tests batch create with auth info
func TestBatchCreate_WithAuth(t *testing.T) {
	cleanTable(t)

	var capturedAuth *metadata.AuthInfo

	customFn := func(ctx context.Context, svc *service.Common[TestUser], _ *metadata.TypeMetadata, auth *metadata.AuthInfo, items []TestUser) ([]*TestUser, error) {
		capturedAuth = auth
		return svc.BatchCreate(ctx, items)
	}

	withAuth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := &metadata.AuthInfo{UserID: "batch_user", Scopes: []string{"batch:create"}}
			ctx := context.WithValue(r.Context(), metadata.AuthInfoKey, auth)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	r := chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Use(withAuth)
		r.Post("/batch", handler.BatchCreate[TestUser](customFn))
	})

	req := httptest.NewRequest("POST", "/users/batch", strings.NewReader(testBatchSingleUserBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status 201, got %d: %s", w.Code, w.Body.String())
	}

	if capturedAuth == nil {
		t.Error("Expected auth info to be captured")
	} else if capturedAuth.UserID != "batch_user" {
		t.Errorf("Expected UserID 'batch_user', got %q", capturedAuth.UserID)
	}
}

// TestBatchCreate_ValidationError tests batch create with validation error
func TestBatchCreate_ValidationError(t *testing.T) {
	cleanTable(t)

	customFn := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, items []TestUser) ([]*TestUser, error) {
		return nil, apperrors.NewValidationError("name is required")
	}

	r := chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Post("/batch", handler.BatchCreate[TestUser](customFn))
	})

	body := `[{"name": "", "email": "user1@example.com"}]`
	req := httptest.NewRequest("POST", "/users/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d: %s", w.Code, w.Body.String())
	}

	if !strings.Contains(w.Body.String(), "name is required") {
		t.Errorf("Expected error message about validation, got %s", w.Body.String())
	}
}

// TestBatchCreate_ErrorHandling tests batch create error handling using table-driven tests
func TestBatchCreate_ErrorHandling(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		expectedStatus int
		expectedMsg    string
	}{
		{
			name:           "DuplicateError",
			err:            apperrors.ErrDuplicate,
			expectedStatus: http.StatusBadRequest,
			expectedMsg:    "duplicate",
		},
		{
			name:           "InvalidReferenceError",
			err:            apperrors.ErrInvalidReference,
			expectedStatus: http.StatusBadRequest,
			expectedMsg:    "invalid_reference",
		},
		{
			name:           "DeadlineExceeded",
			err:            context.DeadlineExceeded,
			expectedStatus: http.StatusGatewayTimeout,
			expectedMsg:    "request_timeout",
		},
		{
			name:           "Unavailable",
			err:            apperrors.ErrUnavailable,
			expectedStatus: http.StatusServiceUnavailable,
			expectedMsg:    "service_unavailable",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cleanTable(t)

			//nolint:unparam // test intentionally returns nil result
			customFn := func(_ context.Context, _ *service.Common[TestUser], _ *metadata.TypeMetadata, _ *metadata.AuthInfo, _ []TestUser) ([]*TestUser, error) {
				return nil, tc.err
			}

			r := chi.NewRouter()
			r.Route("/users", func(r chi.Router) {
				r.Use(withMeta(userMeta))
				r.Post("/batch", handler.BatchCreate[TestUser](customFn))
			})

			body := `[{"name": "Test", "email": "user1@example.com"}]`
			req := httptest.NewRequest("POST", "/users/batch", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != tc.expectedStatus {
				t.Errorf("Expected status %d, got %d: %s", tc.expectedStatus, w.Code, w.Body.String())
			}

			if !strings.Contains(w.Body.String(), tc.expectedMsg) {
				t.Errorf("Expected error about %q, got %s", tc.expectedMsg, w.Body.String())
			}
		})
	}
}

// TestBatchCreate_NoMetadata tests batch create without metadata in context
func TestBatchCreate_NoMetadata(t *testing.T) {
	cleanTable(t)

	r := chi.NewRouter()
	// No withMeta middleware - metadata missing from context
	r.Post("/users/batch", handler.BatchCreate[TestUser](handler.StandardBatchCreate[TestUser]))

	req := httptest.NewRequest("POST", "/users/batch", strings.NewReader(testBatchSingleUserBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d: %s", w.Code, w.Body.String())
	}
}

// TestBatchUpdate_NoMetadata tests batch update without metadata in context
func TestBatchUpdate_NoMetadata(t *testing.T) {
	cleanTable(t)

	r := chi.NewRouter()
	r.Put("/users/batch", handler.BatchUpdate[TestUser](handler.StandardBatchUpdate[TestUser]))

	body := `[{"id": 1, "name": "User 1", "email": "user1@example.com"}]`
	req := httptest.NewRequest("PUT", "/users/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d: %s", w.Code, w.Body.String())
	}
}

// TestBatchDelete_NoMetadata tests batch delete without metadata in context
func TestBatchDelete_NoMetadata(t *testing.T) {
	cleanTable(t)

	r := chi.NewRouter()
	r.Delete("/users/batch", handler.BatchDelete[TestUser](handler.StandardBatchDelete[TestUser]))

	body := `[{"id": 1}]`
	req := httptest.NewRequest("DELETE", "/users/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d: %s", w.Code, w.Body.String())
	}
}
