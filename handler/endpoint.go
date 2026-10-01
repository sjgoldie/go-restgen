package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"reflect"

	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/service"
)

// EndpointHandler is the signature for item-level endpoint handlers.
// Unlike ActionFunc which returns (*T, error), EndpointHandler returns (any, int, error)
// allowing any response type and explicit HTTP status code. A result implementing Responder
// writes its own response (any content type or body); any other result is encoded as JSON.
// The item is pre-fetched by the framework (validates existence and permissions).
type EndpointHandler[T any] func(
	ctx context.Context,
	svc *service.Common[T],
	meta *metadata.TypeMetadata,
	auth *metadata.AuthInfo,
	id string,
	item *T,
	payload []byte,
) (any, int, error)

// RootEndpointHandler is the signature for root-level endpoint handlers.
// Root endpoints have no parent model — they receive the raw request for maximum flexibility.
// Like EndpointHandler, a result implementing Responder writes its own response.
type RootEndpointHandler func(
	ctx context.Context,
	auth *metadata.AuthInfo,
	r *http.Request,
) (any, int, error)

// Endpoint handles requests for item-level endpoint handlers.
// Works like Action but supports any HTTP method and any return type.
// The item is fetched first (validating existence and permissions), then passed to the handler.
func Endpoint[T any](fn EndpointHandler[T]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rc, err := setupRequest(w, r, StandardGet[T])
		if err != nil {
			return
		}

		payload, err := io.ReadAll(r.Body)
		if err != nil {
			handleBodyReadError(rc.ctx, w, err, "failed to read request body")
			return
		}

		result, statusCode, err := fn(rc.ctx, rc.svc, rc.meta, rc.auth, rc.id, rc.item, payload)
		if err != nil {
			handleOperationError(rc.ctx, w, err, "endpoint")
			return
		}

		writeEndpointResponse(rc.ctx, w, result, statusCode)
	}
}

// RootEndpoint handles requests for root-level endpoint handlers.
// No model or parent — just auth and the raw request.
func RootEndpoint(fn RootEndpointHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		auth, _ := ctx.Value(metadata.AuthInfoKey).(*metadata.AuthInfo)

		result, statusCode, err := fn(ctx, auth, r)
		if err != nil {
			handleOperationError(ctx, w, err, "root endpoint")
			return
		}

		writeEndpointResponse(ctx, w, result, statusCode)
	}
}

// Responder is implemented by endpoint results that write their own response: headers, status,
// and body, in any format. Endpoint and root endpoint handlers can return one instead of a value
// to encode as JSON. The status code returned alongside it is not used. An error from
// WriteResponse is logged; by then the response may already be partly written.
type Responder interface {
	WriteResponse(w http.ResponseWriter) error
}

// writeEndpointResponse writes the response for endpoint handlers. A Responder writes its own
// response; any other result is encoded as JSON. nil result -> 204 No Content. Status code 0
// defaults to 200.
func writeEndpointResponse(ctx context.Context, w http.ResponseWriter, result any, statusCode int) {
	if result == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if responder, ok := result.(Responder); ok {
		if v := reflect.ValueOf(responder); v.Kind() == reflect.Pointer && v.IsNil() {
			slog.ErrorContext(ctx, "endpoint returned a nil Responder", "type", v.Type().String())
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, http.StatusText(http.StatusInternalServerError))
			return
		}
		if err := responder.WriteResponse(w); err != nil {
			slog.ErrorContext(ctx, "failed to write endpoint response", "error", err)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(result); err != nil {
		slog.ErrorContext(ctx, "failed to encode response", "error", err)
	}
}
