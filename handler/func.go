package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	apperrors "github.com/sjgoldie/go-restgen/errors"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/service"
)

// FuncHandler is the signature for item-level anything func handlers.
// Unlike ActionFunc which returns (*T, error), FuncHandler returns (any, int, error)
// allowing any response type and explicit HTTP status code.
// The item is pre-fetched by the framework (validates existence and permissions).
type FuncHandler[T any] func(
	ctx context.Context,
	svc *service.Common[T],
	meta *metadata.TypeMetadata,
	auth *metadata.AuthInfo,
	id string,
	item *T,
	payload []byte,
) (any, int, error)

// RootFuncHandler is the signature for root-level anything func handlers.
// Root funcs have no parent model — they receive the raw request for maximum flexibility.
type RootFuncHandler func(
	ctx context.Context,
	auth *metadata.AuthInfo,
	r *http.Request,
) (any, int, error)

// Func handles requests for item-level anything func endpoints.
// Works like Action but supports any HTTP method and any return type.
// The item is fetched first (validating existence and permissions), then passed to the func.
func Func[T any](fn FuncHandler[T]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		svc, err := service.New[T]()
		if err != nil {
			slog.ErrorContext(ctx, "failed to create service", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		meta, err := metadata.FromContext(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "metadata not found in context", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		id := chi.URLParam(r, meta.URLParamUUID)
		if id == "" {
			slog.DebugContext(ctx, "missing id parameter", "paramUUID", meta.URLParamUUID)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		item, err := svc.Get(ctx, id)
		if err != nil {
			if errors.Is(err, apperrors.ErrNotFound) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			handleMutationError(ctx, w, err, "get for func")
			return
		}

		payload, err := io.ReadAll(r.Body)
		if err != nil {
			slog.DebugContext(ctx, "failed to read request body", "error", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		auth, _ := ctx.Value(metadata.AuthInfoKey).(*metadata.AuthInfo)

		result, statusCode, err := fn(ctx, svc, meta, auth, id, item, payload)
		if err != nil {
			handleMutationError(ctx, w, err, "func")
			return
		}

		writeFuncResponse(ctx, w, result, statusCode)
	}
}

// RootFunc handles requests for root-level anything func endpoints.
// No model or parent — just auth and the raw request.
func RootFunc(fn RootFuncHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		auth, _ := ctx.Value(metadata.AuthInfoKey).(*metadata.AuthInfo)

		result, statusCode, err := fn(ctx, auth, r)
		if err != nil {
			handleMutationError(ctx, w, err, "root func")
			return
		}

		writeFuncResponse(ctx, w, result, statusCode)
	}
}

// writeFuncResponse writes the JSON response for func handlers.
// nil result -> 204 No Content. Status code 0 defaults to 200.
func writeFuncResponse(ctx context.Context, w http.ResponseWriter, result any, statusCode int) {
	if result == nil {
		w.WriteHeader(http.StatusNoContent)
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
