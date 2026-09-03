package metrics

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"

	"github.com/sjgoldie/go-restgen/metadata"
)

func TestInitialize(t *testing.T) {
	// Test with nil provider (uses global)
	err := Initialize(nil)
	if err != nil {
		t.Errorf("Initialize(nil) returned error: %v", err)
	}

	// Verify instruments are set
	if GetRequestDuration() == nil {
		t.Error("requestDuration not initialized")
	}
	if GetRequestCount() == nil {
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
	current.Store(nil)

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
	current.Store(nil)

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
	if GetRequestDuration() == nil || GetRequestCount() == nil {
		t.Error("Middleware did not auto-initialize instruments")
	}
}

// TestMiddlewareDefaultsToOK verifies the recorded status when the handler never
// calls WriteHeader explicitly.
func TestMiddlewareDefaultsToOK(t *testing.T) {
	_ = Initialize(noop.NewMeterProvider())

	var captured int
	wrapped := Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		captured = StatusCode(w)
	}))

	wrapped.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/test", nil))

	if captured != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", captured, http.StatusOK)
	}
}

// TestInitializeRaceWithTraffic exercises Initialize running concurrently with
// in-flight requests. The instruments are package-level state read on every
// request, so an unsynchronised write here is a data race. Run with -race.
func TestInitializeRaceWithTraffic(t *testing.T) {
	wrapped := Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	stop := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				if err := Initialize(noop.NewMeterProvider()); err != nil {
					t.Errorf("Initialize returned error: %v", err)
					return
				}
			}
		}
	}()

	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				req := httptest.NewRequest("GET", "/articles", nil)
				wrapped.ServeHTTP(httptest.NewRecorder(), req)
			}
		}()
	}

	close(stop)
	wg.Wait()
}

// TestRecordCustomRaceWithInitialize covers the same shared state via the
// exported RecordCustom path rather than the middleware.
func TestRecordCustomRaceWithInitialize(t *testing.T) {
	if err := Initialize(noop.NewMeterProvider()); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = Initialize(noop.NewMeterProvider())
			}
		}
	}()

	for range 25 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				RecordCustom(context.Background(), "Article", "GET", http.StatusOK, 1.5)
			}
		}()
	}

	close(stop)
	wg.Wait()
}

var (
	errHistogram = errors.New("histogram unavailable")
	errCounter   = errors.New("counter unavailable")
)

// failingMeter returns an error from one of the instrument constructors so the
// error paths in Initialize can be exercised.
type failingMeter struct {
	metric.Meter
	failCounter bool
}

func (m failingMeter) Float64Histogram(name string, opts ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	if m.failCounter {
		return m.Meter.Float64Histogram(name, opts...)
	}
	return nil, errHistogram
}

func (m failingMeter) Int64Counter(name string, opts ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	if m.failCounter {
		return nil, errCounter
	}
	return m.Meter.Int64Counter(name, opts...)
}

type failingProvider struct {
	metric.MeterProvider
	failCounter bool
}

func (p failingProvider) Meter(name string, opts ...metric.MeterOption) metric.Meter {
	return failingMeter{Meter: p.MeterProvider.Meter(name, opts...), failCounter: p.failCounter}
}

func TestInitializeReturnsHistogramError(t *testing.T) {
	provider := failingProvider{MeterProvider: noop.NewMeterProvider()}

	if err := Initialize(provider); !errors.Is(err, errHistogram) {
		t.Errorf("Initialize error = %v, want %v", err, errHistogram)
	}
}

func TestInitializeReturnsCounterError(t *testing.T) {
	provider := failingProvider{MeterProvider: noop.NewMeterProvider(), failCounter: true}

	if err := Initialize(provider); !errors.Is(err, errCounter) {
		t.Errorf("Initialize error = %v, want %v", err, errCounter)
	}
}

// TestFailedInitializeLeavesPreviousInstruments verifies a failed Initialize
// does not clobber working instruments with a half-built pair.
func TestFailedInitializeLeavesPreviousInstruments(t *testing.T) {
	if err := Initialize(noop.NewMeterProvider()); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}

	if err := Initialize(failingProvider{MeterProvider: noop.NewMeterProvider()}); err == nil {
		t.Fatal("expected Initialize to fail")
	}

	if GetRequestDuration() == nil || GetRequestCount() == nil {
		t.Error("a failed Initialize discarded the previously working instruments")
	}
}

// TestGettersReturnNilBeforeInitialize covers the uninitialised branch of both
// accessors.
func TestGettersReturnNilBeforeInitialize(t *testing.T) {
	current.Store(nil)

	if GetRequestDuration() != nil {
		t.Error("GetRequestDuration should be nil before Initialize")
	}
	if GetRequestCount() != nil {
		t.Error("GetRequestCount should be nil before Initialize")
	}
}

// TestMiddlewareSeedsResourceNameHolder verifies the middleware places a
// metadata.ResourceName holder in the request context so go-restgen's metadata
// middleware, which runs inside it, can report the type name outward.
func TestMiddlewareSeedsResourceNameHolder(t *testing.T) {
	if err := Initialize(noop.NewMeterProvider()); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}

	var seen *metadata.ResourceName
	handler := Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = r.Context().Value(metadata.ResourceNameKey).(*metadata.ResourceName)
		if seen != nil {
			seen.Name = "Article"
		}
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/articles", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if seen == nil {
		t.Fatal("expected a ResourceName holder in the inner request context")
	}
}

// TestResourceNamePrefersReportedTypeName covers both branches of the
// resource attribute: the type name reported by the metadata middleware, and
// the URL path when nothing was reported.
func TestResourceNamePrefersReportedTypeName(t *testing.T) {
	req := httptest.NewRequest("GET", "/articles/1", nil)

	if got := resourceName(req, &metadata.ResourceName{Name: "Article"}); got != "Article" {
		t.Errorf("reported type name: got %q, want Article", got)
	}
	if got := resourceName(req, &metadata.ResourceName{}); got != "/articles/1" {
		t.Errorf("empty holder: got %q, want /articles/1", got)
	}
	if got := resourceName(req, nil); got != "/articles/1" {
		t.Errorf("nil holder: got %q, want /articles/1", got)
	}
}

// TestMiddlewareSkipsRecordingWhenUninitialised covers the nil-instrument guard
// in the request path. Before that guard existed this panicked.
func TestMiddlewareSkipsRecordingWhenUninitialised(t *testing.T) {
	handler := Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Drop the instruments after the middleware was built, simulating an
	// Initialize that failed before any traffic arrived.
	current.Store(nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/articles", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
