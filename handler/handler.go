// Package handler provides generic HTTP handlers for RESTful CRUD endpoints.
//
// Logging uses context-aware slog methods for OpenTelemetry trace propagation.
// Log levels: Error for server failures, Warn for recoverable issues, Debug for client errors.
// Configure logging in your application's main() before starting handlers.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	apperrors "github.com/sjgoldie/go-restgen/errors"
	"github.com/sjgoldie/go-restgen/filestore"
	"github.com/sjgoldie/go-restgen/internal/common"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/service"
)

// Custom handler function types
// These allow developers to provide custom logic while the framework handles
// parsing, validation, error handling, and response formatting.

// CustomGetFunc is the signature for custom Get handlers.
// Returns the item for JSON response.
type CustomGetFunc[T any] func(
	ctx context.Context,
	svc *service.Common[T],
	meta *metadata.TypeMetadata,
	auth *metadata.AuthInfo,
	id string,
) (*T, error)

// CustomGetAllFunc is the signature for custom GetAll handlers.
// The custom function receives all parsed/validated inputs and returns items with count and sums.
// Query options are available via metadata.QueryOptionsFromContext(ctx) if needed.
type CustomGetAllFunc[T any] func(
	ctx context.Context,
	svc *service.Common[T],
	meta *metadata.TypeMetadata,
	auth *metadata.AuthInfo,
) ([]*T, int, map[string]float64, error)

// CustomCreateFunc is the signature for custom Create handlers.
// The custom function receives the decoded item and optional file data.
// If file is not nil and meta.IsFileResource, the function should store the file.
type CustomCreateFunc[T any] func(
	ctx context.Context,
	svc *service.Common[T],
	meta *metadata.TypeMetadata,
	auth *metadata.AuthInfo,
	item T,
	file io.Reader,
	fileMeta filestore.FileMetadata,
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

// ActionFunc is the signature for custom action handlers.
// Actions operate on an existing resource and can optionally return an updated item.
// The payload is the raw request body for custom parsing.
type ActionFunc[T any] func(
	ctx context.Context,
	svc *service.Common[T],
	meta *metadata.TypeMetadata,
	auth *metadata.AuthInfo,
	id string,
	item *T,
	payload []byte,
) (*T, error)

// CustomBatchCreateFunc is the signature for custom batch create handlers.
type CustomBatchCreateFunc[T any] func(
	ctx context.Context,
	svc *service.Common[T],
	meta *metadata.TypeMetadata,
	auth *metadata.AuthInfo,
	items []T,
) ([]*T, error)

// CustomBatchUpdateFunc is the signature for custom batch update handlers.
type CustomBatchUpdateFunc[T any] func(
	ctx context.Context,
	svc *service.Common[T],
	meta *metadata.TypeMetadata,
	auth *metadata.AuthInfo,
	items []T,
) ([]*T, error)

// CustomBatchDeleteFunc is the signature for custom batch delete handlers.
type CustomBatchDeleteFunc[T any] func(
	ctx context.Context,
	svc *service.Common[T],
	meta *metadata.TypeMetadata,
	auth *metadata.AuthInfo,
	items []T,
) error

// batchSetup holds common setup data for batch operations
type batchSetup[T any] struct {
	svc   *service.Common[T]
	meta  *metadata.TypeMetadata
	auth  *metadata.AuthInfo
	items []T
	ctx   context.Context
}

// handleBodyReadError handles errors from JSON decoding or body reading.
// MaxBytesError returns 413; other errors return 400.
func handleBodyReadError(ctx context.Context, w http.ResponseWriter, err error, msg string) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		slog.DebugContext(ctx, msg, "error", err)
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	slog.DebugContext(ctx, msg, "error", err)
	http.Error(w, "bad request", http.StatusBadRequest)
}

