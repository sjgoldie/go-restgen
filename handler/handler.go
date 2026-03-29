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
	"mime"
	"net/http"
	"strconv"

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
// The custom function receives all parsed/validated inputs and returns items with count, sums, and cursor info.
// Query options are available via metadata.QueryOptionsFromContext(ctx) if needed.
type CustomGetAllFunc[T any] func(
	ctx context.Context,
	svc *service.Common[T],
	meta *metadata.TypeMetadata,
	auth *metadata.AuthInfo,
) ([]*T, int, map[string]float64, *metadata.CursorInfo, error)

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

// CustomPatchFunc is the signature for custom Patch handlers.
// The custom function receives the ID, the existing item (before patch),
// and the patched item (after JSON overlay). This allows custom handlers
// to compare old and new states.
type CustomPatchFunc[T any] func(
	ctx context.Context,
	svc *service.Common[T],
	meta *metadata.TypeMetadata,
	auth *metadata.AuthInfo,
	id string,
	existing *T,
	patched T,
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

// CustomBatchPatchFunc is the signature for custom batch patch handlers.
type CustomBatchPatchFunc[T any] func(
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

// batchBase holds common setup data shared by all batch operations.
type batchBase[T any] struct {
	svc  *service.Common[T]
	meta *metadata.TypeMetadata
	auth *metadata.AuthInfo
	ctx  context.Context
}

// batchSetup extends batchBase with decoded items for batch create/update/delete.
type batchSetup[T any] struct {
	batchBase[T]
	items []T
}

// handleBodyReadError handles errors from JSON decoding or body reading.
// MaxBytesError returns 413; other errors return 400.
func handleBodyReadError(ctx context.Context, w http.ResponseWriter, err error, msg string) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		slog.DebugContext(ctx, msg, "error", err)
		WriteError(w, http.StatusRequestEntityTooLarge, ErrCodeRequestTooLarge, http.StatusText(http.StatusRequestEntityTooLarge))
		return
	}
	slog.DebugContext(ctx, msg, "error", err)
	WriteError(w, http.StatusBadRequest, ErrCodeBadRequest, http.StatusText(http.StatusBadRequest))
}

