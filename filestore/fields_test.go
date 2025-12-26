package filestore

import (
	"testing"
)

func TestFileFields_SettersAndGetters(t *testing.T) {
	f := &FileFields{}

	// Test StorageKey
	f.SetStorageKey("test-key-123")
	if got := f.GetStorageKey(); got != "test-key-123" {
		t.Errorf("GetStorageKey() = %v, want %v", got, "test-key-123")
	}

	// Test ContentType
	f.SetContentType("image/png")
	if got := f.GetContentType(); got != "image/png" {
		t.Errorf("GetContentType() = %v, want %v", got, "image/png")
	}

	// Test Size
	f.SetSize(12345)
	if got := f.GetSize(); got != 12345 {
		t.Errorf("GetSize() = %v, want %v", got, 12345)
	}

	// Test Filename
	f.SetFilename("photo.png")
	if got := f.GetFilename(); got != "photo.png" {
		t.Errorf("GetFilename() = %v, want %v", got, "photo.png")
	}
}

func TestFileFields_ImplementsFileResource(t *testing.T) {
	var _ FileResource = (*FileFields)(nil)
}

func TestFileFields_ZeroValues(t *testing.T) {
	f := &FileFields{}

	if got := f.GetStorageKey(); got != "" {
		t.Errorf("GetStorageKey() = %v, want empty string", got)
	}
	if got := f.GetContentType(); got != "" {
		t.Errorf("GetContentType() = %v, want empty string", got)
	}
	if got := f.GetSize(); got != 0 {
		t.Errorf("GetSize() = %v, want 0", got)
	}
	if got := f.GetFilename(); got != "" {
		t.Errorf("GetFilename() = %v, want empty string", got)
	}
}
