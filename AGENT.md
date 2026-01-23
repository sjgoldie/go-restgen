# go-restgen Quick Reference

Concise guide for implementing REST APIs with go-restgen.

## Minimal Working Example

```go
package main

import (
    "context"
    "log"
    "log/slog"
    "net/http"
    "os"
    "time"

    "github.com/go-chi/chi/v5"
    "github.com/sjgoldie/go-restgen/datastore"
    "github.com/sjgoldie/go-restgen/router"
    "github.com/uptrace/bun"
)

type User struct {
    bun.BaseModel `bun:"table:users"`
    ID            int       `bun:"id,pk,autoincrement" json:"id"`
    Name          string    `bun:"name,notnull" json:"name"`
    Email         string    `bun:"email,notnull" json:"email"`
    CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
    UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
}

func (u *User) BeforeAppendModel(ctx context.Context, query bun.Query) error {
    now := time.Now()
    switch query.(type) {
    case *bun.InsertQuery:
        u.CreatedAt = now
        u.UpdatedAt = now
    case *bun.UpdateQuery:
        u.UpdatedAt = now
    }
    return nil
}

func main() {
    logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
    slog.SetDefault(logger)

    db, _ := datastore.NewSQLite(":memory:")
    datastore.Initialize(db)
    defer datastore.Cleanup()

    db.GetDB().NewCreateTable().Model((*User)(nil)).IfNotExists().Exec(context.Background())

    r := chi.NewRouter()
    r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        w.Write([]byte("OK"))
    })

    b := router.NewBuilder(r)
    router.RegisterRoutes[User](b, "/users", router.AllPublic())

    log.Fatal(http.ListenAndServe(":8080", r))
}
```

## Defining Models

```go
type Post struct {
    bun.BaseModel `bun:"table:posts"`
    ID            int64     `bun:"id,pk,autoincrement" json:"id"`
    Title         string    `bun:"title,notnull" json:"title"`
    UserID        string    `bun:"user_id,notnull" json:"user_id"`           // ownership field
    BlogID        int64     `bun:"blog_id,notnull,skipupdate" json:"blog_id"` // FK for nesting
    CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
    UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
}
```

**Key patterns:**
- Primary key: `bun:"id,pk,autoincrement"` for int, or use `uuid.UUID` type
- Foreign keys: include `skipupdate` to prevent modification
- Ownership: string field matching auth UserID
- Timestamps: use `BeforeAppendModel` hook (see example above)

## Route Registration

```go
router.RegisterRoutes[Model](builder, "/path",
    // Access control (pick one)
    router.AllPublic(),                    // No auth required
    router.AllScoped("admin"),             // Requires "admin" scope for all methods
    router.IsAuthenticated(),              // Just requires valid auth, no scope check

    // Per-method auth
    router.AuthConfig{
        Methods: []string{router.MethodGet, router.MethodList},
        Scopes:  []string{router.ScopePublic},
    },
    router.AuthConfig{
        Methods: []string{router.MethodPost, router.MethodPut, router.MethodDelete},
        Scopes:  []string{"admin"},
    },

    // Single resource (for belongs-to relations or /me endpoints)
    router.AsSingleRouteWithPut(""),       // GET and PUT only, ID from parent FK or custom handler

    // Ownership (users only see their data, admins bypass)
    router.AllWithOwnershipUnless([]string{"UserID"}, "admin"),

    // Query options
    router.WithFilters("Status", "Name"),
    router.WithSorts("Name", "CreatedAt"),
    router.WithPagination(20, 100),
    router.WithDefaultSort("-CreatedAt"),
    router.WithRelationName("Posts"),  // enables ?include=Posts on parent

    // Custom handlers
    router.WithCustomGet(customGetFn),
    router.WithCustomGetAll(customGetAllFn),
    router.WithCustomCreate(customCreateFn),
    router.WithCustomUpdate(customUpdateFn),
    router.WithCustomDelete(customDeleteFn),

    // File uploads (model must embed filestore.FileFields)
    router.AsFileResource(),

    // Batch operations (enabled via auth methods)
    router.AllScopedWithBatch("admin"),  // all methods + batch for admin scope
    router.WithBatchLimit(100),          // optional: limit items per batch

    // Custom actions
    router.WithAction("publish", publishFn, router.AuthConfig{Scopes: []string{"user"}}),
)
```

