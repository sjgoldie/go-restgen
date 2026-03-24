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

    db, err := datastore.NewSQLite(":memory:")
    if err != nil {
        log.Fatal(err)
    }
    if err := datastore.Initialize(db); err != nil {
        log.Fatal(err)
    }
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
    router.PublicList(),                   // Only LIST (collection GET) is public

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
    router.WithJoinOn("NMI", "NMI"),  // custom join: child.NMI = parent.NMI (no belongs-to tag needed)
    router.WithSums("Price", "Stock"),  // enables ?sum=Price,Stock with X-Sum-* headers (works with any DB-numeric type including decimal.Decimal)
    router.WithAlternatePK("MyPK"),     // when PK field isn't named "ID"

    // Custom handlers
    router.WithCustomGet(customGetFn),
    router.WithCustomGetAll(customGetAllFn),
    router.WithCustomCreate(customCreateFn),
    router.WithCustomUpdate(customUpdateFn),
    router.WithCustomDelete(customDeleteFn),

    // File uploads (model must embed filestore.FileFields)
    router.AsFileResource(),

    // Request body limits
    router.WithMaxBodySize(1024),        // optional: max JSON body size in bytes (default: 1 MB)

    // Batch operations (enabled via auth methods)
    router.AllScopedWithBatch("admin"),  // all methods + batch for admin scope
    router.WithBatchLimit(100),          // optional: limit items per batch

    // Multi-tenant isolation
    router.WithTenantScope("OrgID"),  // auto-filter + auto-set tenant field from AuthInfo.TenantID
    router.IsTenantTable(),           // marks route as the tenant entity (PK = tenant ID)

    // Custom actions
    router.WithAction("publish", publishFn, router.AuthConfig{Scopes: []string{"user"}}),

    // Custom endpoints (any HTTP method, any return type)
    router.WithEndpoint("GET", "wf-status", getWorkflowStatusFn, router.AuthConfig{Scopes: []string{router.ScopePublic}}),
    router.WithEndpoint("POST", "pay", processPaymentFn, router.AuthConfig{Scopes: []string{"user"}}),

    // SSE endpoints (item-level, always GET)
    router.WithSSE("events", streamEventsFn, router.AuthConfig{Scopes: []string{router.ScopePublic}}),

    // Query shorthand (alternative to individual WithFilters/WithSorts/etc)
    router.WithQuery(router.QueryConfig{
        FilterableFields: []string{"Status", "Name"},
        SortableFields:   []string{"Name", "CreatedAt"},
        SummableFields:   []string{"Price"},
        DefaultSort:      "-CreatedAt",
        DefaultLimit:     20,
        MaxLimit:         100,
    }),
)

// Root-level endpoints (no parent model, registered outside RegisterRoutes)
router.RegisterRootEndpoint(b, "GET", "/system/info", getSystemInfoFn, router.AllPublic())
router.RegisterRootEndpoint(b, "POST", "/webhooks/notify", handleWebhookFn, router.AllScoped("admin"))

// Root-level SSE (no parent model, always GET)
router.RegisterRootSSE(b, "/events/system", streamSystemEventsFn, router.AllPublic())
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
                UserID:   userID,
                TenantID: tenantID, // For multi-tenant isolation (optional)
                Scopes:   scopes,
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

## Pattern: Multi-Tenant Isolation

```go
// Tenant entity (PK = tenant ID)
type Organization struct {
    bun.BaseModel `bun:"table:organizations"`
    ID            string `bun:"id,pk" json:"id"`
    Name          string `bun:"name,notnull" json:"name"`
}

// Tenant-scoped model (has OrgID field)
type Project struct {
    bun.BaseModel `bun:"table:projects"`
    ID            int    `bun:"id,pk,autoincrement" json:"id"`
    OrgID         string `bun:"org_id,notnull" json:"org_id"`
    OwnerID       string `bun:"owner_id,notnull" json:"owner_id"`
    Name          string `bun:"name,notnull" json:"name"`
}

// Tenant entity route: WHERE id = tenantID
router.RegisterRoutes[Organization](b, "/organizations",
    router.IsTenantTable(),
    router.IsAuthenticated(),
)

// Tenant-scoped route: auto-filters by OrgID, auto-sets on create/update
router.RegisterRoutes[Project](b, "/projects",
    router.WithTenantScope("OrgID"),
    router.AllWithOwnershipUnless([]string{"OwnerID"}, "admin"),
    func(b *router.Builder) {
        // Children inherit tenant scope automatically
        router.RegisterRoutes[Task](b, "/tasks", router.IsAuthenticated())
    },
)
```