// handleOperationError handles errors from mutation operations (create, update, patch, delete, batch).
func handleOperationError(ctx context.Context, w http.ResponseWriter, err error, operation string) {
	if errors.Is(err, context.Canceled) {
		return // Client disconnected, no response needed
	}
	if errors.Is(err, context.DeadlineExceeded) {
		WriteError(w, http.StatusGatewayTimeout, ErrCodeRequestTimeout, http.StatusText(http.StatusGatewayTimeout))
		return
	}
	var validationErr *apperrors.ValidationError
	if errors.As(err, &validationErr) {
		WriteError(w, http.StatusBadRequest, ErrCodeValidationError, validationErr.Message)
		return
	}
	if errors.Is(err, apperrors.ErrDuplicate) {
		WriteError(w, http.StatusBadRequest, ErrCodeDuplicate, http.StatusText(http.StatusBadRequest))
		return
	}
	if errors.Is(err, apperrors.ErrInvalidReference) {
		WriteError(w, http.StatusBadRequest, ErrCodeInvalidReference, http.StatusText(http.StatusBadRequest))
		return
	}
	if errors.Is(err, apperrors.ErrNotFound) {
		WriteError(w, http.StatusNotFound, ErrCodeNotFound, http.StatusText(http.StatusNotFound))
		return
	}
	if errors.Is(err, apperrors.ErrUnavailable) {
		w.Header().Set("Retry-After", "5")
		WriteError(w, http.StatusServiceUnavailable, ErrCodeServiceUnavailable, http.StatusText(http.StatusServiceUnavailable))
		return
	}
	slog.ErrorContext(ctx, "failed to "+operation, "error", err)
	WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, http.StatusText(http.StatusInternalServerError))
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
		WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, http.StatusText(http.StatusInternalServerError))
		return zero, err
	}

	meta, err := metadata.FromContext(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "metadata not found in context", "error", err)
		WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, http.StatusText(http.StatusInternalServerError))
		return zero, err
	}

	var id string
	if meta.URLParamUUID != "" {
		id = chi.URLParam(r, meta.URLParamUUID)
		if id == "" {
			slog.DebugContext(ctx, "missing id parameter", "paramUUID", meta.URLParamUUID)
			WriteError(w, http.StatusBadRequest, ErrCodeBadRequest, http.StatusText(http.StatusBadRequest))
			return zero, fmt.Errorf("missing id parameter %s", meta.URLParamUUID)
		}
	}

	auth, _ := ctx.Value(metadata.AuthInfoKey).(*metadata.AuthInfo)

	var item *T
	if getFunc != nil {
		item, err = getFunc(ctx, svc, meta, auth, id)
		if err != nil {
			handleOperationError(ctx, w, err, "get item")
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
func StandardGetAll[T any](ctx context.Context, svc *service.Common[T], meta *metadata.TypeMetadata, auth *metadata.AuthInfo) ([]*T, int, map[string]float64, *metadata.CursorInfo, error) {
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
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, http.StatusText(http.StatusInternalServerError))
			return
		}

		// Get metadata from context
		meta, err := metadata.FromContext(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "metadata not found in context", "error", err)
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, http.StatusText(http.StatusInternalServerError))
			return
		}

		// Get auth from context (may be nil if not authenticated)
		auth, _ := ctx.Value(metadata.AuthInfoKey).(*metadata.AuthInfo)

		// Get query options from context (parsed by middleware) for pagination headers
		opts := metadata.QueryOptionsFromContext(ctx)

		// Call the provided function
		items, totalCount, sums, cursorInfo, err := getAllFunc(ctx, svc, meta, auth)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return // Client disconnected, no response needed
			}
			if errors.Is(err, context.DeadlineExceeded) {
				WriteError(w, http.StatusGatewayTimeout, ErrCodeRequestTimeout, http.StatusText(http.StatusGatewayTimeout))
				return
			}
			if errors.Is(err, apperrors.ErrUnavailable) {
				w.Header().Set("Retry-After", "5")
				WriteError(w, http.StatusServiceUnavailable, ErrCodeServiceUnavailable, http.StatusText(http.StatusServiceUnavailable))
				return
			}
			slog.ErrorContext(ctx, "failed to get all items", "error", err)
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, http.StatusText(http.StatusInternalServerError))
			return
		}

		// Build response envelope
		response := ListResponse{Data: items}

		// Build pagination info
		var pagination *PaginationInfo
		if cursorInfo != nil {
			// Cursor-based pagination
			pagination = &PaginationInfo{
				HasMore: &cursorInfo.HasMore,
			}
			if cursorInfo.NextCursor != "" {
				pagination.NextCursor = &cursorInfo.NextCursor
			}
			if cursorInfo.PrevCursor != "" {
				pagination.PrevCursor = &cursorInfo.PrevCursor
			}
			if opts != nil && opts.CountTotal && totalCount > 0 {
				pagination.TotalCount = &totalCount
			}
		} else if opts != nil && (opts.Limit > 0 || opts.Offset > 0 || (opts.CountTotal && totalCount > 0)) {
			// Offset-based pagination
			pagination = &PaginationInfo{}
			if opts.Limit > 0 {
				limit := opts.Limit
				pagination.Limit = &limit
			}
			if opts.Offset > 0 {
				offset := opts.Offset
				pagination.Offset = &offset
			}
			if opts.CountTotal && totalCount > 0 {
				pagination.TotalCount = &totalCount
			}
		}
		response.Pagination = pagination

		// Include sums if any were requested
		if len(sums) > 0 {
			response.Sums = sums
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(response); err != nil {
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
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, http.StatusText(http.StatusInternalServerError))
			return
		}

		// Get metadata from context
		meta, err := metadata.FromContext(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "metadata not found in context", "error", err)
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, http.StatusText(http.StatusInternalServerError))
			return
		}

		// Get auth from context (may be nil if not authenticated)
		auth, _ := ctx.Value(metadata.AuthInfoKey).(*metadata.AuthInfo)

		var item T
		var file io.Reader
		var fileMeta filestore.FileMetadata

		// Parse request body - either multipart form or JSON
		mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if mediaType == "multipart/form-data" {
			// Limit request body size to prevent memory exhaustion
			r.Body = http.MaxBytesReader(w, r.Body, meta.MaxUploadSize)
			if err := r.ParseMultipartForm(meta.MaxUploadSize); err != nil { // #nosec G120 -- body already bounded by MaxBytesReader above
				slog.DebugContext(ctx, "failed to parse multipart form", "error", err)
				WriteError(w, http.StatusBadRequest, ErrCodeBadRequest, http.StatusText(http.StatusBadRequest))
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
					WriteError(w, http.StatusBadRequest, ErrCodeBadRequest, http.StatusText(http.StatusBadRequest))
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
			handleOperationError(ctx, w, err, "create")
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
				WriteError(w, http.StatusBadRequest, ErrCodeBadRequest, http.StatusText(http.StatusBadRequest))
				return
			}
		}

		savedItem, err := updateFunc(rc.ctx, rc.svc, rc.meta, rc.auth, rc.id, item)
		if err != nil {
			handleOperationError(rc.ctx, w, err, "update")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(savedItem); err != nil {
			slog.ErrorContext(rc.ctx, "failed to encode response", "error", err)
		}
	}
}

