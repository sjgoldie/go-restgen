//nolint:dupl,errcheck,gosec,goconst,staticcheck // Test code - duplicate test patterns, unchecked test cleanup, repeated test strings, and string context keys are acceptable
package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/router"
	"github.com/sjgoldie/go-restgen/service"
)

// FuncTestOrder is a test model for func tests
type FuncTestOrder struct {
	bun.BaseModel `bun:"table:func_test_orders"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	Status        string    `bun:"status,notnull" json:"status"`
	OwnerID       string    `bun:"owner_id,notnull" json:"owner_id"`
	CreatedAt     time.Time `bun:"created_at,notnull,default:current_timestamp" json:"created_at,omitempty"`
}

// FuncTestStatus is a different return type
type FuncTestStatus struct {
	OrderID string `json:"order_id"`
	State   string `json:"state"`
	Steps   int    `json:"steps"`
}

// HealthResponse is a root func return type
type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

func setupFuncTestTable(t *testing.T) {
	t.Helper()

	ds, err := datastore.Get()
	if err != nil {
		t.Fatal("Failed to get datastore:", err)
	}

	db := ds.GetDB()
	ctx := context.Background()

	_, err = db.NewCreateTable().Model((*FuncTestOrder)(nil)).IfNotExists().Exec(ctx)
	if err != nil {
		t.Fatal("Failed to create table:", err)
	}
}

func cleanFuncTestTable(t *testing.T) {
	t.Helper()

	ds, err := datastore.Get()
	if err != nil {
		t.Fatal("Failed to get datastore:", err)
	}

	db := ds.GetDB()
	ctx := context.Background()

	_, _ = db.NewDelete().Model((*FuncTestOrder)(nil)).Where("1=1").Exec(ctx)
	_, _ = db.Exec("DELETE FROM sqlite_sequence WHERE name = 'func_test_orders'")
}

// getOrderStatus is a test func handler that returns a different type
func getOrderStatus(ctx context.Context, svc *service.Common[FuncTestOrder], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *FuncTestOrder, payload []byte) (any, int, error) {
	return &FuncTestStatus{
		OrderID: id,
		State:   item.Status,
		Steps:   3,
	}, http.StatusOK, nil
}

func TestWithFunc_Registration(t *testing.T) {
	setupFuncTestTable(t)
	cleanFuncTestTable(t)

	ds, _ := datastore.Get()
	order := &FuncTestOrder{Status: "pending", OwnerID: "user1"}
	_, err := ds.GetDB().NewInsert().Model(order).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test order:", err)
	}

	r := chi.NewRouter()
	b := router.NewBuilder(r, testDB(t))

	router.RegisterRoutes[FuncTestOrder](b, "/orders",
		router.AllPublic(),
		router.WithFunc("GET", "status", getOrderStatus, router.AuthConfig{
			Scopes: []string{router.ScopePublic},
		}),
	)

	req := httptest.NewRequest("GET", "/orders/1/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var result FuncTestStatus
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal("Failed to unmarshal response:", err)
	}

	if result.State != "pending" {
		t.Errorf("Expected state 'pending', got %q", result.State)
	}
	if result.Steps != 3 {
		t.Errorf("Expected steps 3, got %d", result.Steps)
	}
}

func TestWithFunc_DifferentHTTPMethods(t *testing.T) {
	setupFuncTestTable(t)
	cleanFuncTestTable(t)

	ds, _ := datastore.Get()
	order := &FuncTestOrder{Status: "pending", OwnerID: "user1"}
	_, err := ds.GetDB().NewInsert().Model(order).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test order:", err)
	}

	postFunc := func(ctx context.Context, svc *service.Common[FuncTestOrder], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *FuncTestOrder, payload []byte) (any, int, error) {
		return &FuncTestStatus{State: "triggered"}, http.StatusCreated, nil
	}

	r := chi.NewRouter()
	b := router.NewBuilder(r, testDB(t))

	router.RegisterRoutes[FuncTestOrder](b, "/orders",
		router.AllPublic(),
		router.WithFunc("GET", "status", getOrderStatus, router.AuthConfig{
			Scopes: []string{router.ScopePublic},
		}),
		router.WithFunc("POST", "trigger", postFunc, router.AuthConfig{
			Scopes: []string{router.ScopePublic},
		}),
	)

	// GET
	req1 := httptest.NewRequest("GET", "/orders/1/status", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Errorf("GET: Expected status 200, got %d: %s", w1.Code, w1.Body.String())
	}

	// POST
	req2 := httptest.NewRequest("POST", "/orders/1/trigger", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusCreated {
		t.Errorf("POST: Expected status 201, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestWithFunc_WithAuth(t *testing.T) {
	setupFuncTestTable(t)
	cleanFuncTestTable(t)

	ds, _ := datastore.Get()
	order := &FuncTestOrder{Status: "pending", OwnerID: "user1"}
	_, err := ds.GetDB().NewInsert().Model(order).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test order:", err)
	}

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Admin") == "true" {
				auth := &router.AuthInfo{UserID: "admin1", Scopes: []string{"admin"}}
				ctx := context.WithValue(r.Context(), router.AuthInfoKey, auth)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	b := router.NewBuilder(r, testDB(t))

	router.RegisterRoutes[FuncTestOrder](b, "/orders",
		router.AllPublic(),
		router.WithFunc("GET", "status", getOrderStatus, router.AuthConfig{
			Scopes: []string{"admin"},
		}),
	)

	// Without auth — 401
	req1 := httptest.NewRequest("GET", "/orders/1/status", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	if w1.Code != http.StatusUnauthorized {
		t.Errorf("No auth: Expected status 401, got %d: %s", w1.Code, w1.Body.String())
	}

	// With auth — 200
	req2 := httptest.NewRequest("GET", "/orders/1/status", nil)
	req2.Header.Set("X-Admin", "true")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("With auth: Expected status 200, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestWithFunc_Forbidden(t *testing.T) {
	setupFuncTestTable(t)
	cleanFuncTestTable(t)

	ds, _ := datastore.Get()
	order := &FuncTestOrder{Status: "pending", OwnerID: "user1"}
	_, err := ds.GetDB().NewInsert().Model(order).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test order:", err)
	}

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := &router.AuthInfo{UserID: "user1", Scopes: []string{"user"}}
			ctx := context.WithValue(r.Context(), router.AuthInfoKey, auth)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})

	b := router.NewBuilder(r, testDB(t))

	router.RegisterRoutes[FuncTestOrder](b, "/orders",
		router.AllPublic(),
		router.WithFunc("GET", "status", getOrderStatus, router.AuthConfig{
			Scopes: []string{"admin"},
		}),
	)

	req := httptest.NewRequest("GET", "/orders/1/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWithFunc_CRUDStillWorks(t *testing.T) {
	setupFuncTestTable(t)
	cleanFuncTestTable(t)

	r := chi.NewRouter()
	b := router.NewBuilder(r, testDB(t))

	router.RegisterRoutes[FuncTestOrder](b, "/orders",
		router.AllPublic(),
		router.WithFunc("GET", "status", getOrderStatus, router.AuthConfig{
			Scopes: []string{router.ScopePublic},
		}),
	)

	// Create
	createBody := `{"status": "pending", "owner_id": "user1"}`
	createReq := httptest.NewRequest("POST", "/orders", strings.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	r.ServeHTTP(createW, createReq)

	if createW.Code != http.StatusCreated {
		t.Errorf("Create: Expected status 201, got %d: %s", createW.Code, createW.Body.String())
	}

	// Get
	getReq := httptest.NewRequest("GET", "/orders/1", nil)
	getW := httptest.NewRecorder()
	r.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Errorf("Get: Expected status 200, got %d: %s", getW.Code, getW.Body.String())
	}

	// Func
	funcReq := httptest.NewRequest("GET", "/orders/1/status", nil)
	funcW := httptest.NewRecorder()
	r.ServeHTTP(funcW, funcReq)

	if funcW.Code != http.StatusOK {
		t.Errorf("Func: Expected status 200, got %d: %s", funcW.Code, funcW.Body.String())
	}
}

func TestWithFunc_NotFound(t *testing.T) {
	setupFuncTestTable(t)
	cleanFuncTestTable(t)

	r := chi.NewRouter()
	b := router.NewBuilder(r, testDB(t))

	router.RegisterRoutes[FuncTestOrder](b, "/orders",
		router.AllPublic(),
		router.WithFunc("GET", "status", getOrderStatus, router.AuthConfig{
			Scopes: []string{router.ScopePublic},
		}),
	)

	req := httptest.NewRequest("GET", "/orders/999/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWithFuncConfig_TypeAndName(t *testing.T) {
	config := router.WithFunc("GET", "status", getOrderStatus, router.AuthConfig{
		Scopes: []string{"user"},
	})

	if config.Name != "status" {
		t.Errorf("Expected name 'status', got %q", config.Name)
	}

	if config.Method != "GET" {
		t.Errorf("Expected method 'GET', got %q", config.Method)
	}

	if config.Fn == nil {
		t.Error("Expected function to be set")
	}

	if len(config.Auth.Scopes) != 1 || config.Auth.Scopes[0] != "user" {
		t.Errorf("Expected scopes ['user'], got %v", config.Auth.Scopes)
	}
}

func TestRegisterRootFunc_Success(t *testing.T) {
	healthFn := func(ctx context.Context, auth *metadata.AuthInfo, r *http.Request) (any, int, error) {
		return &HealthResponse{Status: "healthy", Version: "1.0"}, http.StatusOK, nil
	}

	r := chi.NewRouter()
	b := router.NewBuilder(r, testDB(t))

	router.RegisterRootFunc(b, "GET", "/health", healthFn, router.AllPublic())

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var result HealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal("Failed to unmarshal response:", err)
	}

	if result.Status != "healthy" {
		t.Errorf("Expected status 'healthy', got %q", result.Status)
	}
}

func TestRegisterRootFunc_WithAuth(t *testing.T) {
	webhookFn := func(ctx context.Context, auth *metadata.AuthInfo, r *http.Request) (any, int, error) {
		return &HealthResponse{Status: "received"}, http.StatusOK, nil
	}

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Admin") == "true" {
				auth := &router.AuthInfo{UserID: "admin1", Scopes: []string{"admin"}}
				ctx := context.WithValue(r.Context(), router.AuthInfoKey, auth)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	b := router.NewBuilder(r, testDB(t))
	router.RegisterRootFunc(b, "POST", "/webhooks/stripe", webhookFn, router.AuthConfig{
		Methods: []string{router.MethodAll},
		Scopes:  []string{"admin"},
	})

	// Without auth — 401
	req1 := httptest.NewRequest("POST", "/webhooks/stripe", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	if w1.Code != http.StatusUnauthorized {
		t.Errorf("No auth: Expected status 401, got %d: %s", w1.Code, w1.Body.String())
	}

	// With auth — 200
	req2 := httptest.NewRequest("POST", "/webhooks/stripe", nil)
	req2.Header.Set("X-Admin", "true")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("With auth: Expected status 200, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestWithFunc_WithOwnership(t *testing.T) {
	setupFuncTestTable(t)
	cleanFuncTestTable(t)

	ds, _ := datastore.Get()
	order1 := &FuncTestOrder{Status: "pending", OwnerID: "user1"}
	order2 := &FuncTestOrder{Status: "pending", OwnerID: "user2"}
	_, err := ds.GetDB().NewInsert().Model(order1).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test order 1:", err)
	}
	_, err = ds.GetDB().NewInsert().Model(order2).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test order 2:", err)
	}

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := &router.AuthInfo{UserID: "user1", Scopes: []string{"user"}}
			ctx := context.WithValue(r.Context(), router.AuthInfoKey, auth)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})

	b := router.NewBuilder(r, testDB(t))

	router.RegisterRoutes[FuncTestOrder](b, "/orders",
		router.AuthConfig{
			Methods: []string{router.MethodAll},
			Scopes:  []string{"user"},
			Ownership: &router.OwnershipConfig{
				Fields: []string{"OwnerID"},
			},
		},
		router.WithFunc("GET", "status", getOrderStatus, router.AuthConfig{
			Scopes: []string{"user"},
			Ownership: &router.OwnershipConfig{
				Fields: []string{"OwnerID"},
			},
		}),
	)

	// Own order — 200
	req1 := httptest.NewRequest("GET", "/orders/1/status", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Errorf("Own order: Expected status 200, got %d: %s", w1.Code, w1.Body.String())
	}

	// Other user's order — 404 (ownership hides existence)
	req2 := httptest.NewRequest("GET", "/orders/2/status", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusNotFound {
		t.Errorf("Other's order: Expected status 404, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestWithFunc_BlockedByDefault(t *testing.T) {
	setupFuncTestTable(t)
	cleanFuncTestTable(t)

	ds, _ := datastore.Get()
	order := &FuncTestOrder{Status: "pending", OwnerID: "user1"}
	_, err := ds.GetDB().NewInsert().Model(order).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test order:", err)
	}

	r := chi.NewRouter()
	b := router.NewBuilder(r, testDB(t))

	router.RegisterRoutes[FuncTestOrder](b, "/orders",
		router.AllPublic(),
		router.WithFunc("GET", "status", getOrderStatus, router.AuthConfig{}),
	)

	req := httptest.NewRequest("GET", "/orders/1/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401 (blocked), got %d: %s", w.Code, w.Body.String())
	}
}

func TestRegisterRootFunc_Forbidden(t *testing.T) {
	webhookFn := func(ctx context.Context, auth *metadata.AuthInfo, r *http.Request) (any, int, error) {
		return &HealthResponse{Status: "received"}, http.StatusOK, nil
	}

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := &router.AuthInfo{UserID: "user1", Scopes: []string{"user"}}
			ctx := context.WithValue(r.Context(), router.AuthInfoKey, auth)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})

	b := router.NewBuilder(r, testDB(t))
	router.RegisterRootFunc(b, "POST", "/webhooks/stripe", webhookFn, router.AuthConfig{
		Methods: []string{router.MethodAll},
		Scopes:  []string{"admin"},
	})

	req := httptest.NewRequest("POST", "/webhooks/stripe", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRegisterRootFunc_NoContent(t *testing.T) {
	webhookFn := func(ctx context.Context, auth *metadata.AuthInfo, r *http.Request) (any, int, error) {
		return nil, 0, nil
	}

	r := chi.NewRouter()
	b := router.NewBuilder(r, testDB(t))

	router.RegisterRootFunc(b, "POST", "/webhooks/test", webhookFn, router.AllPublic())

	req := httptest.NewRequest("POST", "/webhooks/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("Expected status 204, got %d: %s", w.Code, w.Body.String())
	}
}