## Pattern: Nested Resources

```go
type Blog struct {
    bun.BaseModel `bun:"table:blogs"`
    ID            int64  `bun:"id,pk,autoincrement" json:"id"`
    Name          string `bun:"name,notnull" json:"name"`
}

type Post struct {
    bun.BaseModel `bun:"table:posts"`
    ID            int64  `bun:"id,pk,autoincrement" json:"id"`
    BlogID        int64  `bun:"blog_id,notnull,skipupdate" json:"blog_id"`
    Title         string `bun:"title,notnull" json:"title"`
}

b := router.NewBuilder(r)
router.RegisterRoutes[Blog](b, "/blogs", router.AllPublic(), func(b *router.Builder) {
    router.RegisterRoutes[Post](b, "/posts", router.AllPublic())
})
```

Creates routes:
- `GET/POST /blogs`
- `GET/PUT/DELETE /blogs/{blogId}`
- `GET/POST /blogs/{blogId}/posts`
- `GET/PUT/DELETE /blogs/{blogId}/posts/{postId}`

The framework automatically validates parent exists and sets `BlogID` on create.

## Pattern: Authentication & Ownership

```go
// Auth middleware (implement your own token validation)
func authMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        userID, scopes := validateToken(r.Header.Get("Authorization"))
        if userID != "" {
            authInfo := &router.AuthInfo{
                UserID: userID,
                Scopes: scopes,
            }
            ctx := context.WithValue(r.Context(), router.AuthInfoKey, authInfo)
            r = r.WithContext(ctx)
        }
        next.ServeHTTP(w, r)
    })
}

r.Use(authMiddleware)

// Routes with ownership (users only see their own posts, admins see all)
router.RegisterRoutes[Post](b, "/posts",
    router.AllWithOwnershipUnless([]string{"AuthorID"}, "admin"),
)
```

Users only see/modify posts where `AuthorID` matches their auth `UserID`. Admins bypass.

## Pattern: Custom Handlers

```go
// Get single item
func customGet(
    ctx context.Context,
    svc *service.Common[User],
    meta *metadata.TypeMetadata,
    auth *metadata.AuthInfo,
    id string,
) (*User, error) {
    // Custom logic here
    return svc.Get(ctx, id)
}

// Get all items
func customGetAll(
    ctx context.Context,
    svc *service.Common[User],
    meta *metadata.TypeMetadata,
    auth *metadata.AuthInfo,
) ([]*User, int, error) {
    // Custom logic here
    return svc.GetAll(ctx)
}

// Create item
func customCreate(
    ctx context.Context,
    svc *service.Common[User],
    meta *metadata.TypeMetadata,
    auth *metadata.AuthInfo,
    item User,
    file io.Reader,
    fileMeta filestore.FileMetadata,
) (*User, error) {
    // Validation, side effects, etc.
    return svc.Create(ctx, item)
}

// Update item
func customUpdate(
    ctx context.Context,
    svc *service.Common[User],
    meta *metadata.TypeMetadata,
    auth *metadata.AuthInfo,
    id string,
    item User,
) (*User, error) {
    return svc.Update(ctx, id, item)
}

// Delete item
func customDelete(
    ctx context.Context,
    svc *service.Common[User],
    meta *metadata.TypeMetadata,
    auth *metadata.AuthInfo,
    id string,
) error {
    return svc.Delete(ctx, id)
}

router.RegisterRoutes[User](b, "/users",
    router.AllScoped("user"),
    router.WithCustomGet(customGet),
    router.WithCustomCreate(customCreate),
)
```

## Pattern: File Uploads

