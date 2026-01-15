//nolint:gosec,gocritic // Example code - simplified for demonstration
package main

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/sjgoldie/go-restgen/filestore"
)

// LocalStorage implements filestore.FileStorage using the local filesystem
// For signed URL mode, it generates a direct file URL (simulating signed URLs)
type LocalStorage struct {
	basePath  string
	urlPrefix string
}

// NewLocalStorage creates a new LocalStorage instance
func NewLocalStorage(basePath, urlPrefix string) (*LocalStorage, error) {
	// Ensure directory exists
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, err
	}
	return &LocalStorage{
		basePath:  basePath,
		urlPrefix: urlPrefix,
	}, nil
}

// Store saves file content and returns a generated storage key
func (s *LocalStorage) Store(ctx context.Context, r io.Reader, meta filestore.FileMetadata) (string, error) {
	// Generate unique key using UUID
	key := uuid.New().String()

	// Create file
	filePath := filepath.Join(s.basePath, key)
	f, err := os.Create(filePath)
	if err != nil {
		return "", err
	}

	// Copy content
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		_ = os.Remove(filePath)
		return "", err
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(filePath)
		return "", err
	}

	return key, nil
}

// Retrieve returns the file content and metadata for a given key
func (s *LocalStorage) Retrieve(ctx context.Context, key string) (io.ReadCloser, filestore.FileMetadata, error) {
	filePath := filepath.Join(s.basePath, key)

	f, err := os.Open(filePath)
	if err != nil {
		return nil, filestore.FileMetadata{}, err
	}

	stat, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, filestore.FileMetadata{}, err
	}

	meta := filestore.FileMetadata{
		Size: stat.Size(),
	}

	return f, meta, nil
}

// Delete removes a file from storage
func (s *LocalStorage) Delete(ctx context.Context, key string) error {
	filePath := filepath.Join(s.basePath, key)
	return os.Remove(filePath)
}

// GenerateSignedURL returns a direct URL for the file
// In a real implementation, this would generate a time-limited signed URL
// For this example, it returns a static file server URL
func (s *LocalStorage) GenerateSignedURL(ctx context.Context, key string) (string, error) {
	return s.urlPrefix + "/" + key, nil
}