// StandardPatch is the default Patch implementation.
// It delegates to svc.Patch with the pre-merged item so validators receive OpPatch.
func StandardPatch[T any](ctx context.Context, svc *service.Common[T], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, existing *T, patched T) (*T, error) {
	return svc.Patch(ctx, id, patched)
}

// StandardPatchByParentRelation patches an item via the parent's relation field.
// Used for single routes like /posts/{id}/author where the child ID is resolved from the parent.
func StandardPatchByParentRelation[T any](ctx context.Context, svc *service.Common[T], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, existing *T, patched T) (*T, error) {
	return svc.PatchByParentRelation(ctx, id, patched)
}

// Patch handles PATCH requests for partial updates.
// It fetches the existing item using getFunc, clones it, overlays the request body onto the clone,
// and passes both old and new states to the patch function.
func Patch[T any](patchFunc CustomPatchFunc[T], getFunc CustomGetFunc[T]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rc, err := setupRequest(w, r, getFunc)
		if err != nil {
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			handleBodyReadError(rc.ctx, w, err, "failed to read request body")
			return
		}

		patched := *rc.item
		if err := json.Unmarshal(bodyBytes, &patched); err != nil {
			handleBodyReadError(rc.ctx, w, err, "failed to decode request body")
			return
		}

		if rc.id != "" {
			if err := common.SetFieldFromString(&patched, rc.meta.PKField, rc.id); err != nil {
				slog.ErrorContext(rc.ctx, "failed to set ID field", "error", err)
				WriteError(w, http.StatusBadRequest, ErrCodeBadRequest, http.StatusText(http.StatusBadRequest))
				return
			}
		}

		savedItem, err := patchFunc(rc.ctx, rc.svc, rc.meta, rc.auth, rc.id, rc.item, patched)
		if err != nil {
			handleOperationError(rc.ctx, w, err, "patch")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(savedItem); err != nil {
			slog.ErrorContext(rc.ctx, "failed to encode response", "error", err)
		}
	}
}

// StandardBatchPatch is the default batch patch implementation.
// Delegates to svc.BatchPatch so validators receive OpPatch.
func StandardBatchPatch[T any](ctx context.Context, svc *service.Common[T], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, items []T) ([]*T, error) {
	return svc.BatchPatch(ctx, items)
}

// BatchPatch handles PATCH requests to /resources/batch for batch partial updates.
// Each item in the array must include its primary key so the existing record can be fetched.
// The handler fetches each existing item, clones it, overlays the patch body,
// and passes the merged items to the batch patch function.
func BatchPatch[T any](patchFunc CustomBatchPatchFunc[T]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		base := setupBatchBase[T](w, r)
		if base == nil {
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			handleBodyReadError(base.ctx, w, err, "failed to read request body")
			return
		}

		var rawItems []json.RawMessage
		if err := json.Unmarshal(bodyBytes, &rawItems); err != nil {
			handleBodyReadError(base.ctx, w, err, "failed to decode batch request")
			return
		}

		if len(rawItems) == 0 {
			WriteError(w, http.StatusBadRequest, ErrCodeBadRequest, http.StatusText(http.StatusBadRequest))
			return
		}

		if base.meta.BatchLimit > 0 && len(rawItems) > base.meta.BatchLimit {
			WriteError(w, http.StatusBadRequest, ErrCodeBadRequest, http.StatusText(http.StatusBadRequest))
			return
		}

		merged := make([]T, 0, len(rawItems))
		for _, raw := range rawItems {
			var partial T
			if err := json.Unmarshal(raw, &partial); err != nil {
				handleBodyReadError(base.ctx, w, err, "failed to decode batch item")
				return
			}

			id := common.GetFieldAsString(&partial, base.meta.PKField)
			if id == "" {
				WriteError(w, http.StatusBadRequest, ErrCodeBadRequest, http.StatusText(http.StatusBadRequest))
				return
			}

			existing, err := base.svc.Get(base.ctx, id)
			if err != nil {
				handleOperationError(base.ctx, w, err, "batch patch fetch")
				return
			}

			patched := *existing
			if err := json.Unmarshal(raw, &patched); err != nil {
				handleBodyReadError(base.ctx, w, err, "failed to apply patch")
				return
			}

			merged = append(merged, patched)
		}

		results, err := patchFunc(base.ctx, base.svc, base.meta, base.auth, merged)
		if err != nil {
			handleOperationError(base.ctx, w, err, "batch patch")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(BatchResponse{Data: results}); err != nil {
			slog.ErrorContext(base.ctx, "failed to encode response", "error", err)
		}
	}
}

