//nolint:dupl,errcheck,gosec,goconst,staticcheck // Test code - duplicate test patterns, unchecked test cleanup, repeated test strings, and string context keys are acceptable
package router_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/handler"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/router"
	"github.com/sjgoldie/go-restgen/service"
)

func TestWithSSE_Registration(t *testing.T) {
	setupFuncTestTable(t)
	cleanFuncTestTable(t)

	ds, _ := datastore.Get()
	order := &FuncTestOrder{Status: "pending", OwnerID: "user1"}
	_, err := ds.GetDB().NewInsert().Model(order).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test order:", err)
	}

	sseFn := func(ctx context.Context, svc *service.Common[FuncTestOrder], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *FuncTestOrder, events chan<- handler.SSEEvent) error {
		events <- handler.SSEEvent{Event: "status", Data: map[string]string{"state": item.Status}}
		return nil
	}

	r := chi.NewRouter()
	b := router.NewBuilder(r, testDB(t))

	router.RegisterRoutes[FuncTestOrder](b, "/orders",
		router.AllPublic(),
		router.WithSSE("stream", sseFn, router.AuthConfig{
			Scopes: []string{router.ScopePublic},
		}),
	)

	req := httptest.NewRequest("GET", "/orders/1/stream", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("Expected Content-Type 'text/event-stream', got %q", contentType)
	}

	events := parseSSEEventsRouter(t, w.Body.String())
	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d: %s", len(events), w.Body.String())
	}

	if events[0].eventType != "status" {
		t.Errorf("Expected event type 'status', got %q", events[0].eventType)
	}
}

