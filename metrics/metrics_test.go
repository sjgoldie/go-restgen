package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/metric/noop"
)

func TestInitialize(t *testing.T) {
	// Test with nil provider (uses global)
	err := Initialize(nil)
	if err != nil {
		t.Errorf("Initialize(nil) returned error: %v", err)
	}

	// Verify instruments are set
	if requestDuration == nil {
		t.Error("requestDuration not initialized")
	}
	if requestCount == nil {
		t.Error("requestCount not initialized")
	}
}

func TestInitializeWithProvider(t *testing.T) {
	provider := noop.NewMeterProvider()
	err := Initialize(provider)
	if err != nil {
		t.Errorf("Initialize(provider) returned error: %v", err)
	}
}

func TestMiddleware(t *testing.T) {
	// Initialize with noop provider
	_ = Initialize(noop.NewMeterProvider())

	// Create test handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Wrap with middleware
	wrapped := Middleware()(handler)

	// Create request
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	// Execute
	wrapped.ServeHTTP(rec, req)

	// Verify response
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestMiddlewareWithDifferentStatusCodes(t *testing.T) {
	_ = Initialize(noop.NewMeterProvider())

	tests := []struct {
		name       string
		statusCode int
	}{
		{"OK", http.StatusOK},
		{"Created", http.StatusCreated},
		{"BadRequest", http.StatusBadRequest},
		{"NotFound", http.StatusNotFound},
		{"InternalServerError", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			})

			wrapped := Middleware()(handler)
			req := httptest.NewRequest("GET", "/test", nil)
			rec := httptest.NewRecorder()

			wrapped.ServeHTTP(rec, req)

			if rec.Code != tt.statusCode {
				t.Errorf("expected status %d, got %d", tt.statusCode, rec.Code)
			}
		})
	}
}

func TestResponseWriter(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: rec, statusCode: http.StatusOK}

	// Test WriteHeader
	rw.WriteHeader(http.StatusCreated)
	if rw.statusCode != http.StatusCreated {
		t.Errorf("expected status 201, got %d", rw.statusCode)
	}

	// Test Unwrap
	if rw.Unwrap() != rec {
		t.Error("Unwrap did not return underlying ResponseWriter")
	}
}

func TestStatusCode(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: rec, statusCode: http.StatusNotFound}

	code := StatusCode(rw)
	if code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", code)
	}

	// Test with non-wrapped writer
	code = StatusCode(rec)
	if code != 0 {
		t.Errorf("expected 0 for non-wrapped writer, got %d", code)
	}
}

func TestGetters(t *testing.T) {
	_ = Initialize(noop.NewMeterProvider())

	if GetRequestDuration() == nil {
		t.Error("GetRequestDuration returned nil")
	}
	if GetRequestCount() == nil {
		t.Error("GetRequestCount returned nil")
	}
}

func TestRecordCustom(t *testing.T) {
	_ = Initialize(noop.NewMeterProvider())

	// Should not panic
	RecordCustom(context.Background(), "TestResource", "POST", 201, 42.5)
}

func TestRecordCustomBeforeInitialize(t *testing.T) {
	// Reset state
	requestDuration = nil
	requestCount = nil

	// Should not panic even without initialization
	RecordCustom(context.Background(), "TestResource", "POST", 201, 42.5)
}

func TestResponseWriterImplementsFlusher(t *testing.T) {
	rec := httptest.NewRecorder()
	var w http.ResponseWriter = &responseWriter{ResponseWriter: rec, statusCode: http.StatusOK}

	flusher, ok := w.(http.Flusher)
	if !ok {
		t.Fatal("responseWriter wrapping a Flusher-capable writer should implement http.Flusher")
	}

	flusher.Flush()
	if !rec.Flushed {
		t.Error("Flush() should delegate to underlying ResponseWriter")
	}
}

func TestResponseWriterFlushNoopWithoutFlusher(t *testing.T) {
	var w http.ResponseWriter = &responseWriter{ResponseWriter: &minimalResponseWriter{}, statusCode: http.StatusOK}

	flusher, ok := w.(http.Flusher)
	if !ok {
		t.Fatal("responseWriter should always implement http.Flusher")
	}

	// Should not panic when underlying writer doesn't support Flush
	flusher.Flush()
}

func TestMiddlewarePreservesFlusher(t *testing.T) {
	_ = Initialize(noop.NewMeterProvider())

	var flusherAvailable bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, flusherAvailable = w.(http.Flusher)
		w.WriteHeader(http.StatusOK)
	})

	wrapped := Middleware()(handler)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if !flusherAvailable {
		t.Error("Middleware should preserve http.Flusher interface from underlying ResponseWriter")
	}
}

// minimalResponseWriter implements only http.ResponseWriter (no Flusher)
type minimalResponseWriter struct{}

func (m *minimalResponseWriter) Header() http.Header         { return http.Header{} }
func (m *minimalResponseWriter) Write(b []byte) (int, error) { return len(b), nil }
func (m *minimalResponseWriter) WriteHeader(int)             {}

func TestMiddlewareAutoInitializes(t *testing.T) {
	// Reset state
	requestDuration = nil
	requestCount = nil

	// Middleware should auto-initialize
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := Middleware()(handler)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	// Should not panic
	wrapped.ServeHTTP(rec, req)

	// Should have initialized
	if requestDuration == nil || requestCount == nil {
		t.Error("Middleware did not auto-initialize instruments")
	}
}
