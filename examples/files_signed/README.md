# File Upload Example (Signed URL Mode)

This example demonstrates file upload and download using go-restgen with local filesystem storage. Files are downloaded directly via a static file server (simulating signed URL behavior).

## Features

- Posts with nested image attachments
- Multipart file upload
- Direct download (bypasses API, simulates signed URLs)
- Local filesystem storage
- SQLite in-memory database for metadata

## Running the Example

```bash
cd examples/files_signed
go run .
```

Files are stored in `./uploads/` directory and served via `/files/`.

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
| GET | /posts/{id}/images/{id} | Get image metadata (includes direct download URL) |
| DELETE | /posts/{id}/images/{id} | Delete an image |

**Note:** There is no `/download` endpoint in signed URL mode. The `download_url` points directly to `/files/{key}`.

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

### Get Image Metadata

```bash
curl http://localhost:8080/posts/1/images/1
```

Response includes a direct `download_url`:

```json
{
  "id": 1,
  "post_id": 1,
  "filename": "photo.jpg",
  "content_type": "image/jpeg",
  "size": 12345,
  "download_url": "http://localhost:8080/files/abc-123-uuid",
  "alt_text": "A beautiful photo",
  "created_at": "2025-01-01T00:00:00Z"
}
```

### Download an Image

Use the URL from the response:

```bash
curl -O "http://localhost:8080/files/abc-123-uuid"
```

## Signed URL Mode

This example uses **signed URL mode** where clients download directly from storage. In this example, we use a static file server to simulate this behavior. In production with S3/Minio, the `download_url` would be a time-limited signed URL.

Benefits:
- ✅ Reduces server load - downloads bypass your API
- ✅ Better for large files
- ✅ Scalable - CDN friendly

Trade-offs:
- ❌ No server-side auth on download (URL itself is the auth)
- ❌ URLs expire (in real implementations)
- ❌ Exposes storage backend to clients

## Security Considerations

With signed URLs in production:
- Authentication happens when generating the URL (via your API)
- Anyone with the URL can download the file until it expires
- URLs should be treated as secrets
- Set appropriate expiry times based on your use case

See the `files_proxy` example for proxy mode where all downloads go through your server with full auth control.

## Implementing Real Signed URLs

For S3 or Minio, the `GenerateSignedURL` method would use presigned URLs:

```go
func (s *S3Storage) GenerateSignedURL(ctx context.Context, key string) (string, error) {
    presignClient := s3.NewPresignClient(s.client)
    req, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
        Bucket: &s.bucket,
        Key:    &key,
    }, s3.WithPresignExpires(15*time.Minute))
    if err != nil {
        return "", err
    }
    return req.URL, nil
}
```