// handleMutationError handles errors from Create/Update operations
func handleMutationError(ctx context.Context, w http.ResponseWriter, err error, operation string) {
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
	slog.ErrorContext(ctx, "failed to "+operation+" item", "error", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

// requestContext holds common setup data for /{id} handlers.
type requestContext[T any] struct {
	ctx  context.Context
	svc  *service.Common[T]
	meta *metadata.TypeMetadata
	auth *metadata.AuthInfo
	id   string
	item *T
}

// setupRequest performs common validation and setup for /{id} handlers.
// Creates the service, extracts metadata and auth, parses the URL ID parameter,
// and optionally pre-fetches the item using getFunc.
// On error, the HTTP error response is already written to w.
func setupRequest[T any](w http.ResponseWriter, r *http.Request, getFunc CustomGetFunc[T]) (requestContext[T], error) {
	ctx := r.Context()
	var zero requestContext[T]

	svc, err := service.New[T]()
	if err != nil {
		slog.ErrorContext(ctx, "failed to create service", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return zero, err
	}

	meta, err := metadata.FromContext(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "metadata not found in context", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return zero, err
	}

	var id string
	if meta.URLParamUUID != "" {
		id = chi.URLParam(r, meta.URLParamUUID)
		if id == "" {
			slog.DebugContext(ctx, "missing id parameter", "paramUUID", meta.URLParamUUID)
			http.Error(w, "bad request", http.StatusBadRequest)
			return zero, fmt.Errorf("missing id parameter %s", meta.URLParamUUID)
		}
	}

	auth, _ := ctx.Value(metadata.AuthInfoKey).(*metadata.AuthInfo)

	var item *T
	if getFunc != nil {
		item, err = getFunc(ctx, svc, meta, auth, id)
		if err != nil {
			handleMutationError(ctx, w, err, "get item")
			return zero, err
		}
	}

	return requestContext[T]{
		ctx:  ctx,
		svc:  svc,
		meta: meta,
		auth: auth,
		id:   id,
		item: item,
	}, nil
}

// StandardGetAll is the default GetAll implementation that calls svc.GetAll.
// Use this when no custom logic is needed.
func StandardGetAll[T any](ctx context.Context, svc *service.Common[T], meta *metadata.TypeMetadata, auth *metadata.AuthInfo) ([]*T, int, map[string]float64, error) {
	return svc.GetAll(ctx)
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
		ctx := r.Context()

		svc, err := service.New[T]()
		if err != nil {
			slog.ErrorContext(ctx, "failed to create service", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Get metadata from context
		meta, err := metadata.FromContext(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "metadata not found in context", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Get auth from context (may be nil if not authenticated)
		auth, _ := ctx.Value(metadata.AuthInfoKey).(*metadata.AuthInfo)

		// Get query options from context (parsed by middleware) for pagination headers
		opts := metadata.QueryOptionsFromContext(ctx)

		// Call the provided function
		items, totalCount, sums, err := getAllFunc(ctx, svc, meta, auth)
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
			slog.ErrorContext(ctx, "failed to get all items", "error", err)
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

		// Set sum headers
		for field, value := range sums {
			headerName := "X-Sum-" + field
			if value == float64(int64(value)) {
				w.Header().Set(headerName, strconv.FormatInt(int64(value), 10))
			} else {
				w.Header().Set(headerName, strconv.FormatFloat(value, 'f', -1, 64))
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(items); err != nil {
			slog.ErrorContext(ctx, "failed to encode response", "error", err)
		}
	}
}

// StandardGet is the default Get implementation that calls svc.Get.
func StandardGet[T any](ctx context.Context, svc *service.Common[T], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string) (*T, error) {
	return svc.Get(ctx, id)
}

// StandardGetByParentRelation gets an item via the parent's relation field.
// Used for single routes like /posts/{id}/author where the child ID is resolved from the parent.
func StandardGetByParentRelation[T any](ctx context.Context, svc *service.Common[T], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string) (*T, error) {
	return svc.GetByParentRelation(ctx, id)
}

// Get handles GET requests to retrieve a single item of type T by ID.
// The getFunc parameter controls how the item is fetched - use StandardGet for default behavior
// or provide a custom function for specialized logic (e.g., getting user from auth token).
func Get[T any](getFunc CustomGetFunc[T]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rc, err := setupRequest(w, r, getFunc)
		if err != nil {
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(rc.item); err != nil {
			slog.ErrorContext(rc.ctx, "failed to encode response", "error", err)
		}
	}
}

// StandardCreate is the default Create implementation.
// If file is provided and meta.IsFileResource, stores the file and sets fields on item.
func StandardCreate[T any](ctx context.Context, svc *service.Common[T], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, item T, file io.Reader, fileMeta filestore.FileMetadata) (*T, error) {
	var storageKey string

	// If file is provided and this is a file resource, store the file and set fields
	if file != nil && meta.IsFileResource {
		var err error
		storageKey, err = svc.StoreFile(ctx, file, fileMeta)
		if err != nil {
			return nil, err
		}

		// Set file fields on the item
		if fr, ok := any(&item).(filestore.FileResource); ok {
			fr.SetStorageKey(storageKey)
			fr.SetFilename(fileMeta.Filename)
			fr.SetContentType(fileMeta.ContentType)
			fr.SetSize(fileMeta.Size)
		}
	}

	// Create the DB record
	savedItem, err := svc.Create(ctx, item)
	if err != nil {
		// Clean up stored file on DB error
		if storageKey != "" {
			if delErr := svc.DeleteStoredFile(ctx, storageKey); delErr != nil {
				slog.WarnContext(ctx, "failed to delete orphaned file after DB error", "key", storageKey, "error", delErr)
			}
		}
		return nil, err
	}

	return savedItem, nil
}

// Create handles POST requests to create a new item of type T.
// The createFunc parameter controls how the item is created - use StandardCreate for default behavior
// or provide a custom function for specialized logic.
// Automatically detects multipart form data for file uploads.
func Create[T any](createFunc CustomCreateFunc[T]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		svc, err := service.New[T]()
		if err != nil {
			slog.ErrorContext(ctx, "failed to create service", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Get metadata from context
		meta, err := metadata.FromContext(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "metadata not found in context", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Get auth from context (may be nil if not authenticated)
		auth, _ := ctx.Value(metadata.AuthInfoKey).(*metadata.AuthInfo)

		var item T
		var file io.Reader
		var fileMeta filestore.FileMetadata

		// Parse request body - either multipart form or JSON
		contentType := r.Header.Get("Content-Type")
		if strings.HasPrefix(contentType, "multipart/form-data") {
			// Limit request body size to prevent memory exhaustion
			const maxUploadSize = 32 << 20 // 32 MB
			r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
			if err := r.ParseMultipartForm(maxUploadSize); err != nil { // #nosec G120 -- body already bounded by MaxBytesReader above
				slog.DebugContext(ctx, "failed to parse multipart form", "error", err)
				http.Error(w, "bad request: failed to parse multipart form", http.StatusBadRequest)
				return
			}

			// Get the file
			formFile, header, err := r.FormFile("file")
			if err == nil {
				defer func() { _ = formFile.Close() }()
				file = formFile
				fileMeta = filestore.FileMetadata{
					Filename:    sanitizeFilename(header.Filename),
					ContentType: header.Header.Get("Content-Type"),
					Size:        header.Size,
				}
				if fileMeta.ContentType == "" {
					fileMeta.ContentType = "application/octet-stream"
				}
			}

			// JSON metadata comes from form field in multipart
			if metadataValues, ok := r.MultipartForm.Value["metadata"]; ok && len(metadataValues) > 0 {
				if err := json.Unmarshal([]byte(metadataValues[0]), &item); err != nil {
					slog.DebugContext(ctx, "failed to parse metadata JSON", "error", err)
					http.Error(w, "bad request: invalid metadata JSON", http.StatusBadRequest)
					return
				}
			}
		} else {
			// JSON body
			if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
				handleBodyReadError(ctx, w, err, "failed to decode request body")
				return
			}
		}

		// Call the provided function
		savedItem, err := createFunc(ctx, svc, meta, auth, item, file, fileMeta)
		if err != nil {
			handleMutationError(ctx, w, err, "create")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if err := json.NewEncoder(w).Encode(savedItem); err != nil {
			slog.ErrorContext(ctx, "failed to encode response", "error", err)
		}
	}
}

// StandardUpdate is the default Update implementation that calls svc.Update.
// Use this when no custom logic is needed.
func StandardUpdate[T any](ctx context.Context, svc *service.Common[T], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item T) (*T, error) {
	return svc.Update(ctx, id, item)
}

// StandardUpdateByParentRelation updates an item via the parent's relation field.
// Used for single routes like /posts/{id}/author where the child ID is resolved from the parent.
func StandardUpdateByParentRelation[T any](ctx context.Context, svc *service.Common[T], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item T) (*T, error) {
	return svc.UpdateByParentRelation(ctx, id, item)
}

// Update handles PUT requests to update an existing item of type T.
// The updateFunc parameter controls how the item is updated - use StandardUpdate for default behavior
// or provide a custom function for specialized logic.
func Update[T any](updateFunc CustomUpdateFunc[T]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rc, err := setupRequest[T](w, r, nil)
		if err != nil {
			return
		}

		var item T
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			handleBodyReadError(rc.ctx, w, err, "failed to decode request body")
			return
		}

		// Set ID from path onto the struct (overwrite any ID from JSON)
		// This ensures the path ID takes precedence and is required for Bun's WherePK()
		// Skip for single routes with no URL param - custom function handles ID
		if rc.id != "" {
			if err := common.SetFieldFromString(&item, rc.meta.PKField, rc.id); err != nil {
				slog.ErrorContext(rc.ctx, "failed to set ID field", "error", err)
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
		}

		savedItem, err := updateFunc(rc.ctx, rc.svc, rc.meta, rc.auth, rc.id, item)
		if err != nil {
			handleMutationError(rc.ctx, w, err, "update")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(savedItem); err != nil {
			slog.ErrorContext(rc.ctx, "failed to encode response", "error", err)
		}
	}
}

// StandardDelete is the default Delete implementation.
// If the item implements FileResource, also deletes the file from storage.
func StandardDelete[T any](ctx context.Context, svc *service.Common[T], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string) error {
	// Get the item first (validates existence and permissions)
	item, err := svc.Get(ctx, id)
	if err != nil {
		return err
	}

	// Extract storage key if this is a file resource (via type assertion)
	var storageKey string
	if fr, ok := any(item).(filestore.FileResource); ok {
		storageKey = fr.GetStorageKey()
	}

	// Delete the DB record
	if err := svc.Delete(ctx, id); err != nil {
		return err
	}

	// Delete from storage if there was a file (log but don't fail if this fails)
	if storageKey != "" {
		if err := svc.DeleteStoredFile(ctx, storageKey); err != nil {
			slog.WarnContext(ctx, "failed to delete file from storage", "key", storageKey, "error", err)
		}
	}

	return nil
}

// Delete handles DELETE requests to delete an item of type T by ID.
// The deleteFunc parameter controls how the item is deleted - use StandardDelete for default behavior
// or provide a custom function for specialized logic.
func Delete[T any](deleteFunc CustomDeleteFunc[T]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rc, err := setupRequest[T](w, r, nil)
		if err != nil {
			return
		}

		if err := deleteFunc(rc.ctx, rc.svc, rc.meta, rc.auth, rc.id); err != nil {
			handleMutationError(rc.ctx, w, err, "delete")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// Download handles GET requests to download a file resource.
// Streams the file for proxy mode, redirects to signed URL for signed URL mode.
func Download[T any]() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rc, err := setupRequest[T](w, r, nil)
		if err != nil {
			return
		}

		result, err := rc.svc.Download(rc.ctx, rc.id)
		if err != nil {
			if errors.Is(err, filestore.ErrStorageKeyNotFound) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			handleMutationError(rc.ctx, w, err, "download")
			return
		}

		// Redirect mode
		if result.SignedURL != "" {
			http.Redirect(w, r, result.SignedURL, http.StatusTemporaryRedirect)
			return
		}

		// Stream mode
		defer func() { _ = result.Reader.Close() }()

		contentType := result.ContentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", contentType)

		if result.Size > 0 {
			w.Header().Set("Content-Length", strconv.FormatInt(result.Size, 10))
		}
		if result.Filename != "" {
			w.Header().Set("Content-Disposition", contentDisposition(result.Filename))
		}

		w.WriteHeader(http.StatusOK)
		if _, err := io.Copy(w, result.Reader); err != nil {
			slog.WarnContext(rc.ctx, "failed to stream file", "error", err)
		}
	}
}

// Action handles POST requests for custom actions on a resource.
// Actions are registered at POST /resources/{id}/{action-name}.
// The item is fetched first (validating existence and permissions), then passed to the action function.
func Action[T any](actionFunc ActionFunc[T]) http.HandlerFunc {
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

		result, err := actionFunc(rc.ctx, rc.svc, rc.meta, rc.auth, rc.id, rc.item, payload)
		if err != nil {
			handleMutationError(rc.ctx, w, err, "action")
			return
		}

		if result == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(result); err != nil {
			slog.ErrorContext(rc.ctx, "failed to encode response", "error", err)
		}
	}
}

// StandardBatchCreate is the default batch create implementation.
func StandardBatchCreate[T any](ctx context.Context, svc *service.Common[T], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, items []T) ([]*T, error) {
	return svc.BatchCreate(ctx, items)
}

// StandardBatchUpdate is the default batch update implementation.
func StandardBatchUpdate[T any](ctx context.Context, svc *service.Common[T], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, items []T) ([]*T, error) {
	return svc.BatchUpdate(ctx, items)
}

// StandardBatchDelete is the default batch delete implementation.
func StandardBatchDelete[T any](ctx context.Context, svc *service.Common[T], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, items []T) error {
	return svc.BatchDelete(ctx, items)
}

// setupBatch performs common validation and setup for batch operations.
// Returns nil if successful, otherwise writes error response and returns error.
func setupBatch[T any](w http.ResponseWriter, r *http.Request, opName string) *batchSetup[T] {
	ctx := r.Context()

	svc, err := service.New[T]()
	if err != nil {
		slog.ErrorContext(ctx, "failed to create service", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return nil
	}

	meta, err := metadata.FromContext(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "metadata not found in context", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return nil
	}

	if meta.IsFileResource {
		http.Error(w, opName+" not supported for file resources", http.StatusNotImplemented)
		return nil
	}

	var items []T
	if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			slog.DebugContext(ctx, "failed to decode batch request", "error", err)
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return nil
		}
		slog.DebugContext(ctx, "failed to decode batch request", "error", err)
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return nil
	}

	if len(items) == 0 {
		http.Error(w, "batch request must contain at least one item", http.StatusBadRequest)
		return nil
	}

	if meta.BatchLimit > 0 && len(items) > meta.BatchLimit {
		http.Error(w, "batch size exceeds limit", http.StatusBadRequest)
		return nil
	}

	auth, _ := ctx.Value(metadata.AuthInfoKey).(*metadata.AuthInfo)

	return &batchSetup[T]{
		svc:   svc,
		meta:  meta,
		auth:  auth,
		items: items,
		ctx:   ctx,
	}
}

// BatchCreate handles POST requests to /resources/batch for batch creation.
func BatchCreate[T any](createFunc CustomBatchCreateFunc[T]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setup := setupBatch[T](w, r, "batch create")
		if setup == nil {
			return
		}

		results, err := createFunc(setup.ctx, setup.svc, setup.meta, setup.auth, setup.items)
		if err != nil {
			handleBatchError(setup.ctx, w, err, "batch create")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if err := json.NewEncoder(w).Encode(results); err != nil {
			slog.ErrorContext(setup.ctx, "failed to encode response", "error", err)
		}
	}
}

// BatchUpdate handles PUT requests to /resources/batch for batch updates.
func BatchUpdate[T any](updateFunc CustomBatchUpdateFunc[T]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setup := setupBatch[T](w, r, "batch update")
		if setup == nil {
			return
		}

		results, err := updateFunc(setup.ctx, setup.svc, setup.meta, setup.auth, setup.items)
		if err != nil {
			handleBatchError(setup.ctx, w, err, "batch update")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(results); err != nil {
			slog.ErrorContext(setup.ctx, "failed to encode response", "error", err)
		}
	}
}

// BatchDelete handles DELETE requests to /resources/batch for batch deletion.
func BatchDelete[T any](deleteFunc CustomBatchDeleteFunc[T]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setup := setupBatch[T](w, r, "batch delete")
		if setup == nil {
			return
		}

		if err := deleteFunc(setup.ctx, setup.svc, setup.meta, setup.auth, setup.items); err != nil {
			handleBatchError(setup.ctx, w, err, "batch delete")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// handleBatchError handles errors from batch operations
func handleBatchError(ctx context.Context, w http.ResponseWriter, err error, operation string) {
	if errors.Is(err, context.Canceled) {
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		http.Error(w, "request timeout", http.StatusGatewayTimeout)
		return
	}
	var validationErr *apperrors.ValidationError
	if errors.As(err, &validationErr) {
		http.Error(w, validationErr.Message, http.StatusBadRequest)
		return
	}
	if errors.Is(err, apperrors.ErrDuplicate) {
		http.Error(w, "one or more resources already exist", http.StatusBadRequest)
		return
	}
	if errors.Is(err, apperrors.ErrInvalidReference) {
		http.Error(w, "one or more items have invalid reference", http.StatusBadRequest)
		return
	}
	if errors.Is(err, apperrors.ErrNotFound) {
		http.Error(w, "one or more items not found", http.StatusNotFound)
		return
	}
	if errors.Is(err, apperrors.ErrUnavailable) {
		w.Header().Set("Retry-After", "5")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	slog.ErrorContext(ctx, "failed to "+operation, "error", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}
