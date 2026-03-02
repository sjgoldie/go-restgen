//nolint:staticcheck,errcheck,gosec,unparam // Test code - string context keys, unchecked test cleanup, and unused params in handler signatures are acceptable
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

// WorkflowStatus is a different return type for func handler tests
type WorkflowStatus struct {
	OrgID  string `json:"org_id"`
	Status string `json:"status"`
	Steps  int    `json:"steps"`
}

func TestFunc_Success(t *testing.T) {
	cleanTable(t)

	db := testDB.GetDB()
	user := &TestUser{Name: "Test User", Email: "test@example.com"}
	_, err := db.NewInsert().Model(user).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test user:", err)
	}

	funcHandler := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *TestUser, payload []byte) (any, int, error) {
		return &WorkflowStatus{OrgID: id, Status: "active", Steps: 5}, http.StatusOK, nil
	}

	r := chi.NewRouter()
	r.Route("/users/{id}", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Get("/wf-status", handler.Func[TestUser](funcHandler))
	})

	req := httptest.NewRequest("GET", "/users/1/wf-status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var result WorkflowStatus
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal("Failed to unmarshal response:", err)
	}

	if result.Status != "active" {
		t.Errorf("Expected status 'active', got %q", result.Status)
	}
	if result.Steps != 5 {
		t.Errorf("Expected steps 5, got %d", result.Steps)
	}
	if result.OrgID != "1" {
		t.Errorf("Expected org_id '1', got %q", result.OrgID)
	}
}

func TestFunc_NoContent(t *testing.T) {
	cleanTable(t)

	db := testDB.GetDB()
	user := &TestUser{Name: "Test User", Email: "test@example.com"}
	_, err := db.NewInsert().Model(user).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test user:", err)
	}

	funcHandler := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *TestUser, payload []byte) (any, int, error) {
		return nil, 0, nil
	}

	r := chi.NewRouter()
	r.Route("/users/{id}", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Post("/trigger", handler.Func[TestUser](funcHandler))
	})

	req := httptest.NewRequest("POST", "/users/1/trigger", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("Expected status 204, got %d: %s", w.Code, w.Body.String())
	}

	if w.Body.Len() > 0 {
		t.Errorf("Expected empty body, got %q", w.Body.String())
	}
}

func TestFunc_CustomStatusCode(t *testing.T) {
	cleanTable(t)

	db := testDB.GetDB()
	user := &TestUser{Name: "Test User", Email: "test@example.com"}
	_, err := db.NewInsert().Model(user).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test user:", err)
	}

	funcHandler := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *TestUser, payload []byte) (any, int, error) {
		return &WorkflowStatus{Status: "created"}, http.StatusCreated, nil
	}

	r := chi.NewRouter()
	r.Route("/users/{id}", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Post("/create-wf", handler.Func[TestUser](funcHandler))
	})

	req := httptest.NewRequest("POST", "/users/1/create-wf", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestFunc_DefaultStatusCode(t *testing.T) {
	cleanTable(t)

	db := testDB.GetDB()
	user := &TestUser{Name: "Test User", Email: "test@example.com"}
	_, err := db.NewInsert().Model(user).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test user:", err)
	}

	funcHandler := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *TestUser, payload []byte) (any, int, error) {
		return &WorkflowStatus{Status: "ok"}, 0, nil
	}

	r := chi.NewRouter()
	r.Route("/users/{id}", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Get("/status", handler.Func[TestUser](funcHandler))
	})

	req := httptest.NewRequest("GET", "/users/1/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 (default), got %d: %s", w.Code, w.Body.String())
	}
}

func TestFunc_NotFound(t *testing.T) {
	cleanTable(t)

	funcHandler := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *TestUser, payload []byte) (any, int, error) {
		return item, http.StatusOK, nil
	}

	r := chi.NewRouter()
	r.Route("/users/{id}", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Get("/status", handler.Func[TestUser](funcHandler))
	})

	req := httptest.NewRequest("GET", "/users/999/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestFunc_WithPayload(t *testing.T) {
	cleanTable(t)

	db := testDB.GetDB()
	user := &TestUser{Name: "Test User", Email: "test@example.com"}
	_, err := db.NewInsert().Model(user).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test user:", err)
	}

	var capturedPayload []byte

	funcHandler := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *TestUser, payload []byte) (any, int, error) {
		capturedPayload = payload
		return &WorkflowStatus{Status: "ok"}, http.StatusOK, nil
	}

	r := chi.NewRouter()
	r.Route("/users/{id}", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Post("/trigger", handler.Func[TestUser](funcHandler))
	})

	body := `{"action": "start"}`
	req := httptest.NewRequest("POST", "/users/1/trigger", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	if string(capturedPayload) != body {
		t.Errorf("Expected payload %q, got %q", body, string(capturedPayload))
	}
}

func TestFunc_WithAuth(t *testing.T) {
	cleanTable(t)

	db := testDB.GetDB()
	user := &TestUser{Name: "Test User", Email: "test@example.com"}
	_, err := db.NewInsert().Model(user).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test user:", err)
	}

	var capturedAuth *metadata.AuthInfo

	//nolint:unparam
	funcHandler := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *TestUser, payload []byte) (any, int, error) {
		capturedAuth = auth
		return &WorkflowStatus{Status: "ok"}, http.StatusOK, nil
	}

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
		r.Get("/status", handler.Func[TestUser](funcHandler))
	})

	req := httptest.NewRequest("GET", "/users/1/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	if capturedAuth == nil {
		t.Error("Expected auth info to be captured")
	} else if capturedAuth.UserID != "user123" {
		t.Errorf("Expected UserID 'user123', got %q", capturedAuth.UserID)
	}
}