func TestWithSSE_WithAuth(t *testing.T) {
	setupFuncTestTable(t)
	cleanFuncTestTable(t)

	ds, _ := datastore.Get()
	order := &FuncTestOrder{Status: "pending", OwnerID: "user1"}
	_, err := ds.GetDB().NewInsert().Model(order).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test order:", err)
	}

	sseFn := func(ctx context.Context, svc *service.Common[FuncTestOrder], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *FuncTestOrder, events chan<- handler.SSEEvent) error {
		events <- handler.SSEEvent{Data: "ok"}
		return nil
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
		router.WithSSE("stream", sseFn, router.AuthConfig{
			Scopes: []string{"admin"},
		}),
	)

	// Without auth — 401
	req1 := httptest.NewRequest("GET", "/orders/1/stream", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	if w1.Code != http.StatusUnauthorized {
		t.Errorf("No auth: Expected status 401, got %d: %s", w1.Code, w1.Body.String())
	}

	// With auth — 200
	req2 := httptest.NewRequest("GET", "/orders/1/stream", nil)
	req2.Header.Set("X-Admin", "true")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("With auth: Expected status 200, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestWithSSE_Forbidden(t *testing.T) {
	setupFuncTestTable(t)
	cleanFuncTestTable(t)

	ds, _ := datastore.Get()
	order := &FuncTestOrder{Status: "pending", OwnerID: "user1"}
	_, err := ds.GetDB().NewInsert().Model(order).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test order:", err)
	}

	sseFn := func(ctx context.Context, svc *service.Common[FuncTestOrder], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *FuncTestOrder, events chan<- handler.SSEEvent) error {
		events <- handler.SSEEvent{Data: "ok"}
		return nil
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
		router.WithSSE("stream", sseFn, router.AuthConfig{
			Scopes: []string{"admin"},
		}),
	)

	req := httptest.NewRequest("GET", "/orders/1/stream", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWithSSE_NotFound(t *testing.T) {
	setupFuncTestTable(t)
	cleanFuncTestTable(t)

	sseFn := func(ctx context.Context, svc *service.Common[FuncTestOrder], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *FuncTestOrder, events chan<- handler.SSEEvent) error {
		events <- handler.SSEEvent{Data: "ok"}
		return nil
	}

	r := chi.NewRouter()
	b := router.NewBuilder(r, testDB(t))

	router.RegisterRoutes[FuncTestOrder](b, "/orders",
		router.AllPublic(),
		router.WithSSE("stream", sseFn, router.AuthConfig{
			Scopes: []string{router.ScopePublic},
		}),
	)

	req := httptest.NewRequest("GET", "/orders/999/stream", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWithSSE_WithOwnership(t *testing.T) {
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

	sseFn := func(ctx context.Context, svc *service.Common[FuncTestOrder], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *FuncTestOrder, events chan<- handler.SSEEvent) error {
		events <- handler.SSEEvent{Data: map[string]string{"status": item.Status}}
		return nil
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
		router.WithSSE("stream", sseFn, router.AuthConfig{
			Scopes: []string{"user"},
			Ownership: &router.OwnershipConfig{
				Fields: []string{"OwnerID"},
			},
		}),
	)

	// Own order — 200
	req1 := httptest.NewRequest("GET", "/orders/1/stream", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Errorf("Own order: Expected status 200, got %d: %s", w1.Code, w1.Body.String())
	}

	// Other user's order — 404 (ownership hides existence)
	req2 := httptest.NewRequest("GET", "/orders/2/stream", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusNotFound {
		t.Errorf("Other's order: Expected status 404, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestWithSSE_BlockedByDefault(t *testing.T) {
	setupFuncTestTable(t)
	cleanFuncTestTable(t)

	ds, _ := datastore.Get()
	order := &FuncTestOrder{Status: "pending", OwnerID: "user1"}
	_, err := ds.GetDB().NewInsert().Model(order).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test order:", err)
	}

	sseFn := func(ctx context.Context, svc *service.Common[FuncTestOrder], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *FuncTestOrder, events chan<- handler.SSEEvent) error {
		events <- handler.SSEEvent{Data: "ok"}
		return nil
	}

	r := chi.NewRouter()
	b := router.NewBuilder(r, testDB(t))

	router.RegisterRoutes[FuncTestOrder](b, "/orders",
		router.AllPublic(),
		router.WithSSE("stream", sseFn, router.AuthConfig{}),
	)

	req := httptest.NewRequest("GET", "/orders/1/stream", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401 (blocked), got %d: %s", w.Code, w.Body.String())
	}
}

func TestWithSSE_CRUDStillWorks(t *testing.T) {
	setupFuncTestTable(t)
	cleanFuncTestTable(t)

	sseFn := func(ctx context.Context, svc *service.Common[FuncTestOrder], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *FuncTestOrder, events chan<- handler.SSEEvent) error {
		events <- handler.SSEEvent{Data: "ok"}
		return nil
	}

	r := chi.NewRouter()
	b := router.NewBuilder(r, testDB(t))

	router.RegisterRoutes[FuncTestOrder](b, "/orders",
		router.AllPublic(),
		router.WithSSE("stream", sseFn, router.AuthConfig{
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
}

func TestRegisterRootSSE_Success(t *testing.T) {
	sseFn := func(ctx context.Context, auth *metadata.AuthInfo, r *http.Request, events chan<- handler.SSEEvent) error {
		events <- handler.SSEEvent{Event: "system", Data: map[string]string{"msg": "hello"}}
		events <- handler.SSEEvent{Event: "system", Data: map[string]string{"msg": "world"}}
		return nil
	}

	r := chi.NewRouter()
	b := router.NewBuilder(r, testDB(t))

	router.RegisterRootSSE(b, "/events/system", sseFn, router.AllPublic())

	req := httptest.NewRequest("GET", "/events/system", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("Expected Content-Type 'text/event-stream', got %q", contentType)
	}

	events := parseSSEEventsRouter(t, w.Body.String())
	if len(events) != 2 {
		t.Fatalf("Expected 2 events, got %d: %s", len(events), w.Body.String())
	}
}

func TestRegisterRootSSE_WithAuth(t *testing.T) {
	sseFn := func(ctx context.Context, auth *metadata.AuthInfo, r *http.Request, events chan<- handler.SSEEvent) error {
		events <- handler.SSEEvent{Data: "ok"}
		return nil
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
	router.RegisterRootSSE(b, "/events/system", sseFn, router.AuthConfig{
		Methods: []string{router.MethodAll},
		Scopes:  []string{"admin"},
	})

	// Without auth — 401
	req1 := httptest.NewRequest("GET", "/events/system", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	if w1.Code != http.StatusUnauthorized {
		t.Errorf("No auth: Expected status 401, got %d: %s", w1.Code, w1.Body.String())
	}

	// With auth — 200
	req2 := httptest.NewRequest("GET", "/events/system", nil)
	req2.Header.Set("X-Admin", "true")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("With auth: Expected status 200, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestRegisterRootSSE_Forbidden(t *testing.T) {
	sseFn := func(ctx context.Context, auth *metadata.AuthInfo, r *http.Request, events chan<- handler.SSEEvent) error {
		events <- handler.SSEEvent{Data: "ok"}
		return nil
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
	router.RegisterRootSSE(b, "/events/system", sseFn, router.AuthConfig{
		Methods: []string{router.MethodAll},
		Scopes:  []string{"admin"},
	})

	req := httptest.NewRequest("GET", "/events/system", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d: %s", w.Code, w.Body.String())
	}
}

// sseEventRouter represents a parsed SSE event for test assertions
type sseEventRouter struct {
	eventType string
	data      string
	id        string
}

// parseSSEEventsRouter parses SSE events from a response body string
func parseSSEEventsRouter(t *testing.T, body string) []sseEventRouter {
	t.Helper()

	var events []sseEventRouter
	var current sseEventRouter
	hasData := false

	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			if hasData {
				events = append(events, current)
				current = sseEventRouter{}
				hasData = false
			}
			continue
		}

		switch {
		case strings.HasPrefix(line, "event: "):
			current.eventType = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			current.data = strings.TrimPrefix(line, "data: ")
			hasData = true
		case strings.HasPrefix(line, "id: "):
			current.id = strings.TrimPrefix(line, "id: ")
		}
	}

	if hasData {
		events = append(events, current)
	}

	return events
}