```go
type Document struct {
    bun.BaseModel `bun:"table:documents"`
    ID            int64  `bun:"id,pk,autoincrement" json:"id"`
    Name          string `bun:"name,notnull" json:"name"`
    filestore.FileFields  // embeds StorageKey, Filename, ContentType, Size + getter/setters
}

// Implement FileStorage interface (your own storage backend)
type MyStorage struct {
    basePath string
}

func (s *MyStorage) Store(ctx context.Context, r io.Reader, meta filestore.FileMetadata) (string, error) {
    key := uuid.New().String()
    // save file to s.basePath/key
    return key, nil
}

func (s *MyStorage) Retrieve(ctx context.Context, key string) (io.ReadCloser, filestore.FileMetadata, error) {
    // return file reader and metadata
}

func (s *MyStorage) Delete(ctx context.Context, key string) error {
    // delete file
}

func (s *MyStorage) GenerateSignedURL(ctx context.Context, key string) (string, error) {
    return "", nil  // empty = proxy mode (download via /download endpoint)
    // or return URL for signed URL mode (direct download from storage)
}

// Initialize in main()
filestore.Initialize(&MyStorage{basePath: "./uploads"})

router.RegisterRoutes[Document](b, "/documents",
    router.AsFileResource(),
    router.AllScoped("user"),
)
```

Upload: `POST /documents` with multipart form (`file` field + optional `metadata` JSON field).
Download: `GET /documents/{id}/download` (proxy mode) or use `download_url` from response (signed URL mode).

## Pattern: Custom Actions

```go
func publishPost(
    ctx context.Context,
    svc *service.Common[Post],
    meta *metadata.TypeMetadata,
    auth *metadata.AuthInfo,
    id string,
    item *Post,
    payload []byte,
) (*Post, error) {
    item.Status = "published"
    item.PublishedAt = time.Now()
    return svc.Update(ctx, id, *item)
}

router.RegisterRoutes[Post](b, "/posts",
    router.AllScoped("user"),
    router.WithAction("publish", publishPost, router.AuthConfig{Scopes: []string{"user"}}),
)
```

URL: `POST /posts/{id}/publish`

## Query Parameters

Built-in support on GetAll endpoints:

| Parameter | Example | Description |
|-----------|---------|-------------|
| Filter | `?filter[status]=active` | Exact match |
| Filter ops | `?filter[age][gt]=18` | Operators: eq, neq, gt, gte, lt, lte, like |
| Sort | `?sort=name,-created_at` | `-` prefix for descending |
| Limit | `?limit=10` | Max results |
| Offset | `?offset=20` | Skip results |
| Count | `?count=true` | Include X-Total-Count header |
| Include | `?include=Posts` | Load relations (requires WithRelationName on child route) |

## Error Handling

```go
import apperrors "github.com/sjgoldie/go-restgen/errors"

// In custom handlers, return domain errors:
return nil, apperrors.ErrNotFound           // 404
return nil, apperrors.ErrDuplicate          // 400
return nil, apperrors.ErrInvalidReference   // 400
return nil, apperrors.ErrUnavailable        // 503

// Custom validation error (message sent to client):
return nil, apperrors.NewValidationError("title cannot be empty")  // 400
```

## Logging

Framework uses `slog`. Default level is Error (quiet).

```go
import "log/slog"

func main() {
    logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
        Level: slog.LevelWarn,
    }))
    slog.SetDefault(logger)
    // ...
}
```

Logs at Warn level: failed operations, missing parameters, decode errors.

## Testing Checklist

For each resource:
- [ ] `GET /resources` returns list
- [ ] `GET /resources/{id}` returns single item
- [ ] `POST /resources` creates item
- [ ] `PUT /resources/{id}` updates item
- [ ] `DELETE /resources/{id}` deletes item
- [ ] Unauthorized requests are rejected
- [ ] Ownership filters work correctly
- [ ] Nested routes validate parent exists
- [ ] Query params (filter, sort, limit) work
