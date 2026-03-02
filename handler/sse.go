package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/service"
)

// SSEEvent represents a single Server-Sent Event.
type SSEEvent struct {
	Event string // Event type (optional, omitted if empty)
	Data  any    // Event data, JSON-encoded by the framework
	ID    string // Event ID (optional, omitted if empty)
}

// SSEFunc is the signature for item-level SSE handlers.
// The item is pre-fetched by the framework. The func writes events to the channel.
// Return nil when done streaming. The channel is closed by the framework after return.
// Context is cancelled when the client disconnects.
type SSEFunc[T any] func(
	ctx context.Context,
	svc *service.Common[T],
	meta *metadata.TypeMetadata,
	auth *metadata.AuthInfo,
	id string,
	item *T,
	events chan<- SSEEvent,
) error

// RootSSEFunc is the signature for root-level SSE handlers.
// No parent model — receives auth and the raw request.
type RootSSEFunc func(
	ctx context.Context,
	auth *metadata.AuthInfo,
	r *http.Request,
	events chan<- SSEEvent,
) error

// SSE handles Server-Sent Event requests for item-level endpoints.
// Sets up SSE headers, creates event channel, runs func in goroutine,
// streams events to client, and cancels context on disconnect.
func SSE[T any](fn SSEFunc[T]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rc, err := setupRequest(w, r, StandardGet[T])
		if err != nil {
			return
		}

		events := make(chan SSEEvent)

		ctx, cancel := context.WithCancel(rc.ctx)
		defer cancel()

		go func() {
			defer close(events)
			if err := fn(ctx, rc.svc, rc.meta, rc.auth, rc.id, rc.item, events); err != nil {
				if !errors.Is(err, context.Canceled) {
					slog.ErrorContext(ctx, "sse func error", "error", err)
				}
			}
		}()

		streamSSEEvents(w, events)
	}
}

// RootSSE handles Server-Sent Event requests for root-level endpoints.
func RootSSE(fn RootSSEFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		auth, _ := ctx.Value(metadata.AuthInfoKey).(*metadata.AuthInfo)

		events := make(chan SSEEvent)

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		go func() {
			defer close(events)
			if err := fn(ctx, auth, r, events); err != nil {
				if !errors.Is(err, context.Canceled) {
					slog.ErrorContext(ctx, "root sse func error", "error", err)
				}
			}
		}()

		streamSSEEvents(w, events)
	}
}

// streamSSEEvents sets SSE headers and streams events from the channel to the response writer.
func streamSSEEvents(w http.ResponseWriter, events <-chan SSEEvent) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, canFlush := w.(http.Flusher)

	for event := range events {
		data, err := json.Marshal(event.Data)
		if err != nil {
			slog.Error("failed to marshal sse event data", "error", err)
			continue
		}

		if event.ID != "" {
			if _, err := fmt.Fprintf(w, "id: %s\n", event.ID); err != nil {
				slog.Error("failed to write sse event id", "error", err)
				return
			}
		}
		if event.Event != "" {
			if _, err := fmt.Fprintf(w, "event: %s\n", event.Event); err != nil {
				slog.Error("failed to write sse event type", "error", err)
				return
			}
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			slog.Error("failed to write sse event data", "error", err)
			return
		}

		if canFlush {
			flusher.Flush()
		}
	}
}