// StandardDelete is the default Delete implementation.
// If the item implements FileResource, fetches it first to extract the storage key
// for file cleanup after deletion. For non-file resources, delegates directly to
// svc.Delete which handles existence checks and validation internally.
func StandardDelete[T any](ctx context.Context, svc *service.Common[T], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string) error {
	var storageKey string
	if meta.IsFileResource {
		item, err := svc.Get(ctx, id)
		if err != nil {
			return err
		}
		if fr, ok := any(item).(filestore.FileResource); ok {
			storageKey = fr.GetStorageKey()
		}
	}

	if err := svc.Delete(ctx, id); err != nil {
		return err
	}

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
			handleOperationError(rc.ctx, w, err, "delete")
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
				WriteError(w, http.StatusNotFound, ErrCodeNotFound, http.StatusText(http.StatusNotFound))
				return
			}
			handleOperationError(rc.ctx, w, err, "download")
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
			handleOperationError(rc.ctx, w, err, "action")
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

// setupBatchBase performs common validation and setup shared by all batch operations:
// service creation, metadata extraction, file resource check, and auth extraction.
// Returns nil if an error response has already been written.
func setupBatchBase[T any](w http.ResponseWriter, r *http.Request) *batchBase[T] {
	ctx := r.Context()

	svc, err := service.New[T]()
	if err != nil {
		slog.ErrorContext(ctx, "failed to create service", "error", err)
		WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, http.StatusText(http.StatusInternalServerError))
		return nil
	}

	meta, err := metadata.FromContext(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "metadata not found in context", "error", err)
		WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, http.StatusText(http.StatusInternalServerError))
		return nil
	}

	if meta.IsFileResource {
		WriteError(w, http.StatusNotImplemented, ErrCodeNotImplemented, http.StatusText(http.StatusNotImplemented))
		return nil
	}

	auth, _ := ctx.Value(metadata.AuthInfoKey).(*metadata.AuthInfo)

	return &batchBase[T]{
		svc:  svc,
		meta: meta,
		auth: auth,
		ctx:  ctx,
	}
}

// setupBatch performs full setup for batch create/update/delete: base setup + body decoding.
// Returns nil if an error response has already been written.
func setupBatch[T any](w http.ResponseWriter, r *http.Request) *batchSetup[T] {
	base := setupBatchBase[T](w, r)
	if base == nil {
		return nil
	}

	var items []T
	if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
		handleBodyReadError(base.ctx, w, err, "failed to decode batch request")
		return nil
	}

	if len(items) == 0 {
		WriteError(w, http.StatusBadRequest, ErrCodeBadRequest, http.StatusText(http.StatusBadRequest))
		return nil
	}

	if base.meta.BatchLimit > 0 && len(items) > base.meta.BatchLimit {
		WriteError(w, http.StatusBadRequest, ErrCodeBadRequest, http.StatusText(http.StatusBadRequest))
		return nil
	}

	return &batchSetup[T]{
		batchBase: *base,
		items:     items,
	}
}

// BatchCreate handles POST requests to /resources/batch for batch creation.
func BatchCreate[T any](createFunc CustomBatchCreateFunc[T]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setup := setupBatch[T](w, r)
		if setup == nil {
			return
		}

		results, err := createFunc(setup.ctx, setup.svc, setup.meta, setup.auth, setup.items)
		if err != nil {
			handleOperationError(setup.ctx, w, err, "batch create")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if err := json.NewEncoder(w).Encode(BatchResponse{Data: results}); err != nil {
			slog.ErrorContext(setup.ctx, "failed to encode response", "error", err)
		}
	}
}

// BatchUpdate handles PUT requests to /resources/batch for batch updates.
func BatchUpdate[T any](updateFunc CustomBatchUpdateFunc[T]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setup := setupBatch[T](w, r)
		if setup == nil {
			return
		}

		results, err := updateFunc(setup.ctx, setup.svc, setup.meta, setup.auth, setup.items)
		if err != nil {
			handleOperationError(setup.ctx, w, err, "batch update")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(BatchResponse{Data: results}); err != nil {
			slog.ErrorContext(setup.ctx, "failed to encode response", "error", err)
		}
	}
}

// BatchDelete handles DELETE requests to /resources/batch for batch deletion.
func BatchDelete[T any](deleteFunc CustomBatchDeleteFunc[T]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setup := setupBatch[T](w, r)
		if setup == nil {
			return
		}

		if err := deleteFunc(setup.ctx, setup.svc, setup.meta, setup.auth, setup.items); err != nil {
			handleOperationError(setup.ctx, w, err, "batch delete")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
