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

	"github.com/sjgoldie/go-restgen/handler"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/service"
)

// CancelPayload is a test action payload
type CancelPayload struct {
	Reason string `json:"reason"`
}

// TestAction_Success tests a successful action execution
func TestAction_Success(t *testing.T) {
	cleanTable(t)

	// Create a test user
	db := testDB.GetDB()
	user := &TestUser{Name: "Test User", Email: "test@example.com"}
	_, err := db.NewInsert().Model(user).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test user:", err)
	}

	// Action handler that updates the user name
	actionFn := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *TestUser, payload []byte) (*TestUser, error) {
		var req CancelPayload
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, err
		}
		item.Name = "Cancelled: " + req.Reason
		return svc.Update(ctx, id, *item)
	}

	r := chi.NewRouter()
	r.Route("/users/{id}", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Post("/cancel", handler.Action[TestUser](actionFn))
	})

	// Make request
	body := `{"reason": "customer request"}`
	req := httptest.NewRequest("POST", "/users/1/cancel", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify response
	var result TestUser
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal("Failed to unmarshal response:", err)
	}

	expected := "Cancelled: customer request"
	if result.Name != expected {
		t.Errorf("Expected name %q, got %q", expected, result.Name)
	}
}

// TestAction_NoContent tests an action that returns nil (204 No Content)
func TestAction_NoContent(t *testing.T) {
	cleanTable(t)

	// Create a test user
	db := testDB.GetDB()
	user := &TestUser{Name: "Test User", Email: "test@example.com"}
	_, err := db.NewInsert().Model(user).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test user:", err)
	}

	// Action handler that returns nil (no content)
	actionFn := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *TestUser, payload []byte) (*TestUser, error) {
		// Do something but return nil
		return nil, nil
	}

	r := chi.NewRouter()
	r.Route("/users/{id}", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Post("/archive", handler.Action[TestUser](actionFn))
	})

	req := httptest.NewRequest("POST", "/users/1/archive", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("Expected status 204, got %d: %s", w.Code, w.Body.String())
	}

	if w.Body.Len() > 0 {
		t.Errorf("Expected empty body, got %q", w.Body.String())
	}
}

// TestAction_NotFound tests action on non-existent resource
func TestAction_NotFound(t *testing.T) {
	cleanTable(t)

	actionFn := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *TestUser, payload []byte) (*TestUser, error) {
		return item, nil
	}

	r := chi.NewRouter()
	r.Route("/users/{id}", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Post("/cancel", handler.Action[TestUser](actionFn))
	})

	req := httptest.NewRequest("POST", "/users/999/cancel", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestAction_MissingURLParam tests action with empty URLParamUUID.
// When URLParamUUID is empty, setupRequest skips ID parsing (single-route support),
// so StandardGet is called with an empty ID which returns 404.
func TestAction_MissingURLParam(t *testing.T) {
	cleanTable(t)

	actionFn := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *TestUser, payload []byte) (*TestUser, error) {
		return item, nil
	}

	badMeta := &metadata.TypeMetadata{
		TypeID:       "test_user_id",
		TypeName:     "TestUser",
		TableName:    "users",
		URLParamUUID: "",
		ModelType:    userMeta.ModelType,
	}

	r := chi.NewRouter()
	r.Route("/users/{id}", func(r chi.Router) {
		r.Use(withMeta(badMeta))
		r.Post("/cancel", handler.Action[TestUser](actionFn))
	})

	req := httptest.NewRequest("POST", "/users/1/cancel", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestAction_HandlerError tests action handler returning error
func TestAction_HandlerError(t *testing.T) {
	cleanTable(t)

	// Create a test user
	db := testDB.GetDB()
	user := &TestUser{Name: "Test User", Email: "test@example.com"}
	_, err := db.NewInsert().Model(user).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test user:", err)
	}

	// Action handler that returns an error
	actionFn := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *TestUser, payload []byte) (*TestUser, error) {
		return nil, &testError{msg: "action failed"}
	}

	r := chi.NewRouter()
	r.Route("/users/{id}", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Post("/fail", handler.Action[TestUser](actionFn))
	})

	req := httptest.NewRequest("POST", "/users/1/fail", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d: %s", w.Code, w.Body.String())
	}
}

// TestAction_WithAuth tests action with auth info available
func TestAction_WithAuth(t *testing.T) {
	cleanTable(t)

	// Create a test user
	db := testDB.GetDB()
	user := &TestUser{Name: "Test User", Email: "test@example.com"}
	_, err := db.NewInsert().Model(user).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test user:", err)
	}

	var capturedAuth *metadata.AuthInfo

	// Action handler that captures auth info
	//nolint:unparam // test always returns nil error
	actionFn := func(_ context.Context, _ *service.Common[TestUser], _ *metadata.TypeMetadata, auth *metadata.AuthInfo, _ string, item *TestUser, _ []byte) (*TestUser, error) {
		capturedAuth = auth
		return item, nil
	}

	// Middleware to inject auth
	withAuth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := &metadata.AuthInfo{UserID: "user123", Scopes: []string{"admin"}}
			ctx := context.WithValue(r.Context(), metadata.AuthInfoKey, auth)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	r := chi.NewRouter()
	r.Route("/users/{id}", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Use(withAuth)
		r.Post("/check", handler.Action[TestUser](actionFn))
	})

	req := httptest.NewRequest("POST", "/users/1/check", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	if capturedAuth == nil {
		t.Error("Expected auth info to be captured")
	} else {
		if capturedAuth.UserID != "user123" {
			t.Errorf("Expected UserID 'user123', got %q", capturedAuth.UserID)
		}
		if len(capturedAuth.Scopes) != 1 || capturedAuth.Scopes[0] != "admin" {
			t.Errorf("Expected scopes ['admin'], got %v", capturedAuth.Scopes)
		}
	}
}

// TestAction_EmptyPayload tests action with empty request body
func TestAction_EmptyPayload(t *testing.T) {
	cleanTable(t)

	// Create a test user
	db := testDB.GetDB()
	user := &TestUser{Name: "Test User", Email: "test@example.com"}
	_, err := db.NewInsert().Model(user).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test user:", err)
	}

	var capturedPayload []byte

	// Action handler that captures payload
	//nolint:unparam // test always returns nil error
	actionFn := func(_ context.Context, _ *service.Common[TestUser], _ *metadata.TypeMetadata, _ *metadata.AuthInfo, _ string, item *TestUser, payload []byte) (*TestUser, error) {
		capturedPayload = payload
		return item, nil
	}

	r := chi.NewRouter()
	r.Route("/users/{id}", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Post("/trigger", handler.Action[TestUser](actionFn))
	})

	req := httptest.NewRequest("POST", "/users/1/trigger", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	if len(capturedPayload) != 0 {
		t.Errorf("Expected empty payload, got %q", string(capturedPayload))
	}
}

// testError is a simple error type for testing
type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}

// failReader is an io.Reader that always returns an error
type failReader struct{}

func (f *failReader) Read([]byte) (int, error) {
	return 0, &testError{msg: "read failed"}
}

// TestAction_NoMetadata tests action without metadata in context
func TestAction_NoMetadata(t *testing.T) {
	cleanTable(t)

	actionFn := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *TestUser, payload []byte) (*TestUser, error) {
		return item, nil
	}

	r := chi.NewRouter()
	// No withMeta middleware - metadata missing from context
	r.Post("/users/{id}/action", handler.Action[TestUser](actionFn))

	req := httptest.NewRequest("POST", "/users/1/action", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d: %s", w.Code, w.Body.String())
	}
}
