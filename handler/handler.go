// Package handler provides generic HTTP handlers for RESTful CRUD endpoints.
//
// By default, handler warnings are suppressed (log level set to Error).
// To enable warnings in development, set the log level:
//
//	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
//	    Level: slog.LevelWarn,
//	}))
//	slog.SetDefault(logger)
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	apperrors "github.com/sjgoldie/go-restgen/errors"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/service"
)

func init() {
	// Set default log level to Error (suppresses warnings in production)
	// Users can override this in their main() before starting handlers
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
	slog.SetDefault(logger)
}

// handleMutationError handles errors from Create/Update operations
func handleMutationError(w http.ResponseWriter, err error, operation string) {
	if errors.Is(err, context.Canceled) {
		return // Client disconnected, no response needed
	}
	if errors.Is(err, context.DeadlineExceeded) {
		http.Error(w, "request timeout", http.StatusGatewayTimeout)
		return
	}
	if errors.Is(err, apperrors.ErrDuplicate) {
		http.Error(w, "resource already exists", http.StatusBadRequest)
		return
	}
	if errors.Is(err, apperrors.ErrInvalidReference) {
		http.Error(w, "invalid reference to related resource", http.StatusBadRequest)
		return
	}
	if errors.Is(err, apperrors.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if errors.Is(err, apperrors.ErrUnavailable) {
		w.Header().Set("Retry-After", "5")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	slog.Warn("failed to "+operation+" item", "error", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

// GetAll handles GET requests to retrieve all items of type T
// Supports query parameters for filtering, sorting, and pagination:
//   - filter[field]=value or filter[field][op]=value (ops: eq, neq, gt, gte, lt, lte, like)
//   - sort=field1,-field2 (prefix with - for descending)
//   - limit=N, offset=N for pagination
//   - count=true to include X-Total-Count header
func GetAll[T any]() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc, err := service.New[T]()
		if err != nil {
			slog.Warn("failed to create service", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Extract relations from context if available
		ctx := r.Context()
		relations, ok := ctx.Value("relations").([]string)
		if !ok {
			relations = []string{}
		}

		// Parse query parameters into QueryOptions
		opts := parseQueryOptions(r.URL.Query())

		// Add QueryOptions to context
		ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

		items, totalCount, err := svc.GetAll(ctx, relations)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return // Client disconnected, no response needed
			}
			if errors.Is(err, context.DeadlineExceeded) {
				http.Error(w, "request timeout", http.StatusGatewayTimeout)
				return
			}
			if errors.Is(err, apperrors.ErrUnavailable) {
				w.Header().Set("Retry-After", "5")
				http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)
				return
			}
			slog.Warn("failed to get all items", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Set pagination headers
		if opts.CountTotal && totalCount > 0 {
			w.Header().Set("X-Total-Count", strconv.Itoa(totalCount))
		}
		if opts.Limit > 0 {
			w.Header().Set("X-Limit", strconv.Itoa(opts.Limit))
		}
		if opts.Offset > 0 {
			w.Header().Set("X-Offset", strconv.Itoa(opts.Offset))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(items); err != nil {
			slog.Warn("failed to encode response", "error", err)
		}
	}
}

// parseQueryOptions extracts filtering, sorting, and pagination options from query parameters
func parseQueryOptions(query url.Values) *metadata.QueryOptions {
	opts := &metadata.QueryOptions{
		Filters: make(map[string]metadata.FilterValue),
	}

	// Parse filters: filter[field]=value or filter[field][op]=value
	for key, values := range query {
		if len(values) == 0 {
			continue
		}
		value := values[0]

		// Check for filter[field] or filter[field][op] pattern
		if strings.HasPrefix(key, "filter[") && strings.HasSuffix(key, "]") {
			// Remove "filter[" prefix and "]" suffix
			inner := key[7 : len(key)-1]

			// Check for nested operator: field][op
			if idx := strings.Index(inner, "]["); idx != -1 {
				field := inner[:idx]
				op := inner[idx+2:]
				opts.Filters[field] = metadata.FilterValue{Value: value, Operator: op}
			} else {
				// Simple filter: filter[field]=value (default eq operator)
				opts.Filters[inner] = metadata.FilterValue{Value: value, Operator: "eq"}
			}
		}
	}

	// Parse sort: sort=field1,-field2
	if sortStr := query.Get("sort"); sortStr != "" {
		fields := strings.Split(sortStr, ",")
		for _, field := range fields {
			field = strings.TrimSpace(field)
			if field == "" {
				continue
			}
			desc := false
			if strings.HasPrefix(field, "-") {
				desc = true
				field = field[1:]
			}
			opts.Sort = append(opts.Sort, metadata.SortField{Field: field, Desc: desc})
		}
	}

	// Parse pagination
	if limitStr := query.Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 {
			opts.Limit = limit
		}
	}

	if offsetStr := query.Get("offset"); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil && offset >= 0 {
			opts.Offset = offset
		}
	}

	// Parse count flag
	if countStr := query.Get("count"); countStr == "true" || countStr == "1" {
		opts.CountTotal = true
	}

	return opts
}

// Get handles GET requests to retrieve a single item of type T by ID
func Get[T any]() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc, err := service.New[T]()
		if err != nil {
			slog.Warn("failed to create service", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Extract relations from context if available
		ctx := r.Context()
		relations, ok := ctx.Value("relations").([]string)
		if !ok {
			relations = []string{}
		}

		// Get metadata from context
		meta, err := metadata.FromContext(ctx)
		if err != nil {
			slog.Warn("metadata not found in context", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Parse ID from URL parameter
		id, err := strconv.Atoi(chi.URLParam(r, meta.URLParamUUID))
		if err != nil {
			slog.Warn("invalid id parameter", "error", err, "paramUUID", meta.URLParamUUID)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		item, err := svc.Get(r.Context(), id, relations)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return // Client disconnected, no response needed
			}
			if errors.Is(err, context.DeadlineExceeded) {
				http.Error(w, "request timeout", http.StatusGatewayTimeout)
				return
			}
			if errors.Is(err, apperrors.ErrNotFound) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			if errors.Is(err, apperrors.ErrUnavailable) {
				w.Header().Set("Retry-After", "5")
				http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)
				return
			}
			slog.Warn("failed to get item", "id", id, "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(item); err != nil {
			slog.Warn("failed to encode response", "error", err)
		}
	}
}

// Create handles POST requests to create a new item of type T
func Create[T any]() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc, err := service.New[T]()
		if err != nil {
			slog.Warn("failed to create service", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		var item T
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			slog.Warn("failed to decode request body", "error", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		savedItem, err := svc.Create(r.Context(), item)
		if err != nil {
			handleMutationError(w, err, "create")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if err := json.NewEncoder(w).Encode(savedItem); err != nil {
			slog.Warn("failed to encode response", "error", err)
		}
	}
}

// Update handles PUT requests to update an existing item of type T
func Update[T any]() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc, err := service.New[T]()
		if err != nil {
			slog.Warn("failed to create service", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Get metadata from context
		ctx := r.Context()
		meta, err := metadata.FromContext(ctx)
		if err != nil {
			slog.Warn("metadata not found in context", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Parse ID from URL parameter
		id, err := strconv.Atoi(chi.URLParam(r, meta.URLParamUUID))
		if err != nil {
			slog.Warn("invalid id parameter", "error", err, "paramUUID", meta.URLParamUUID)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		var item T
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			slog.Warn("failed to decode request body", "error", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Set ID from path onto the struct (overwrite any ID from JSON)
		reflect.ValueOf(&item).Elem().FieldByName("ID").SetInt(int64(id))

		savedItem, err := svc.Update(r.Context(), id, item)
		if err != nil {
			handleMutationError(w, err, "update")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(savedItem); err != nil {
			slog.Warn("failed to encode response", "error", err)
		}
	}
}

// Delete handles DELETE requests to delete an item of type T by ID
func Delete[T any]() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc, err := service.New[T]()
		if err != nil {
			slog.Warn("failed to create service", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Get metadata from context
		ctx := r.Context()
		meta, err := metadata.FromContext(ctx)
		if err != nil {
			slog.Warn("metadata not found in context", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Parse ID from URL parameter
		id, err := strconv.Atoi(chi.URLParam(r, meta.URLParamUUID))
		if err != nil {
			slog.Warn("invalid id parameter", "error", err, "paramUUID", meta.URLParamUUID)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		if err := svc.Delete(r.Context(), id); err != nil {
			if errors.Is(err, context.Canceled) {
				return // Client disconnected, no response needed
			}
			if errors.Is(err, context.DeadlineExceeded) {
				http.Error(w, "request timeout", http.StatusGatewayTimeout)
				return
			}
			if errors.Is(err, apperrors.ErrNotFound) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			if errors.Is(err, apperrors.ErrUnavailable) {
				w.Header().Set("Retry-After", "5")
				http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)
				return
			}
			slog.Warn("failed to delete item", "id", id, "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
