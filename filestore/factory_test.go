package filestore

import (
	"context"
	"io"
	"testing"
)

// mockStorage is a test implementation of FileStorage
type mockStorage struct{}

func (m *mockStorage) Store(_ context.Context, _ io.Reader, _ FileMetadata) (string, error) {
	return "test-key", nil
}

func (m *mockStorage) Retrieve(_ context.Context, _ string) (io.ReadCloser, FileMetadata, error) {
	return nil, FileMetadata{}, nil
}

func (m *mockStorage) Delete(_ context.Context, _ string) error {
	return nil
}

func (m *mockStorage) GenerateSignedURL(_ context.Context, _ string) (string, error) {
	return "https://example.com/signed", nil
}

// TestFileStoreSingleton tests the singleton behavior in the order it would
// be used in a real application: uninitialized -> initialized -> subsequent calls
func TestFileStoreSingleton(t *testing.T) {
	// Step 1: Before initialization, everything should return errors
	t.Run("before initialization", func(t *testing.T) {
		if IsInitialized() {
			t.Skip("singleton already initialized by another test")
		}

		_, err := Get()
		if err == nil {
			t.Error("Get() should return error when not initialized")
		}

		_, err = GetMode()
		if err == nil {
			t.Error("GetMode() should return error when not initialized")
		}
	})

	// Step 2: Initialize with valid storage
	t.Run("initialize with valid storage", func(t *testing.T) {
		if IsInitialized() {
			t.Skip("singleton already initialized by another test")
		}

		ms := &mockStorage{}
		if err := Initialize(ms, StorageProxy); err != nil {
			t.Fatalf("Initialize() error = %v", err)
		}

		if !IsInitialized() {
			t.Error("IsInitialized() = false after Initialize()")
		}
	})

	// Step 3: After initialization, Get and GetMode should work
	t.Run("after initialization", func(t *testing.T) {
		if !IsInitialized() {
			t.Skip("singleton not initialized")
		}

		fs, err := Get()
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if fs == nil {
			t.Error("Get() returned nil storage")
		}

		m, err := GetMode()
		if err != nil {
			t.Fatalf("GetMode() error = %v", err)
		}
		if m != StorageProxy {
			t.Errorf("GetMode() = %v, want StorageProxy", m)
		}
	})

	// Step 4: Second Initialize call should be ignored (sync.Once behavior)
	t.Run("second initialize is ignored", func(t *testing.T) {
		if !IsInitialized() {
			t.Skip("singleton not initialized")
		}

		// Try to reinitialize with different mode - should be ignored
		err := Initialize(&mockStorage{}, StorageSignedURL)
		if err != nil {
			t.Fatalf("Second Initialize() error = %v", err)
		}

		// Mode should still be StorageProxy from first init
		m, _ := GetMode()
		if m != StorageProxy {
			t.Errorf("Mode changed after second Initialize(), got %v want StorageProxy", m)
		}
	})
}

func TestInitializeWithNil(t *testing.T) {
	// This test verifies nil storage returns error
	// Note: If singleton is already initialized, this will be a no-op due to sync.Once
	// The error is only returned if this is the first Initialize call
	if IsInitialized() {
		t.Skip("singleton already initialized, cannot test nil initialization")
	}

	err := Initialize(nil, StorageProxy)
	if err == nil {
		t.Error("Initialize(nil) should return error")
	}
}
