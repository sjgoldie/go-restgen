// Package service provides business logic layer for CRUD operations.
package service

import (
	"context"
	"io"

	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/filestore"
	"github.com/sjgoldie/go-restgen/metadata"
)

// Common provides generic CRUD operations for any model type
type Common[T any] struct {
	store *datastore.Wrapper[T]
}

// DownloadResult contains the result of a Download operation.
// Either Reader is set (for proxy/streaming) or SignedURL is set (for redirect).
type DownloadResult struct {
	ContentType string
	Filename    string
	Size        int64
	Reader      io.ReadCloser // nil if SignedURL is set
	SignedURL   string        // non-empty for redirect
}

// GetAll retrieves all items of type T
// Returns items, total count (0 if not requested), sums (nil if not requested), cursor info (nil if not cursor mode), and error
func (s *Common[T]) GetAll(ctx context.Context) ([]*T, int, map[string]float64, *metadata.CursorInfo, error) {
	return s.store.GetAll(ctx)
}

// Get retrieves a single item of type T by ID
// The id parameter is a string to support both integer and UUID primary keys
func (s *Common[T]) Get(ctx context.Context, id string) (*T, error) {
	return s.store.Get(ctx, id)
}

// Create creates a new item of type T
func (s *Common[T]) Create(ctx context.Context, item T) (*T, error) {
	return s.store.Create(ctx, item)
}

// Update updates an existing item of type T
// The id parameter is a string to support both integer and UUID primary keys
func (s *Common[T]) Update(ctx context.Context, id string, item T) (*T, error) {
	return s.store.Update(ctx, id, item)
}

// Patch partially updates an existing item of type T.
// Identical to Update but passes OpPatch to validators and auditors.
func (s *Common[T]) Patch(ctx context.Context, id string, item T) (*T, error) {
	return s.store.Patch(ctx, id, item)
}

// Delete deletes an item of type T by ID
// The id parameter is a string to support both integer and UUID primary keys
func (s *Common[T]) Delete(ctx context.Context, id string) error {
	return s.store.Delete(ctx, id)
}

// GetByParentRelation retrieves a single item of type T via the parent's relation field
// The parentID is the ID of the parent, and the relation field is used to resolve the child's ID
// Security checks are preserved by calling the normal Get with the resolved child ID
func (s *Common[T]) GetByParentRelation(ctx context.Context, parentID string) (*T, error) {
	return s.store.GetByParentRelation(ctx, parentID)
}

// UpdateByParentRelation updates a single item of type T via the parent's relation field
// The parentID is the ID of the parent, and the relation field is used to resolve the child's ID
// Security checks are preserved by calling the normal Update with the resolved child ID
func (s *Common[T]) UpdateByParentRelation(ctx context.Context, parentID string, item T) (*T, error) {
	return s.store.UpdateByParentRelation(ctx, parentID, item)
}

// PatchByParentRelation patches a single item of type T via the parent's relation field.
// The parentID is the ID of the parent, and the relation field is used to resolve the child's ID.
// Security checks are preserved by calling the normal Patch with the resolved child ID.
func (s *Common[T]) PatchByParentRelation(ctx context.Context, parentID string, item T) (*T, error) {
	return s.store.PatchByParentRelation(ctx, parentID, item)
}

// StoreFile stores a file and returns the storage key.
// The caller is responsible for setting fields on the item.
func (s *Common[T]) StoreFile(ctx context.Context, file io.Reader, fileMeta filestore.FileMetadata) (string, error) {
	fs, err := filestore.Get()
	if err != nil {
		return "", err
	}
	return fs.Store(ctx, file, fileMeta)
}

// DeleteStoredFile deletes a file from file storage by its storage key.
func (s *Common[T]) DeleteStoredFile(ctx context.Context, storageKey string) error {
	if storageKey == "" {
		return nil
	}
	fs, err := filestore.Get()
	if err != nil {
		return err
	}
	return fs.Delete(ctx, storageKey)
}

// RetrieveFile retrieves a file's content stream from file storage.
func (s *Common[T]) RetrieveFile(ctx context.Context, storageKey string) (io.ReadCloser, error) {
	if storageKey == "" {
		return nil, filestore.ErrStorageKeyNotFound
	}
	fs, err := filestore.Get()
	if err != nil {
		return nil, err
	}
	reader, _, err := fs.Retrieve(ctx, storageKey)
	return reader, err
}

// Download retrieves a file for download, handling both proxy and signed URL modes.
// Returns a DownloadResult that indicates either streaming (Reader set) or redirect (SignedURL set).
// The storage implementation decides whether to return a signed URL or not.
func (s *Common[T]) Download(ctx context.Context, id string) (*DownloadResult, error) {
	item, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	fr, ok := any(item).(filestore.FileResource)
	if !ok {
		return nil, filestore.ErrStorageKeyNotFound
	}

	storageKey := fr.GetStorageKey()
	if storageKey == "" {
		return nil, filestore.ErrStorageKeyNotFound
	}

	storage, err := filestore.Get()
	if err != nil {
		return nil, err
	}

	// Try signed URL first - storage implementation decides if supported
	signedURL, err := storage.GenerateSignedURL(ctx, storageKey)
	if err != nil {
		return nil, err
	}
	if signedURL != "" {
		return &DownloadResult{SignedURL: signedURL}, nil
	}

	// Fall back to proxy mode
	reader, err := s.RetrieveFile(ctx, storageKey)
	if err != nil {
		return nil, err
	}

	return &DownloadResult{
		ContentType: fr.GetContentType(),
		Filename:    fr.GetFilename(),
		Size:        fr.GetSize(),
		Reader:      reader,
	}, nil
}

// ComputeIncludeCounts computes per-item child relation counts for the given items.
// Returns a map of relation name → {pk_string → count}.
func (s *Common[T]) ComputeIncludeCounts(ctx context.Context, items []*T, includeCounts []string) (map[string]map[string]int, error) {
	return s.store.ComputeIncludeCounts(ctx, items, includeCounts)
}

// BatchCreate creates multiple items in a single transaction.
// All items succeed or none do (all-or-nothing).
func (s *Common[T]) BatchCreate(ctx context.Context, items []T) ([]*T, error) {
	return s.store.BatchCreate(ctx, items)
}

// BatchUpdate updates multiple items in a single transaction.
// All items succeed or none do (all-or-nothing).
func (s *Common[T]) BatchUpdate(ctx context.Context, items []T) ([]*T, error) {
	return s.store.BatchUpdate(ctx, items)
}

// BatchPatch partially updates multiple items in a single transaction.
// Identical to BatchUpdate but passes OpPatch to validators and auditors.
func (s *Common[T]) BatchPatch(ctx context.Context, items []T) ([]*T, error) {
	return s.store.BatchPatch(ctx, items)
}

// BatchDelete deletes multiple items in a single transaction.
// All items succeed or none do (all-or-nothing).
// Items must have at least an ID field set.
func (s *Common[T]) BatchDelete(ctx context.Context, items []T) error {
	return s.store.BatchDelete(ctx, items)
}
