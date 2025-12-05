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
	"github.com/google/uuid"
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

// Custom handler function types
// These allow developers to provide custom logic while the framework handles
// parsing, validation, error handling, and response formatting.

// CustomGetFunc is the signature for custom Get handlers.
// The custom function receives all parsed/validated inputs and returns the item.
type CustomGetFunc[T any] func(
	ctx context.Context,
	svc *service.Common[T],
	meta *metadata.TypeMetadata,
	auth *metadata.AuthInfo,
	id string,
	relations []string,
) (*T, error)

// CustomGetAllFunc is the signature for custom GetAll handlers.
// The custom function receives all parsed/validated inputs and returns items with count.
type CustomGetAllFunc[T any] func(
	ctx context.Context,
	svc *service.Common[T],
	meta *metadata.TypeMetadata,
	auth *metadata.AuthInfo,
	opts *metadata.QueryOptions,
	relations []string,
) ([]*T, int, error)

// CustomCreateFunc is the signature for custom Create handlers.
// The custom function receives the decoded item and returns the created item.
type CustomCreateFunc[T any] func(
	ctx context.Context,
	svc *service.Common[T],
	meta *metadata.TypeMetadata,
	auth *metadata.AuthInfo,
	item T,
) (*T, error)

// CustomUpdateFunc is the signature for custom Update handlers.
// The custom function receives the ID and decoded item, returns the updated item.
type CustomUpdateFunc[T any] func(
	ctx context.Context,
	svc *service.Common[T],
	meta *metadata.TypeMetadata,
	auth *metadata.AuthInfo,
	id string,
	item T,
) (*T, error)

// CustomDeleteFunc is the signature for custom Delete handlers.
// The custom function receives the ID and performs the deletion.
type CustomDeleteFunc[T any] func(
	ctx context.Context,
	svc *service.Common[T],
	meta *metadata.TypeMetadata,
	auth *metadata.AuthInfo,
	id string,
) error

