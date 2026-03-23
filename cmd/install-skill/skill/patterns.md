# go-restgen Detailed Patterns

## All Route Registration Options

```go
router.RegisterRoutes[Model](builder, "/path",
    // Access control (pick one)
    router.AllPublic(),
    router.AllScoped("admin"),
    router.IsAuthenticated(),
    router.PublicList(),
    router.PublicGet(),
    router.PublicReadOnly(),
    router.AllPublicWithBatch(),
    router.AllScopedWithBatch("admin"),

    // Per-method auth
    router.AuthConfig{
        Methods: []string{router.MethodGet, router.MethodList},
        Scopes:  []string{router.ScopePublic},
    },

    // Ownership
    router.AllWithOwnershipUnless([]string{"UserID"}, "admin"),

    // Single resource (belongs-to or /me)
    router.AsSingleRoute("AuthorID"),
    router.AsSingleRouteWithPut(""),

    // Query options (individual)
    router.WithFilters("Status", "Name"),
    router.WithSorts("Name", "CreatedAt"),
    router.WithPagination(20, 100),
    router.WithDefaultSort("-CreatedAt"),
    router.WithSums("Price", "Stock"),

    // Query options (combined)
    router.WithQuery(router.QueryConfig{
        FilterableFields: []string{"Status", "Name"},
        SortableFields:   []string{"Name", "CreatedAt"},
        SummableFields:   []string{"Price"},
        DefaultSort:      "-CreatedAt",
        DefaultLimit:     20,
        MaxLimit:         100,
    }),

    // Relations
    router.WithRelationName("Posts"),
    router.WithJoinOn("NMI", "NMI"),
    router.WithAlternatePK("MyPK"),

    // Custom CRUD handlers
    router.WithCustomGet(customGetFn),
    router.WithCustomGetAll(customGetAllFn),
    router.WithCustomCreate(customCreateFn),
    router.WithCustomUpdate(customUpdateFn),
    router.WithCustomDelete(customDeleteFn),

    // Custom batch handlers
    router.WithCustomBatchCreate(customBatchCreateFn),
    router.WithCustomBatchUpdate(customBatchUpdateFn),
    router.WithCustomBatchDelete(customBatchDeleteFn),
    router.WithBatchLimit(100),

    // Validation and audit
    router.WithValidator(validatorFn),
    router.WithAudit(auditFn),

    // File uploads (model must embed filestore.FileFields)
    router.AsFileResource(),

    // Multi-tenant isolation
    router.WithTenantScope("OrgID"),
    router.IsTenantTable(),

    // Actions (POST /resource/{id}/{name})
    router.WithAction("publish", publishFn, router.AuthConfig{Scopes: []string{"user"}}),

    // Custom endpoints (any method, any return type)
    router.WithEndpoint("GET", "status", endpointFn, router.AuthConfig{Scopes: []string{router.ScopePublic}}),

    // SSE endpoints (always GET)
    router.WithSSE("events", sseFn, router.AuthConfig{Scopes: []string{router.ScopePublic}}),

    // Nesting callback
    func(b *router.Builder) {
        router.RegisterRoutes[Child](b, "/children", router.AllPublic())
    },
)

// Root-level (outside RegisterRoutes)
router.RegisterRootEndpoint(b, "GET", "/system/info", rootEndpointFn, router.AllPublic())
router.RegisterRootSSE(b, "/events/system", rootSSEFn, router.AllPublic())
```

## Handler Signatures

### Custom CRUD Handlers

```go
// GET /resource/{id}
type CustomGetFunc[T any] func(
    ctx context.Context,
    svc *service.Common[T],
    meta *metadata.TypeMetadata,
    auth *metadata.AuthInfo,
    id string,
) (*T, error)

// GET /resource (list)
type CustomGetAllFunc[T any] func(
    ctx context.Context,
    svc *service.Common[T],
    meta *metadata.TypeMetadata,
    auth *metadata.AuthInfo,
) ([]*T, int, map[string]float64, error)

// POST /resource
type CustomCreateFunc[T any] func(
    ctx context.Context,
    svc *service.Common[T],
    meta *metadata.TypeMetadata,
    auth *metadata.AuthInfo,
    item T,
    file io.Reader,
    fileMeta filestore.FileMetadata,
) (*T, error)

// PUT /resource/{id}
type CustomUpdateFunc[T any] func(
    ctx context.Context,
    svc *service.Common[T],
    meta *metadata.TypeMetadata,
    auth *metadata.AuthInfo,
    id string,
    item T,
) (*T, error)

// DELETE /resource/{id}
type CustomDeleteFunc[T any] func(
    ctx context.Context,
    svc *service.Common[T],
    meta *metadata.TypeMetadata,
    auth *metadata.AuthInfo,
    id string,
) error
```

### Action Handler

```go
// POST /resource/{id}/{name}
type ActionFunc[T any] func(
    ctx context.Context,
    svc *service.Common[T],
    meta *metadata.TypeMetadata,
    auth *metadata.AuthInfo,
    id string,
    item *T,
    payload []byte,
) (*T, error)
```

### Endpoint Handlers (Anything Funcs)

```go
// Item-level: METHOD /resource/{id}/{name}
// Returns any type + explicit status code
type EndpointHandler[T any] func(
    ctx context.Context,
    svc *service.Common[T],
    meta *metadata.TypeMetadata,
    auth *metadata.AuthInfo,
    id string,
    item *T,
    payload []byte,
) (any, int, error)

// Root-level: METHOD /any/path
// No parent model, raw request access
type RootEndpointHandler func(
    ctx context.Context,
    auth *metadata.AuthInfo,
    r *http.Request,
) (any, int, error)
```

