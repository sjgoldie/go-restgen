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
    router.AsSingleRouteWithUpdate(""),

    // Query options (individual)
    router.WithFilters("Status", "Name"),
    router.WithSorts("Name", "CreatedAt"),
    router.WithPagination(20, 100),                // cursor-based (default)
    router.WithPagination(20, 100, router.OffsetMode), // offset-based (opt-in)
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
        Pagination:       router.CursorMode, // default; use router.OffsetMode for offset-based
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
    router.WithCustomPatch(customPatchFn),
    router.WithCustomDelete(customDeleteFn),

    // Custom batch handlers
    router.WithCustomBatchCreate(customBatchCreateFn),
    router.WithCustomBatchUpdate(customBatchUpdateFn),
    router.WithCustomBatchPatch(customBatchPatchFn),
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
) ([]*T, int, map[string]float64, *metadata.CursorInfo, error)

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

// PATCH /resource/{id} (partial update)
// Receives existing (before) and patched (after JSON overlay)
type CustomPatchFunc[T any] func(
    ctx context.Context,
    svc *service.Common[T],
    meta *metadata.TypeMetadata,
    auth *metadata.AuthInfo,
    id string,
    existing *T,
    patched T,
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

type CustomBatchPatchFunc[T any] func(
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
    // vc.Operation: metadata.OpCreate, OpUpdate, OpPatch, OpDelete
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
| Filter ops | `?filter[Age][gt]=18` | Operators: eq, neq, gt, gte, lt, lte, like, ilike, in, nin, bt, nbt |
| Relation exists | `?filter[Comments][exists]=true` | Filter by child existence (true/false) |
| Relation count | `?filter[Comments][count_gt]=5` | Filter by child count (count_eq, count_neq, count_gt, count_gte, count_lt, count_lte) |
| Include count | `?include_count=Comments` | Return per-item child counts in `counts` object |
| Sort | `?sort=Name,-CreatedAt` | `-` prefix for descending |
| Limit | `?limit=10` | Max results per page |
| After | `?after=<cursor>` | Next page (cursor from `pagination.next_cursor`) |
| Before | `?before=<cursor>` | Previous page (cursor from `pagination.prev_cursor`) |
| Offset | `?offset=20` | Skip results (switches to offset pagination) |
| Count | `?count=true` | Include `total_count` in `pagination` |
| Include | `?include=Posts.Comments` | Load relations (dot notation for nested) |
| Sum | `?sum=Price,Stock` | Returns in `sums` object in response body |

**Response envelope (GetAll):** `{"data": [...], "pagination": {...}, "sums": {...}, "counts": {...}}`.
Cursor mode: `has_more`, `next_cursor`, `prev_cursor`, `total_count`. Offset mode: `limit`, `offset`, `total_count`.
Batch responses: `{"data": [...]}`. Single-item responses: raw object (no envelope).

**Nested includes:** child direction needs `WithRelationName` at each level. Parent direction (e.g., `?include=Author`) auto-derived from `rel:belongs-to` tags. Auth is cumulative AND, ownership is cumulative OR.

**Relation filters and counts:** `filter[Relation][exists]` and `filter[Relation][count_*]` use correlated subqueries and require the relation to be in `AllowedIncludes`. `include_count` returns `counts: {"Relation": {"pk": count}}` in the response. Unauthorized relations are silently skipped.

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
router.MethodGet, router.MethodList, router.MethodPost, router.MethodPut, router.MethodPatch, router.MethodDelete
router.MethodAll           // Expands to Get, List, Post, Put, Patch, Delete (excludes batch)
router.MethodBatchCreate, router.MethodBatchUpdate, router.MethodBatchPatch, router.MethodBatchDelete
router.MethodAllWithBatch  // All including batch
router.ScopePublic         // No auth required
router.ScopeAuthOnly       // Auth required, no scope check
router.AuthInfoKey         // Typed context key for AuthInfo
```
