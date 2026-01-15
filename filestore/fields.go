package filestore

// FileResource is the interface that file-backed models must implement
type FileResource interface {
	SetStorageKey(key string)
	GetStorageKey() string
	SetContentType(ct string)
	GetContentType() string
	SetSize(size int64)
	GetSize() int64
	SetFilename(name string)
	GetFilename() string
}

// FileFields provides a default implementation of FileResource
// Embed this in your model for automatic file field handling
type FileFields struct {
	StorageKey  string `bun:"storage_key,notnull" json:"-"`
	Filename    string `bun:"filename,notnull" json:"filename"`
	ContentType string `bun:"content_type,notnull" json:"content_type"`
	Size        int64  `bun:"size,notnull" json:"size"`
}

func (f *FileFields) SetStorageKey(key string) { f.StorageKey = key }
func (f *FileFields) GetStorageKey() string    { return f.StorageKey }
func (f *FileFields) SetContentType(ct string) { f.ContentType = ct }
func (f *FileFields) GetContentType() string   { return f.ContentType }
func (f *FileFields) SetSize(size int64)       { f.Size = size }
func (f *FileFields) GetSize() int64           { return f.Size }
func (f *FileFields) SetFilename(name string)  { f.Filename = name }
func (f *FileFields) GetFilename() string      { return f.Filename }