Endpoint response rules:
- `(result, statusCode, nil)` -> JSON + status code
- `(result, 0, nil)` -> 200 OK + JSON (0 defaults to 200)
- `(nil, _, nil)` -> 204 No Content
- `(_, _, error)` -> error response

### SSE Handlers

```go
// Item-level: GET /resource/{id}/{name}
type SSEFunc[T any] func(
    ctx context.Context,
    svc *service.Common[T],
    meta *metadata.TypeMetadata,
    auth *metadata.AuthInfo,
    id string,
    item *T,
    events chan<- handler.SSEEvent,
) error

// Root-level: GET /any/path
type RootSSEFunc func(
    ctx context.Context,
    auth *metadata.AuthInfo,
    r *http.Request,
    events chan<- handler.SSEEvent,
) error

// SSEEvent struct
type SSEEvent struct {
    Event string // Event type (omitted if empty)
    Data  any    // JSON-encoded by framework
    ID    string // Event ID (omitted if empty)
}
```

### Batch Handlers

```go
type CustomBatchCreateFunc[T any] func(
    ctx context.Context,
    svc *service.Common[T],
    meta *metadata.TypeMetadata,
    auth *metadata.AuthInfo,
    items []T,
) ([]*T, error)

type CustomBatchUpdateFunc[T any] func(
    ctx context.Context,
    svc *service.Common[T],
    meta *metadata.TypeMetadata,
    auth *metadata.AuthInfo,
    items []T,
) ([]*T, error)

type CustomBatchDeleteFunc[T any] func(
    ctx context.Context,
    svc *service.Common[T],
    meta *metadata.TypeMetadata,
    auth *metadata.AuthInfo,
    items []T,
) error
```

### Validator and Audit

```go
// Validator — return error to reject with 400
router.WithValidator(func(vc metadata.ValidationContext[T]) error {
    // vc.Operation: metadata.OpCreate, OpUpdate, OpDelete
    // vc.New: incoming item (nil for delete)
    // vc.Old: existing item (nil for create)
    // vc.Ctx: request context
    return nil
})

// Audit — return a model to insert in same transaction, nil to skip
router.WithAudit(func(ac metadata.AuditContext[T]) any {
    // ac.Operation, ac.New, ac.Old, ac.Ctx (same as validator)
    return &AuditLog{...}
})
```

## Query Parameters

| Parameter | Example | Description |
|-----------|---------|-------------|
| Filter | `?filter[Status]=active` | Exact match |
| Filter ops | `?filter[Age][gt]=18` | Operators: eq, neq, gt, gte, lt, lte, like, in, nin, bt, nbt |
| Sort | `?sort=Name,-CreatedAt` | `-` prefix for descending |
| Limit | `?limit=10` | Max results |
| Offset | `?offset=20` | Skip results |
| Count | `?count=true` | X-Total-Count header |
| Include | `?include=Posts.Comments` | Load relations (dot notation for nested) |
| Sum | `?sum=Price,Stock` | X-Sum-Price, X-Sum-Stock headers |

**Nested includes:** child direction needs `WithRelationName` at each level. Parent direction (e.g., `?include=Author`) auto-derived from `rel:belongs-to` tags. Auth is cumulative AND, ownership is cumulative OR.

## Error Handling

```go
import apperrors "github.com/sjgoldie/go-restgen/errors"

return nil, apperrors.ErrNotFound                          // 404
return nil, apperrors.ErrDuplicate                         // 400
return nil, apperrors.ErrInvalidReference                  // 400
return nil, apperrors.ErrUnavailable                       // 503
return nil, apperrors.NewValidationError("invalid input")  // 400 with message
```

## File Resources

Model embeds `filestore.FileFields` (StorageKey, Filename, ContentType, Size):

```go
type Document struct {
    bun.BaseModel    `bun:"table:documents"`
    ID               int    `bun:"id,pk,autoincrement" json:"id"`
    filestore.FileFields
    AltText          string `bun:"alt_text" json:"alt_text,omitempty"`
}
```

Upload: `POST /documents` with multipart form (`file` field + optional `metadata` JSON field).
Download: `GET /documents/{id}/download` (proxy streams, signed URL redirects 307).

## Multi-Tenant Isolation

```go
// Tenant entity: PK = tenant ID
router.RegisterRoutes[Organization](b, "/organizations",
    router.IsTenantTable(),
    router.IsAuthenticated(),
)

// Tenant-scoped: auto-filters + auto-sets tenant field
router.RegisterRoutes[Project](b, "/projects",
    router.WithTenantScope("OrgID"),
    router.AllWithOwnershipUnless([]string{"OwnerID"}, "admin"),
)
```

Auth middleware must set `TenantID` on `AuthInfo`.

## Database Setup

```go
// SQLite (in-memory for dev/test)
db, _ := datastore.NewSQLite(":memory:")

// PostgreSQL
db, _ := datastore.NewPostgres("postgres://user:pass@localhost:5432/dbname?sslmode=disable")

// External connection (you own the *sql.DB)
db := datastore.NewPostgresWithDB(sqlDB)

// Always:
datastore.Initialize(db)
defer datastore.Cleanup()
```

## Auth Method Constants

```go
router.MethodGet, router.MethodList, router.MethodPost, router.MethodPut, router.MethodDelete
router.MethodAll           // Expands to Get, List, Post, Put, Delete (excludes batch)
router.MethodBatchCreate, router.MethodBatchUpdate, router.MethodBatchDelete
router.MethodAllWithBatch  // All including batch
router.ScopePublic         // No auth required
router.ScopeAuthOnly       // Auth required, no scope check
router.AuthInfoKey         // Typed context key for AuthInfo
```