Auth middleware must set `TenantID`:
```go
authInfo := &router.AuthInfo{
    UserID:   userID,
    TenantID: tenantID,  // Required for WithTenantScope / IsTenantTable routes
    Scopes:   scopes,
}
```

Behaviour: CREATE auto-sets tenant field, GET/LIST auto-filters, UPDATE re-enforces tenant field, cross-tenant returns 404, missing TenantID returns 401. Children inherit from parent.

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
) ([]*User, int, map[string]float64, error) {
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

## Pattern: Custom Endpoints (Anything Funcs)

Custom endpoints support any HTTP method and any return type. SSE variants stream events.

```go
// Item-level endpoint: METHOD /resource/{id}/{name}
// Handler signature: EndpointHandler[T]
func getWorkflowStatus(
    ctx context.Context,
    svc *service.Common[Order],
    meta *metadata.TypeMetadata,
    auth *metadata.AuthInfo,
    id string,
    item *Order,       // pre-fetched by framework
    payload []byte,    // raw request body
) (any, int, error) { // any return type + status code
    return &WorkflowStatus{OrderID: id, State: item.Status}, http.StatusOK, nil
}

router.RegisterRoutes[Order](b, "/orders",
    router.AllPublic(),
    router.WithEndpoint("GET", "wf-status", getWorkflowStatus, router.AuthConfig{
        Scopes: []string{router.ScopePublic},
    }),
)
// URL: GET /orders/{id}/wf-status

// Root-level endpoint: METHOD /any/path (no parent model)
// Handler signature: RootEndpointHandler
func getSystemInfo(
    ctx context.Context,
    auth *metadata.AuthInfo,
    r *http.Request,   // full request access
) (any, int, error) {
    return &SystemInfo{Version: "1.0.0"}, http.StatusOK, nil
}

router.RegisterRootEndpoint(b, "GET", "/system/info", getSystemInfo, router.AllPublic())

// Item-level SSE: GET /resource/{id}/{name}
// Handler signature: SSEFunc[T]
func streamOrderEvents(
    ctx context.Context,
    svc *service.Common[Order],
    meta *metadata.TypeMetadata,
    auth *metadata.AuthInfo,
    id string,
    item *Order,
    events chan<- handler.SSEEvent,
) error {
    events <- handler.SSEEvent{Event: "status", Data: map[string]string{"state": item.Status}, ID: "1"}
    return nil
}

router.RegisterRoutes[Order](b, "/orders",
    router.AllPublic(),
    router.WithSSE("events", streamOrderEvents, router.AuthConfig{
        Scopes: []string{router.ScopePublic},
    }),
)
// URL: GET /orders/{id}/events

// Root-level SSE: GET /any/path (no parent model)
// Handler signature: RootSSEFunc
func streamSystemEvents(
    ctx context.Context,
    auth *metadata.AuthInfo,
    r *http.Request,
    events chan<- handler.SSEEvent,
) error {
    events <- handler.SSEEvent{Event: "heartbeat", Data: map[string]string{"status": "ok"}}
    return nil
}

router.RegisterRootSSE(b, "/events/system", streamSystemEvents, router.AllPublic())
```

**SSEEvent fields:** `Event` (string, optional), `Data` (any, JSON-encoded), `ID` (string, optional).

**Response:** `(result, statusCode, nil)` → JSON + status code. `(nil, _, nil)` → 204. Status `0` defaults to `200`.

## Query Parameters

Built-in support on GetAll endpoints:

| Parameter | Example | Description |
|-----------|---------|-------------|
| Filter | `?filter[status]=active` | Exact match |
| Filter ops | `?filter[age][gt]=18` | Operators: eq, neq, gt, gte, lt, lte, like, in, nin, bt, nbt |
| Sort | `?sort=name,-created_at` | `-` prefix for descending |
| Limit | `?limit=10` | Max results |
| Offset | `?offset=20` | Skip results |
| Count | `?count=true` | Include X-Total-Count header |
| Include | `?include=Posts` or `?include=Posts.Comments` | Load relations (requires WithRelationName on child route). Dot notation for nested. |
| Sum | `?sum=Price,Stock` | Sum fields, returns X-Sum-Price, X-Sum-Stock headers (requires WithSums). Works with any DB-numeric type including `decimal.Decimal`. Bool fields return count of `true` values. DB validates types — non-numeric columns return a database error. |

**Filter operator details:**
- `in` - In list: `?filter[Status][in]=active,pending`
- `nin` - Not in list: `?filter[Status][nin]=deleted,archived`
- `bt` - Between (inclusive): `?filter[Age][bt]=18,65`
- `nbt` - Not between: `?filter[Price][nbt]=100,500`

**Nested includes (dot notation):**
- Child direction: `?include=Posts.Comments` — each level needs `WithRelationName` on its route
- Parent direction: `?include=Author` — auto-derived from `rel:belongs-to` tags, no `WithRelationName` needed
- Auth is cumulative AND (deeper levels blocked if parent fails), ownership is cumulative OR
- Middle-level auth failure silently omits everything below

## Error Handling

All errors are returned as structured JSON: `{"error": "not_found", "message": "Not Found"}`

```go
import apperrors "github.com/sjgoldie/go-restgen/errors"

// In custom handlers, return domain errors:
return nil, apperrors.ErrNotFound           // 404 {"error":"not_found"}
return nil, apperrors.ErrDuplicate          // 400 {"error":"duplicate"}
return nil, apperrors.ErrInvalidReference   // 400 {"error":"invalid_reference"}
return nil, apperrors.ErrUnavailable        // 503 {"error":"service_unavailable"}

// Custom validation error (message sent to client):
return nil, apperrors.NewValidationError("title cannot be empty")  // 400 {"error":"validation_error","message":"title cannot be empty"}
```

For custom middleware, use `handler.WriteError(w, statusCode, errorCode, message)`.

## Logging

Framework uses context-aware `slog`. You configure the level in your application.

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

Log levels: Error (DB failures), Warn (auth rejections, validation), Debug (client 4xx errors).

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
- [ ] Tenant isolation prevents cross-tenant access (if using WithTenantScope/IsTenantTable)

## External Database Connections

Use `NewPostgresWithDB` or `NewSQLiteWithDB` when you need to manage the database connection externally (e.g., Vault rotating credentials, custom connection pooling):

```go
// Create your own *sql.DB with custom configuration
sqlDB, err := sql.Open("pgx", connString)
if err != nil {
    log.Fatal(err)
}
// Configure pooling, timeouts, etc.
sqlDB.SetMaxOpenConns(25)
sqlDB.SetMaxIdleConns(5)

// Pass to go-restgen
db := datastore.NewPostgresWithDB(sqlDB)
if err := datastore.Initialize(db); err != nil {
    log.Fatal(err)
}

// IMPORTANT: Cleanup() will NOT close your *sql.DB
// You must close it yourself when done
defer sqlDB.Close()
```

**Key difference from `NewPostgres(dsn)`:**
- `NewPostgres(dsn)`: go-restgen owns the connection; `Cleanup()` closes it
- `NewPostgresWithDB(sqlDB)`: you own the connection; `Cleanup()` does nothing to it

Same pattern applies for SQLite with `NewSQLiteWithDB(sqlDB)`.
