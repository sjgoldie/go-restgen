# File Upload Example (Proxy Mode)

This example demonstrates file upload and download using go-restgen with local filesystem storage. Files are streamed through the server (proxy mode).

## Features

- Posts with nested image attachments
- Multipart file upload
- Proxy download (files stream through server)
- Local filesystem storage
- SQLite in-memory database for metadata

## Running the Example

```bash
cd examples/files_proxy
go run .
```

Files are stored in `./uploads/` directory.

## API Endpoints

### Posts

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | /posts | Create a post |
| GET | /posts | List posts |
| GET | /posts/{id} | Get a post |
| PUT | /posts/{id} | Update a post |
| DELETE | /posts/{id} | Delete a post |

### Images (nested under posts)

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | /posts/{id}/images | Upload an image (multipart) |
| GET | /posts/{id}/images | List images |
| GET | /posts/{id}/images/{id} | Get image metadata (includes download URL) |
| GET | /posts/{id}/images/{id}/download | Download image binary |
| DELETE | /posts/{id}/images/{id} | Delete an image |

## Usage Examples

### Create a Post

```bash
curl -X POST http://localhost:8080/posts \
  -H "Content-Type: application/json" \
  -d '{"title": "My First Post", "content": "Hello world!"}'
```

### Upload an Image

```bash
curl -X POST http://localhost:8080/posts/1/images \
  -F "file=@photo.jpg" \
  -F 'metadata={"alt_text":"A beautiful photo"}'
```

### List Images

```bash
curl http://localhost:8080/posts/1/images
```

Response includes `download_url` for each image:

```json
[
  {
    "id": 1,
    "post_id": 1,
    "filename": "photo.jpg",
    "content_type": "image/jpeg",
    "size": 12345,
    "download_url": "/posts/1/images/1/download",
    "alt_text": "A beautiful photo",
    "created_at": "2025-01-01T00:00:00Z"
  }
]
```

### Download an Image

```bash
curl -O http://localhost:8080/posts/1/images/1/download
```

### Delete an Image

```bash
curl -X DELETE http://localhost:8080/posts/1/images/1
```

## Storage Implementation

The `storage.go` file contains a local filesystem implementation of `filestore.FileStorage`:

```go
type LocalStorage struct {
    basePath  string
    urlPrefix string
}

func (s *LocalStorage) Store(ctx context.Context, r io.Reader, meta filestore.FileMetadata) (string, error)
func (s *LocalStorage) Retrieve(ctx context.Context, key string) (io.ReadCloser, filestore.FileMetadata, error)
func (s *LocalStorage) Delete(ctx context.Context, key string) error
func (s *LocalStorage) GenerateSignedURL(ctx context.Context, key string) (string, error)
```

## Proxy vs Signed URL Mode

This example uses **proxy mode** where file downloads stream through the server. This:

- ✅ Allows full auth/security control on downloads
- ✅ Hides storage backend from clients
- ❌ All download traffic goes through your server

See the `files_signed` example for signed URL mode where clients download directly from storage.

## Implementing for S3/Minio

To use S3 or Minio instead of local storage, implement the `filestore.FileStorage` interface:

```go
type S3Storage struct {
    client *s3.Client
    bucket string
}

func (s *S3Storage) Store(ctx context.Context, r io.Reader, meta filestore.FileMetadata) (string, error) {
    key := uuid.New().String()
    _, err := s.client.PutObject(ctx, &s3.PutObjectInput{
        Bucket:      &s.bucket,
        Key:         &key,
        Body:        r,
        ContentType: &meta.ContentType,
    })
    return key, err
}

// ... implement other methods
```
