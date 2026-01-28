// Package metrics provides OpenTelemetry metrics middleware for go-restgen.
//
// The middleware records request duration and count, with dimensions for
// resource type, HTTP method, and status code. This enables tracking slow
// endpoints and error rates per resource.
//
// Usage:
//
//	import "github.com/sjgoldie/go-restgen/metrics"
//
//	// Initialize with your meter provider
//	metrics.Initialize(meterProvider)
//
//	// Add middleware to router
//	r := chi.NewRouter()
//	r.Use(metrics.Middleware())
//
// If no meter provider is set, the middleware uses the global meter provider.
// If OTEL is not configured at all, metrics are no-ops with negligible overhead.
package metrics

import (
	"context"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/sjgoldie/go-restgen/metadata"
)

const (
	instrumentationName = "github.com/sjgoldie/go-restgen/metrics"
)

var (
	requestDuration metric.Float64Histogram
	requestCount    metric.Int64Counter
)

// Initialize sets up the metrics instruments using the provided meter provider.
// If provider is nil, uses the global meter provider.
// Call this in your main() before starting the server.
func Initialize(provider metric.MeterProvider) error {
	if provider == nil {
		provider = otel.GetMeterProvider()
	}

	meter := provider.Meter(instrumentationName)

	var err error
	requestDuration, err = meter.Float64Histogram(
		"restgen.request.duration",
		metric.WithDescription("Request duration in milliseconds"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return err
	}

	requestCount, err = meter.Int64Counter(
		"restgen.request.count",
		metric.WithDescription("Total number of requests"),
	)
	if err != nil {
		return err
	}

	return nil
}

// Middleware returns HTTP middleware that records request metrics.
// Records restgen.request.duration (histogram) and restgen.request.count (counter)
// with attributes: resource, method, status.
//
// The resource attribute comes from go-restgen metadata context when available,
// otherwise defaults to the URL path.
func Middleware() func(http.Handler) http.Handler {
	// Ensure instruments are initialized (uses global provider if Initialize not called)
	if requestDuration == nil || requestCount == nil {
		_ = Initialize(nil)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Wrap response writer to capture status code
			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			// Process request
			next.ServeHTTP(wrapped, r)

			// Calculate duration
			duration := float64(time.Since(start).Milliseconds())

			// Get resource name from metadata context if available
			resource := r.URL.Path
			if meta, err := metadata.FromContext(r.Context()); err == nil && meta != nil {
				resource = meta.TypeName
			}

			// Build attributes
			attrs := []attribute.KeyValue{
				attribute.String("resource", resource),
				attribute.String("method", r.Method),
				attribute.Int("status", wrapped.statusCode),
			}

			// Record metrics
			ctx := r.Context()
			requestDuration.Record(ctx, duration, metric.WithAttributes(attrs...))
			requestCount.Add(ctx, 1, metric.WithAttributes(attrs...))
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture the status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Unwrap returns the underlying ResponseWriter for middleware compatibility
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// StatusCode returns the recorded status code (for testing)
func StatusCode(w http.ResponseWriter) int {
	if rw, ok := w.(*responseWriter); ok {
		return rw.statusCode
	}
	return 0
}

// GetRequestDuration returns the duration histogram for custom recording.
// Returns nil if Initialize has not been called.
func GetRequestDuration() metric.Float64Histogram {
	return requestDuration
}

// GetRequestCount returns the count counter for custom recording.
// Returns nil if Initialize has not been called.
func GetRequestCount() metric.Int64Counter {
	return requestCount
}

// RecordCustom records a custom metric observation with the standard attributes.
// Useful for recording metrics from custom handlers or actions.
func RecordCustom(ctx context.Context, resource, method string, status int, durationMs float64) {
	if requestDuration == nil || requestCount == nil {
		return
	}

	attrs := []attribute.KeyValue{
		attribute.String("resource", resource),
		attribute.String("method", method),
		attribute.Int("status", status),
	}

	requestDuration.Record(ctx, durationMs, metric.WithAttributes(attrs...))
	requestCount.Add(ctx, 1, metric.WithAttributes(attrs...))
}