func TestFunc_HandlerError(t *testing.T) {
	cleanTable(t)

	db := testDB.GetDB()
	user := &TestUser{Name: "Test User", Email: "test@example.com"}
	_, err := db.NewInsert().Model(user).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test user:", err)
	}

	funcHandler := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *TestUser, payload []byte) (any, int, error) {
		return nil, 0, &testError{msg: "something went wrong"}
	}

	r := chi.NewRouter()
	r.Route("/users/{id}", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Get("/status", handler.Func[TestUser](funcHandler))
	})

	req := httptest.NewRequest("GET", "/users/1/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestFunc_NoMetadata(t *testing.T) {
	cleanTable(t)

	funcHandler := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *TestUser, payload []byte) (any, int, error) {
		return nil, http.StatusOK, nil
	}

	r := chi.NewRouter()
	r.Get("/users/{id}/status", handler.Func[TestUser](funcHandler))

	req := httptest.NewRequest("GET", "/users/1/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestFunc_ItemPassedThrough(t *testing.T) {
	cleanTable(t)

	db := testDB.GetDB()
	user := &TestUser{Name: "Alice", Email: "alice@example.com"}
	_, err := db.NewInsert().Model(user).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test user:", err)
	}

	var capturedItem *TestUser

	//nolint:unparam
	funcHandler := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *TestUser, payload []byte) (any, int, error) {
		capturedItem = item
		return &WorkflowStatus{Status: "ok"}, http.StatusOK, nil
	}

	r := chi.NewRouter()
	r.Route("/users/{id}", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Get("/status", handler.Func[TestUser](funcHandler))
	})

	req := httptest.NewRequest("GET", "/users/1/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	if capturedItem == nil {
		t.Fatal("Expected item to be captured")
	}
	if capturedItem.Name != "Alice" {
		t.Errorf("Expected name 'Alice', got %q", capturedItem.Name)
	}
}

func TestRootFunc_Success(t *testing.T) {
	funcHandler := func(ctx context.Context, auth *metadata.AuthInfo, r *http.Request) (any, int, error) {
		return &WorkflowStatus{Status: "healthy", Steps: 0}, http.StatusOK, nil
	}

	r := chi.NewRouter()
	r.Get("/health", handler.RootFunc(funcHandler))

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var result WorkflowStatus
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal("Failed to unmarshal response:", err)
	}

	if result.Status != "healthy" {
		t.Errorf("Expected status 'healthy', got %q", result.Status)
	}
}

func TestRootFunc_NoContent(t *testing.T) {
	funcHandler := func(ctx context.Context, auth *metadata.AuthInfo, r *http.Request) (any, int, error) {
		return nil, 0, nil
	}

	r := chi.NewRouter()
	r.Post("/webhook", handler.RootFunc(funcHandler))

	req := httptest.NewRequest("POST", "/webhook", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("Expected status 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRootFunc_WithAuth(t *testing.T) {
	var capturedAuth *metadata.AuthInfo

	funcHandler := func(ctx context.Context, auth *metadata.AuthInfo, r *http.Request) (any, int, error) {
		capturedAuth = auth
		return &WorkflowStatus{Status: "ok"}, http.StatusOK, nil
	}

	withAuth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := &metadata.AuthInfo{UserID: "admin1", Scopes: []string{"admin"}}
			ctx := context.WithValue(r.Context(), metadata.AuthInfoKey, auth)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	r := chi.NewRouter()
	r.Use(withAuth)
	r.Get("/health", handler.RootFunc(funcHandler))

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	if capturedAuth == nil {
		t.Error("Expected auth info to be captured")
	} else if capturedAuth.UserID != "admin1" {
		t.Errorf("Expected UserID 'admin1', got %q", capturedAuth.UserID)
	}
}

func TestRootFunc_HandlerError(t *testing.T) {
	funcHandler := func(ctx context.Context, auth *metadata.AuthInfo, r *http.Request) (any, int, error) {
		return nil, 0, &testError{msg: "webhook failed"}
	}

	r := chi.NewRouter()
	r.Post("/webhook", handler.RootFunc(funcHandler))

	req := httptest.NewRequest("POST", "/webhook", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRootFunc_RequestPassedThrough(t *testing.T) {
	var capturedHeader string

	funcHandler := func(ctx context.Context, auth *metadata.AuthInfo, r *http.Request) (any, int, error) {
		capturedHeader = r.Header.Get("X-Webhook-Signature")
		return &WorkflowStatus{Status: "verified"}, http.StatusOK, nil
	}

	r := chi.NewRouter()
	r.Post("/webhook", handler.RootFunc(funcHandler))

	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(`{"event":"test"}`))
	req.Header.Set("X-Webhook-Signature", "sha256=abc123")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	if capturedHeader != "sha256=abc123" {
		t.Errorf("Expected header 'sha256=abc123', got %q", capturedHeader)
	}
}
