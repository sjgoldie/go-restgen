package filestore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
)

// ErrStorageKeyNotFound is returned when a file resource doesn't have a storage key
var ErrStorageKeyNotFound = errors.New("storage key not found on file resource")

// FileMetadata contains metadata about a stored file
type FileMetadata struct {
	ContentType string
	Size        int64
	Filename    string
}

// FileStorage defines the interface for file storage backends
type FileStorage interface {
	// Store saves file content and returns a generated storage key
	Store(ctx context.Context, r io.Reader, meta FileMetadata) (key string, err error)

	// Retrieve returns the file content and metadata for a given key
	Retrieve(ctx context.Context, key string) (io.ReadCloser, FileMetadata, error)

	// Delete removes a file from storage
	Delete(ctx context.Context, key string) error

	// GenerateSignedURL returns a signed URL for direct access to the file.
	// If the storage supports signed URLs, it returns the URL.
	// If not supported, it returns an empty string (not an error).
	// Errors are only returned for actual failures (e.g., network issues).
	GenerateSignedURL(ctx context.Context, key string) (string, error)
}

var (
	// singleton instance for file storage
	storage FileStorage
	// once ensures Initialize is only called once
	once sync.Once
	// initErr stores any initialization error
	initErr error
)

// Initialize sets the global file storage singleton
// This must be called during application startup before any file handlers are used
// It is safe to call from multiple goroutines, but only the first call will take effect
func Initialize(fs FileStorage) error {
	once.Do(func() {
		if fs == nil {
			initErr = fmt.Errorf("file storage cannot be nil")
			return
		}
		storage = fs
	})
	return initErr
}

// Get returns the singleton file storage instance
// Returns an error if Initialize() hasn't been called
func Get() (FileStorage, error) {
	if storage == nil {
		return nil, fmt.Errorf("file storage not initialized - call filestore.Initialize() first")
	}
	return storage, nil
}

// IsInitialized returns true if file storage has been initialized
func IsInitialized() bool {
	return storage != nil
}
