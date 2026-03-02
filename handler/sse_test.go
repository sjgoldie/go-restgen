//nolint:staticcheck,errcheck,gosec,unparam // Test code - string context keys, unchecked test cleanup, and unused params in handler signatures are acceptable
package handler_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sjgoldie/go-restgen/handler"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/service"
)

func TestSSE_StreamEvents(t *testing.T) {
	cleanTable(t)

	db := testDB.GetDB()
	user := &TestUser{Name: "Test User", Email: "test@example.com"}
	_, err := db.NewInsert().Model(user).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test user:", err)
	}

	sseFn := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *TestUser, events chan<- handler.SSEEvent) error {
		events <- handler.SSEEvent{Event: "status", Data: map[string]string{"state": "starting"}}
		events <- handler.SSEEvent{Event: "status", Data: map[string]string{"state": "running"}}
		events <- handler.SSEEvent{Event: "done", Data: map[string]string{"state": "complete"}}
		return nil
	}

	r := chi.NewRouter()
	r.Route("/users/{id}", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Get("/stream", handler.SSE[TestUser](sseFn))
	})

	req := httptest.NewRequest("GET", "/users/1/stream", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("Expected Content-Type 'text/event-stream', got %q", contentType)
	}

	cacheControl := w.Header().Get("Cache-Control")
	if cacheControl != "no-cache" {
		t.Errorf("Expected Cache-Control 'no-cache', got %q", cacheControl)
	}

	// Parse SSE events from response body
	events := parseSSEEvents(t, w.Body.String())
	if len(events) != 3 {
		t.Fatalf("Expected 3 events, got %d: %s", len(events), w.Body.String())
	}

	if events[0].eventType != "status" {
		t.Errorf("Expected first event type 'status', got %q", events[0].eventType)
	}
	if !strings.Contains(events[0].data, "starting") {
		t.Errorf("Expected first event data to contain 'starting', got %q", events[0].data)
	}
	if events[2].eventType != "done" {
		t.Errorf("Expected third event type 'done', got %q", events[2].eventType)
	}
}

func TestSSE_EventWithID(t *testing.T) {
	cleanTable(t)

	db := testDB.GetDB()
	user := &TestUser{Name: "Test User", Email: "test@example.com"}
	_, err := db.NewInsert().Model(user).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test user:", err)
	}

	sseFn := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *TestUser, events chan<- handler.SSEEvent) error {
		events <- handler.SSEEvent{Event: "update", Data: "hello", ID: "evt-1"}
		return nil
	}

	r := chi.NewRouter()
	r.Route("/users/{id}", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Get("/stream", handler.SSE[TestUser](sseFn))
	})

	req := httptest.NewRequest("GET", "/users/1/stream", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, "id: evt-1") {
		t.Errorf("Expected body to contain 'id: evt-1', got %q", body)
	}
	if !strings.Contains(body, "event: update") {
		t.Errorf("Expected body to contain 'event: update', got %q", body)
	}
}

func TestSSE_NotFound(t *testing.T) {
	cleanTable(t)

	sseFn := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *TestUser, events chan<- handler.SSEEvent) error {
		return nil
	}

	r := chi.NewRouter()
	r.Route("/users/{id}", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Get("/stream", handler.SSE[TestUser](sseFn))
	})

	req := httptest.NewRequest("GET", "/users/999/stream", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSSE_NoMetadata(t *testing.T) {
	sseFn := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *TestUser, events chan<- handler.SSEEvent) error {
		return nil
	}

	r := chi.NewRouter()
	r.Get("/users/{id}/stream", handler.SSE[TestUser](sseFn))

	req := httptest.NewRequest("GET", "/users/1/stream", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSSE_ClientDisconnect(t *testing.T) {
	cleanTable(t)

	db := testDB.GetDB()
	user := &TestUser{Name: "Test User", Email: "test@example.com"}
	_, err := db.NewInsert().Model(user).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test user:", err)
	}

	ctxCancelled := make(chan bool, 1)

	sseFn := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *TestUser, events chan<- handler.SSEEvent) error {
		events <- handler.SSEEvent{Event: "ping", Data: "hello"}
		<-ctx.Done()
		ctxCancelled <- true
		return ctx.Err()
	}

	r := chi.NewRouter()
	r.Route("/users/{id}", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Get("/stream", handler.SSE[TestUser](sseFn))
	})

	// Use a cancellable context to simulate client disconnect
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("GET", "/users/1/stream", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		r.ServeHTTP(w, req)
		close(done)
	}()

	// Cancel the context to simulate disconnect
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Handler did not finish after context cancellation")
	}

	select {
	case cancelled := <-ctxCancelled:
		if !cancelled {
			t.Error("Expected context to be cancelled")
		}
	case <-time.After(time.Second):
		t.Error("SSE func did not detect context cancellation")
	}
}

func TestRootSSE_Success(t *testing.T) {
	sseFn := func(ctx context.Context, auth *metadata.AuthInfo, r *http.Request, events chan<- handler.SSEEvent) error {
		events <- handler.SSEEvent{Event: "system", Data: map[string]string{"msg": "hello"}}
		events <- handler.SSEEvent{Event: "system", Data: map[string]string{"msg": "world"}}
		return nil
	}

	r := chi.NewRouter()
	r.Get("/events", handler.RootSSE(sseFn))

	req := httptest.NewRequest("GET", "/events", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("Expected Content-Type 'text/event-stream', got %q", contentType)
	}

	events := parseSSEEvents(t, w.Body.String())
	if len(events) != 2 {
		t.Fatalf("Expected 2 events, got %d: %s", len(events), w.Body.String())
	}
}

func TestRootSSE_WithAuth(t *testing.T) {
	var capturedAuth *metadata.AuthInfo

	sseFn := func(ctx context.Context, auth *metadata.AuthInfo, r *http.Request, events chan<- handler.SSEEvent) error {
		capturedAuth = auth
		events <- handler.SSEEvent{Data: "ok"}
		return nil
	}

	withAuth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := &metadata.AuthInfo{UserID: "user1", Scopes: []string{"events"}}
			ctx := context.WithValue(r.Context(), metadata.AuthInfoKey, auth)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	r := chi.NewRouter()
	r.Use(withAuth)
	r.Get("/events", handler.RootSSE(sseFn))

	req := httptest.NewRequest("GET", "/events", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	if capturedAuth == nil {
		t.Error("Expected auth info to be captured")
	} else if capturedAuth.UserID != "user1" {
		t.Errorf("Expected UserID 'user1', got %q", capturedAuth.UserID)
	}
}

// sseEvent represents a parsed SSE event for test assertions
type sseEvent struct {
	eventType string
	data      string
	id        string
}

// parseSSEEvents parses SSE events from a response body string
func parseSSEEvents(t *testing.T, body string) []sseEvent {
	t.Helper()

	var events []sseEvent
	var current sseEvent
	hasData := false

	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			if hasData {
				events = append(events, current)
				current = sseEvent{}
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