// handleMutationError handles errors from Create/Update operations
func handleMutationError(w http.ResponseWriter, err error, operation string) {
	if errors.Is(err, context.Canceled) {
		return // Client disconnected, no response needed
	}
	if errors.Is(err, context.DeadlineExceeded) {
		http.Error(w, "request timeout", http.StatusGatewayTimeout)
		return
	}
	// Check for validation error - return custom message to client
	var validationErr *apperrors.ValidationError
	if errors.As(err, &validationErr) {
		http.Error(w, validationErr.Message, http.StatusBadRequest)
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

// StandardGetAll is the default GetAll implementation that calls svc.GetAll.
// Use this when no custom logic is needed.
func StandardGetAll[T any](ctx context.Context, svc *service.Common[T], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, opts *metadata.QueryOptions, relations []string) ([]*T, int, error) {
	return svc.GetAll(ctx, relations)
}

// GetAll handles GET requests to retrieve all items of type T.
// The getAllFunc parameter controls how items are fetched - use StandardGetAll for default behavior
// or provide a custom function for specialized logic.
// Supports query parameters for filtering, sorting, and pagination:
//   - filter[field]=value or filter[field][op]=value (ops: eq, neq, gt, gte, lt, lte, like)
//   - sort=field1,-field2 (prefix with - for descending)
//   - limit=N, offset=N for pagination
//   - count=true to include X-Total-Count header
func GetAll[T any](getAllFunc CustomGetAllFunc[T]) http.HandlerFunc {
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

		// Parse query parameters into QueryOptions
		opts := parseQueryOptions(r.URL.Query())

		// Add QueryOptions to context
		ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

		// Get auth from context (may be nil if not authenticated)
		auth, _ := ctx.Value(metadata.AuthInfoKey).(*metadata.AuthInfo)

		// Call the provided function
		items, totalCount, err := getAllFunc(ctx, svc, meta, auth, opts, relations)
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

// StandardGet is the default Get implementation that calls svc.Get.
// Use this when no custom logic is needed.
func StandardGet[T any](ctx context.Context, svc *service.Common[T], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, relations []string) (*T, error) {
	return svc.Get(ctx, id, relations)
}

// Get handles GET requests to retrieve a single item of type T by ID.
// The getFunc parameter controls how the item is fetched - use StandardGet for default behavior
// or provide a custom function for specialized logic (e.g., getting user from auth token).
func Get[T any](getFunc CustomGetFunc[T]) http.HandlerFunc {
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

		// Get ID from URL parameter (passed as string to support both int and UUID PKs)
		id := chi.URLParam(r, meta.URLParamUUID)
		if id == "" {
			slog.Warn("missing id parameter", "paramUUID", meta.URLParamUUID)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Get auth from context (may be nil if not authenticated)
		auth, _ := ctx.Value(metadata.AuthInfoKey).(*metadata.AuthInfo)

		// Call the provided function
		item, err := getFunc(ctx, svc, meta, auth, id, relations)
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

// StandardCreate is the default Create implementation that calls svc.Create.
// Use this when no custom logic is needed.
func StandardCreate[T any](ctx context.Context, svc *service.Common[T], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, item T) (*T, error) {
	return svc.Create(ctx, item)
}

// Create handles POST requests to create a new item of type T.
// The createFunc parameter controls how the item is created - use StandardCreate for default behavior
// or provide a custom function for specialized logic.
func Create[T any](createFunc CustomCreateFunc[T]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc, err := service.New[T]()
		if err != nil {
			slog.Warn("failed to create service", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		ctx := r.Context()

		// Get metadata from context
		meta, err := metadata.FromContext(ctx)
		if err != nil {
			slog.Warn("metadata not found in context", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Get auth from context (may be nil if not authenticated)
		auth, _ := ctx.Value(metadata.AuthInfoKey).(*metadata.AuthInfo)

		var item T
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			slog.Warn("failed to decode request body", "error", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Call the provided function
		savedItem, err := createFunc(ctx, svc, meta, auth, item)
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

// StandardUpdate is the default Update implementation that calls svc.Update.
// Use this when no custom logic is needed.
func StandardUpdate[T any](ctx context.Context, svc *service.Common[T], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item T) (*T, error) {
	return svc.Update(ctx, id, item)
}

// Update handles PUT requests to update an existing item of type T.
// The updateFunc parameter controls how the item is updated - use StandardUpdate for default behavior
// or provide a custom function for specialized logic.
func Update[T any](updateFunc CustomUpdateFunc[T]) http.HandlerFunc {
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

		// Get ID from URL parameter (passed as string to support both int and UUID PKs)
		id := chi.URLParam(r, meta.URLParamUUID)
		if id == "" {
			slog.Warn("missing id parameter", "paramUUID", meta.URLParamUUID)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Get auth from context (may be nil if not authenticated)
		auth, _ := ctx.Value(metadata.AuthInfoKey).(*metadata.AuthInfo)

		var item T
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			slog.Warn("failed to decode request body", "error", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Set ID from path onto the struct (overwrite any ID from JSON)
		// This ensures the path ID takes precedence and is required for Bun's WherePK()
		if err := setIDField(&item, id); err != nil {
			slog.Warn("failed to set ID field", "error", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Call the provided function
		savedItem, err := updateFunc(ctx, svc, meta, auth, id, item)
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

// StandardDelete is the default Delete implementation that calls svc.Delete.
// Use this when no custom logic is needed.
func StandardDelete[T any](ctx context.Context, svc *service.Common[T], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string) error {
	return svc.Delete(ctx, id)
}

// Delete handles DELETE requests to delete an item of type T by ID.
// The deleteFunc parameter controls how the item is deleted - use StandardDelete for default behavior
// or provide a custom function for specialized logic.
func Delete[T any](deleteFunc CustomDeleteFunc[T]) http.HandlerFunc {
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

		// Get ID from URL parameter (passed as string to support both int and UUID PKs)
		id := chi.URLParam(r, meta.URLParamUUID)
		if id == "" {
			slog.Warn("missing id parameter", "paramUUID", meta.URLParamUUID)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Get auth from context (may be nil if not authenticated)
		auth, _ := ctx.Value(metadata.AuthInfoKey).(*metadata.AuthInfo)

		// Call the provided function
		if err := deleteFunc(ctx, svc, meta, auth, id); err != nil {
			if errors.Is(err, context.Canceled) {
				return // Client disconnected, no response needed
			}
			if errors.Is(err, context.DeadlineExceeded) {
				http.Error(w, "request timeout", http.StatusGatewayTimeout)
				return
			}
			// Check for validation error - return custom message to client
			var validationErr *apperrors.ValidationError
			if errors.As(err, &validationErr) {
				http.Error(w, validationErr.Message, http.StatusBadRequest)
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

// setIDField sets the ID field on a struct from a string value
// Handles int, int64, string, and uuid.UUID field types
func setIDField[T any](item *T, id string) error {
	idField := reflect.ValueOf(item).Elem().FieldByName("ID")
	if !idField.IsValid() || !idField.CanSet() {
		return errors.New("ID field not found or not settable")
	}

	switch idField.Kind() {
	case reflect.Int, reflect.Int64:
		// Parse string to int
		intID, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			return err
		}
		idField.SetInt(intID)
	case reflect.String:
		idField.SetString(id)
	default:
		// Check for uuid.UUID type (which is [16]byte array)
		if idField.Type() == reflect.TypeOf(uuid.UUID{}) {
			parsed, err := uuid.Parse(id)
			if err != nil {
				return err
			}
			idField.Set(reflect.ValueOf(parsed))
		} else {
			return errors.New("unsupported ID field type: " + idField.Type().String())
		}
	}

	return nil
}
