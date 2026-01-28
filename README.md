# go-restgen

[![Go Reference](https://pkg.go.dev/badge/github.com/sjgoldie/go-restgen.svg)](https://pkg.go.dev/github.com/sjgoldie/go-restgen)
[![Go Report Card](https://goreportcard.com/badge/github.com/sjgoldie/go-restgen)](https://goreportcard.com/report/github.com/sjgoldie/go-restgen)
[![codecov](https://codecov.io/github/sjgoldie/go-restgen/graph/badge.svg?token=Q2FFGVF0WH)](https://codecov.io/github/sjgoldie/go-restgen)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A lightweight, type-safe REST API framework for Go that leverages generics to automatically generate CRUD endpoints. Build production-ready REST APIs with minimal boilerplate while maintaining full type safety.

## Disclosure

I have leaned heavily on Claude Code (https://www.claude.com/product/claude-code) to build out this package, especially to do test and document generation, though the concept, architecture, and design are my own. If you are morally against AI assistance in coding and thus do not want to use this package, no problems.

## Features

- 🚀 **Zero boilerplate** - Generate full CRUD APIs with minimal code
- 🔒 **Type-safe** - Leverage Go generics for compile-time type checking
- 🔐 **Granular auth** - Flexible authentication, authorization, and ownership controls
- 🌳 **Nested resources** - Automatic parent-child relationships with full chain validation
- 🗄️ **Database agnostic** - Supports PostgreSQL and SQLite out of the box
- 🏗️ **Production-ready** - Built on battle-tested libraries (Chi router, Bun ORM)
- 📦 **Composable** - Mix generated routes with custom handlers
- 🧪 **Testable** - SQLite in-memory database for fast tests
- 🛡️ **Secure by default** - Blocked unless explicitly configured, path IDs always take precedence

## Installation

```bash
go get github.com/sjgoldie/go-restgen
```

## Quick Start

See the [simple example](./examples/simple) for a simple working example to get started.

## Using with AI Coding Agents

The [AGENT.md](./AGENT.md) file provides a concise reference optimized for AI coding assistants (Claude, Copilot, etc.). Point your AI agent to this file for quick, accurate go-restgen implementations.

**Sample prompt:**
```
Read AGENT.md, then create a REST API with these resources:
- Products (name, price, category, in_stock boolean)
- Reviews (belongs to Product, rating 1-5, comment, author_name)

Requirements:
- Products: public read, admin-only write
- Reviews: public read, authenticated users can create
- Support filtering by category and in_stock
- Support sorting by price and name
```

## Database Setup

### PostgreSQL

```go
import "github.com/sjgoldie/go-restgen/datastore"

db, err := datastore.NewPostgres("postgres://user:pass@localhost:5432/dbname?sslmode=disable")
if err != nil {
    log.Fatal(err)
}
datastore.Initialize(db)
defer datastore.Cleanup()
```

### SQLite

```go
import "github.com/sjgoldie/go-restgen/datastore"

// In-memory database (perfect for testing)
db, err := datastore.NewSQLite(":memory:")
if err != nil {
    log.Fatal(err)
}
datastore.Initialize(db)
defer datastore.Cleanup()

// Or file-based database
db, err := datastore.NewSQLite("./data.db")
```

### Using an External Database Connection

Use `NewPostgresWithDB` or `NewSQLiteWithDB` when you need to manage the database connection externally, such as with Vault rotating credentials or custom connection pooling:

```go
import (
    "database/sql"
    "github.com/sjgoldie/go-restgen/datastore"
    _ "github.com/jackc/pgx/v5/stdlib"
)

// Create and configure your own *sql.DB
sqlDB, err := sql.Open("pgx", "postgres://user:pass@localhost:5432/dbname")
if err != nil {
    log.Fatal(err)
}
sqlDB.SetMaxOpenConns(25)
sqlDB.SetMaxIdleConns(5)

// Pass to go-restgen
db := datastore.NewPostgresWithDB(sqlDB)
datastore.Initialize(db)

// IMPORTANT: You own the connection - close it yourself when done
defer sqlDB.Close()
```

**Connection ownership:**
- `NewPostgres(dsn)` / `NewSQLite(dsn)`: go-restgen owns the connection; `Cleanup()` closes it
- `NewPostgresWithDB(sqlDB)` / `NewSQLiteWithDB(sqlDB)`: you own the connection; `Cleanup()` does NOT close it - you must close it yourself

## Primary Key Types

go-restgen supports both integer and UUID primary keys. The framework automatically detects and handles the PK type based on your model definition.

### Integer Primary Keys (Default)

```go
type Blog struct {
    bun.BaseModel `bun:"table:blogs"`
    ID            int       `bun:"id,pk,autoincrement" json:"id"`
    Name          string    `bun:"name,notnull" json:"name"`
}
```

### UUID Primary Keys

```go
import "github.com/google/uuid"

type Blog struct {
    bun.BaseModel `bun:"table:blogs"`
    ID            uuid.UUID `bun:"id,pk,type:uuid" json:"id"`
    Name          string    `bun:"name,notnull" json:"name"`
}

// Use BeforeAppendModel hook to generate UUIDs
func (b *Blog) BeforeAppendModel(ctx context.Context, query bun.Query) error {
    if _, ok := query.(*bun.InsertQuery); ok {
        if b.ID == uuid.Nil {
            b.ID = uuid.New()
        }
    }
    return nil
}
```

**Important for nested resources with UUID foreign keys:**

Foreign key fields must include `skipupdate` to prevent them being overwritten during updates:

```go
type Post struct {
    bun.BaseModel `bun:"table:posts"`
    ID            uuid.UUID `bun:"id,pk,type:uuid" json:"id"`
    BlogID        uuid.UUID `bun:"blog_id,notnull,type:uuid,skipupdate" json:"blog_id"`  // Note: skipupdate
    Blog          *Blog     `bun:"rel:belongs-to,join:blog_id=id" json:"blog,omitempty"`
    Title         string    `bun:"title,notnull" json:"title"`
}
```

See the [UUID example](./examples/uuid_pk) for a complete working example with UUID primary keys and nested routes.

## Nested Resources

go-restgen automatically handles parent-child relationships with full chain validation:

**Security Features:**
- Foreign keys are automatically set from the URL path
- Foreign keys in JSON body are ignored (path takes precedence)
- IDs in JSON body are ignored (path takes precedence)
- Parent chain is validated at database level with JOINs
- Returns 404 if resource doesn't belong to parent chain

See the [nested routes example](./examples/nested_routes) for a complete working example with 3-level nesting.

## Relation Includes

Load related resources in a single request using the `?include=` query parameter. This avoids N+1 queries when you need parent and child data together.

### Enabling Includes with WithRelationName

Use `WithRelationName()` when registering child routes to enable the `?include=` parameter:

```go
type Author struct {
    bun.BaseModel `bun:"table:authors"`
    ID            int     `bun:"id,pk,autoincrement" json:"id"`
    Name          string  `bun:"name" json:"name"`
    Posts         []*Post `bun:"rel:has-many,join:id=author_id" json:"posts,omitempty"`
}

type Post struct {
    bun.BaseModel `bun:"table:posts"`
    ID            int     `bun:"id,pk,autoincrement" json:"id"`
    AuthorID      int     `bun:"author_id,notnull" json:"author_id"`
    Author        *Author `bun:"rel:belongs-to,join:author_id=id" json:"-"`
    OwnerID       string  `bun:"owner_id,notnull" json:"owner_id"`
    Title         string  `bun:"title" json:"title"`
}

router.RegisterRoutes[Author](b, "/authors",
    router.AllPublic(),
    func(b *router.Builder) {
        router.RegisterRoutes[Post](b, "/posts",
            router.AllWithOwnershipUnless([]string{"OwnerID"}, "admin"),
            router.WithRelationName("Posts"),  // Enables ?include=Posts on parent
        )
    },
)
```

### Using ?include=

```bash
# Get author with their posts
GET /authors/1?include=Posts

# Response includes the relation
{
    "id": 1,
    "name": "Alice",
    "posts": [
        {"id": 1, "title": "First Post", "owner_id": "alice"},
        {"id": 2, "title": "Second Post", "owner_id": "alice"}
    ]
}

# Multiple includes (comma-separated)
GET /authors/1?include=Posts,Comments
```

### Security: Same Auth as Direct Access

**Includes respect the child route's auth configuration.** The same security rules that apply when accessing the child route directly also apply when including it:

- **Unauthorized relations are silently omitted** - no error, just not included
- **Ownership filtering applies** - users only see their own child records
- **Bypass scopes work** - admins see all child records if configured

```go
// Parent is public, child has ownership
router.RegisterRoutes[Author](b, "/authors",
    router.AllPublic(),
    func(b *router.Builder) {
        router.RegisterRoutes[Post](b, "/posts",
            router.AllWithOwnershipUnless([]string{"OwnerID"}, "admin"),
            router.WithRelationName("Posts"),
        )
    },
)
```

| Request | Result |
|---------|--------|
| No auth + `?include=Posts` | Author returned, posts omitted (not authorized) |
| Alice + `?include=Posts` | Author + only Alice's posts (ownership filtered) |
| Admin + `?include=Posts` | Author + all posts (bypass scope) |

### Key Points

- **Relation name must match the struct field** - `WithRelationName("Posts")` maps to `Posts []*Post` field
- **Unknown relation names are silently ignored** - for security, no error is returned
- **Works with GET, LIST, and Actions** - `/authors/1?include=Posts`, `/authors?include=Posts`, and `/orders/1/complete?include=Items`
- **Does not work with Batch operations** - batch create/update return items without relations (reload separately if needed)
- **Nested includes not supported** - only direct children can be included

See the [relations example](./examples/relations) for a complete working example.

## Single Routes (Belongs-To Relations)

go-restgen supports single-object routes for belongs-to relationships. Unlike collection routes that return arrays, single routes return a single object and only support GET (and optionally PUT).

### Use Cases

- **Nested belongs-to**: `/posts/{id}/author` - Get the author of a post
- **Current user endpoint**: `/me` - Get/update the authenticated user

### Nested Single Route (Parent FK Field)

When a parent has a foreign key to a child (e.g., `Post.AuthorID` → `User.ID`), use `AsSingleRoute()` or `AsSingleRouteWithPut()`:

```go
type Post struct {
    bun.BaseModel `bun:"table:posts"`
    ID            int   `bun:"id,pk,autoincrement" json:"id"`
    AuthorID      int   `bun:"author_id,notnull" json:"author_id"`
    Author        *User `bun:"rel:belongs-to,join:author_id=id" json:"author,omitempty"`
    Title         string `bun:"title" json:"title"`
}

router.RegisterRoutes[Post](b, "/posts",
    router.AllPublic(),
    func(b *router.Builder) {
        // GET /posts/{id}/author - returns the User referenced by Post.AuthorID
        router.RegisterRoutes[User](b, "/author",
            router.AsSingleRoute("AuthorID"),  // Field on Post that holds User.ID
            router.AllPublic(),
        )
    },
)
```

To also allow PUT on the single route:

```go
router.RegisterRoutes[User](b, "/author",
    router.AsSingleRouteWithPut("AuthorID"),  // Enables both GET and PUT
    router.AllPublic(),
)
```

### Root-Level Single Route (Custom Logic)

For routes like `/me` where the ID comes from authentication context rather than a URL parameter, pass an empty string and provide custom handlers:

```go
// Custom get - ID from auth context
func getMe(ctx context.Context, svc *service.Common[User], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, _ string) (*User, error) {
    if auth == nil {
        return nil, fmt.Errorf("not authenticated")
    }
    // Look up user by external ID from auth
    return lookupUserByExternalID(ctx, auth.UserID)
}

// Custom update - must set ID to prevent fake ID in request body
func updateMe(ctx context.Context, svc *service.Common[User], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, _ string, item User) (*User, error) {
    if auth == nil {
        return nil, fmt.Errorf("not authenticated")
    }
    userID := lookupUserIDByExternalID(auth.UserID)
    item.ID = userID  // Prevent client from setting fake ID
    return svc.Update(ctx, strconv.Itoa(userID), item)
}

router.RegisterRoutes[User](b, "/me",
    router.AsSingleRouteWithPut(""),  // Empty string = no parent FK
    router.IsAuthenticated(),
    router.WithCustomGet(getMe),
    router.WithCustomUpdate(updateMe),
)
```

**Important**: Without custom handlers on root-level single routes, GET/PUT will fail because there's no way to determine the ID.

### Key Differences from Collection Routes

| Feature | Collection Route | Single Route |
|---------|-----------------|--------------|
| Response | Array `[...]` | Single object `{...}` |
| Endpoints | GET, POST, PUT, DELETE | GET only (or GET + PUT with `WithPut`) |
| ID source | URL parameter | Parent's FK field or custom logic |
| Use case | Has-many relations | Belongs-to relations |

### Security

Single routes inherit the same security features as collection routes:
- Auth and scope checks apply
- Ownership filtering works (if configured)
- Parent chain validation is performed

See the [relations example](./examples/relations) for a complete working example with both nested and root-level single routes.

## Authentication & Authorization

go-restgen provides flexible, granular authentication and authorization controls. **Routes are blocked by default** (secure by default) unless explicitly configured.

### Developer Provides Auth Middleware

The framework doesn't implement auth itself - you provide your own middleware:

```go
// Example: JWT auth middleware
func authMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Your auth logic (JWT, OAuth, session, etc.)
        token := extractToken(r)
        userID, scopes := validateToken(token)  // Your implementation

        // Populate AuthInfo for the framework
        authInfo := &router.AuthInfo{
            UserID: userID,  // External user ID (e.g., "auth0|123", Firebase UID)
            Scopes: scopes,  // User's permissions/scopes
        }

        ctx := context.WithValue(r.Context(), router.AuthInfoKey, authInfo)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}

r := chi.NewRouter()
r.Use(authMiddleware)  // Apply globally
```

### Public Routes

No authentication required:

```go
router.RegisterRoutes[Article](b, "/articles", router.AuthConfig{
    Methods: []string{router.MethodAll},
    Scopes:  []string{router.ScopePublic},
})
```

### Authentication Required (No Scope Check)

Any authenticated user can access:

```go
router.RegisterRoutes[Profile](b, "/profile", router.AuthConfig{
    Methods: []string{router.MethodAll},
    Scopes:  []string{router.ScopeAuthOnly},
})
```

### Scope-Based Authorization

Require specific scopes (user must have at least one):

```go
router.RegisterRoutes[User](b, "/admin/users", router.AuthConfig{
    Methods: []string{router.MethodAll},
    Scopes:  []string{"admin", "superuser"},  // Needs admin OR superuser
})
```

### Method-Specific Auth

Different auth requirements per HTTP method:

```go
router.RegisterRoutes[Post](b, "/posts",
    router.AuthConfig{
        Methods: []string{router.MethodGet},
        Scopes:  []string{router.ScopePublic},  // Public reads
    },
    router.AuthConfig{
        Methods: []string{router.MethodPost, router.MethodPut, router.MethodDelete},
        Scopes:  []string{"user"},  // Authenticated writes
    },
)
```

### MethodAll Override Pattern

Set a default for all methods, then override specific ones:

```go
router.RegisterRoutes[Post](b, "/posts",
    router.AuthConfig{
        Methods: []string{router.MethodAll},
        Scopes:  []string{"user"},  // Default: require "user" scope
    },
    router.AuthConfig{
        Methods: []string{router.MethodGet},
        Scopes:  []string{router.ScopePublic},  // Override: GET is public
    },
)
```

### Ownership-Based Access Control

Automatically scope resources to the authenticated user:

```go
type Post struct {
    bun.BaseModel `bun:"table:posts"`
    ID            int    `bun:"id,pk,autoincrement" json:"id"`
    UserID        string `bun:"user_id,notnull" json:"user_id"`  // External user ID
    Title         string `bun:"title,notnull" json:"title"`
}

router.RegisterRoutes[Post](b, "/posts", router.AuthConfig{
    Methods: []string{router.MethodAll},
    Ownership: &router.OwnershipConfig{
        Fields:       []string{"UserID"},  // Field(s) to check for ownership
        BypassScopes: []string{},  // No bypass
    },
})
```

**How Ownership Works:**
- **CREATE**: Automatically sets `UserID` from `AuthInfo.UserID` (ignores JSON body value)
- **GET/LIST**: Auto-applies `WHERE user_id = <authUserID>` filter
- **UPDATE/DELETE**: Validates resource belongs to user before allowing operation
- **Returns 404** if resource doesn't belong to user (doesn't leak existence)

### Ownership with Admin Bypass

Allow admins to access all resources:

```go
router.RegisterRoutes[Post](b, "/posts", router.AuthConfig{
    Methods: []string{router.MethodAll},
    Ownership: &router.OwnershipConfig{
        Fields:       []string{"UserID"},
        BypassScopes: []string{"admin", "moderator"},  // Admins bypass ownership
    },
})
```

### Multiple Owner Fields (OR Logic)

Allow access if user owns via any of the specified fields:

```go
type Post struct {
    ID           int    `bun:"id,pk,autoincrement" json:"id"`
    AuthorID     string `bun:"author_id,notnull" json:"author_id"`
    AssignedToID string `bun:"assigned_to_id" json:"assigned_to_id"`
    Title        string `bun:"title" json:"title"`
}

router.RegisterRoutes[Post](b, "/posts",
    router.AuthConfig{
        Methods: []string{router.MethodGet, router.MethodPut},
        Ownership: &router.OwnershipConfig{
            Fields:       []string{"AuthorID", "AssignedToID"},  // Author OR assigned
            BypassScopes: []string{"admin"},
        },
    },
    router.AuthConfig{
        Methods: []string{router.MethodDelete},
        Ownership: &router.OwnershipConfig{
            Fields:       []string{"AuthorID"},  // Only author can delete
            BypassScopes: []string{"admin"},
        },
    },
)
```

### Complex Example: Mixed Auth Patterns

```go
router.RegisterRoutes[Post](b, "/posts",
    // Public reads
    router.AuthConfig{
        Methods: []string{router.MethodGet},
        Scopes:  []string{router.ScopePublic},
    },
    // Authenticated users can create (ownership auto-set)
    router.AuthConfig{
        Methods: []string{router.MethodPost},
        Ownership: &router.OwnershipConfig{
            Fields:       []string{"UserID"},
            BypassScopes: []string{},
        },
    },
    // Owners can update/delete their own, admins can update/delete any
    router.AuthConfig{
        Methods: []string{router.MethodPut, router.MethodDelete},
        Ownership: &router.OwnershipConfig{
            Fields:       []string{"UserID"},
            BypassScopes: []string{"admin"},
        },
    },
)
```

### Auth Constants Reference

```go
// HTTP Methods
router.MethodGet, router.MethodPost, router.MethodPut, router.MethodDelete, router.MethodAll

// Special Scopes
router.ScopePublic    // "__restgen_public__" - No auth required
router.ScopeAuthOnly  // "__restgen_auth_only__" - Auth required, no scope check

// Context Key
router.AuthInfoKey  // "authInfo" - Key for AuthInfo in context
```

## Query Parameters: Filtering, Sorting & Pagination

go-restgen supports query parameters for filtering, sorting, and paginating results on `GET /resource` (list) endpoints.

### Configuration

Configure allowed fields when registering routes:

```go
router.RegisterRoutes[User](b, "/users",
    router.AllPublic(),
    router.WithFilters("Name", "Email", "Status"),      // Allow filtering by these fields
    router.WithSorts("Name", "Email", "CreatedAt"),     // Allow sorting by these fields
    router.WithPagination(20, 100),                     // Default 20 items, max 100
    router.WithDefaultSort("-CreatedAt"),               // Default sort (- prefix = descending)
)
```

**Security Note**: Only fields explicitly listed in `WithFilters` and `WithSorts` can be used. Invalid fields are silently ignored.

### Filtering

Filter results using the `filter[field]` query parameter:

```bash
# Simple equality filter
GET /users?filter[Name]=Alice

# With operator
GET /users?filter[Status][neq]=inactive
GET /users?filter[Age][gte]=18
GET /users?filter[Name][like]=John%

# Multiple filters (AND logic)
GET /users?filter[Status]=active&filter[Role]=admin
```

**Supported Operators:**
| Operator | Description | Example |
|----------|-------------|---------|
| `eq` (default) | Equals | `filter[Status]=active` or `filter[Status][eq]=active` |
| `neq` | Not equals | `filter[Status][neq]=inactive` |
| `gt` | Greater than | `filter[Age][gt]=18` |
| `gte` | Greater than or equal | `filter[Age][gte]=18` |
| `lt` | Less than | `filter[Age][lt]=65` |
| `lte` | Less than or equal | `filter[Age][lte]=65` |
| `like` | SQL LIKE pattern | `filter[Name][like]=John%` |
| `in` | In list | `filter[Status][in]=active,pending` |
| `nin` | Not in list | `filter[Status][nin]=deleted,archived` |
| `bt` | Between (inclusive) | `filter[Age][bt]=18,65` |
| `nbt` | Not between | `filter[Price][nbt]=100,500` |

### Sorting

Sort results using the `sort` query parameter:

```bash
# Single field ascending
GET /users?sort=Name

# Single field descending (- prefix)
GET /users?sort=-CreatedAt

# Multiple fields (comma-separated)
GET /users?sort=Status,-CreatedAt
```

### Pagination

Control result size with `limit` and `offset`:

```bash
# First 10 results
GET /users?limit=10

# Skip first 20, get next 10
GET /users?limit=10&offset=20
```

**Response Headers:**
- `X-Limit` - The limit applied to the query
- `X-Offset` - The offset applied to the query

### Total Count

Request total count (useful for pagination UI) with `count=true`:

```bash
GET /users?limit=10&offset=20&count=true
```

**Response Headers:**
- `X-Total-Count` - Total number of records (before pagination)

### Complete Example

```bash
# Get active users, sorted by newest first, page 2 (10 per page), with total count
curl 'http://localhost:8080/users?filter[Status]=active&sort=-CreatedAt&limit=10&offset=10&count=true'

# Response headers include:
# X-Total-Count: 47
# X-Limit: 10
# X-Offset: 10
```

## Multi-Registration

go-restgen supports registering the same model type at multiple routes with different configurations. This is useful when:
- You need different auth/ownership rules for the same resource at different endpoints
- A resource should appear both as a root resource and nested under a parent

### Same Model at Root and Nested

```go
// Item can be accessed at both /items (root) and /projects/{id}/items (nested)
type Item struct {
    bun.BaseModel `bun:"table:items"`
    ID            int    `bun:"id,pk,autoincrement" json:"id"`
    ProjectID     int    `bun:"project_id" json:"project_id"`
    Project       *Project `bun:"rel:belongs-to,join:project_id=id" json:"-"`
    Name          string `bun:"name" json:"name"`
}

b := router.NewBuilder(r)

// Root registration - public read, admin write
router.RegisterRoutes[Item](b, "/items",
    router.PublicReadOnly(),
    router.AllScoped("admin"),
)

// Nested registration - filtered by project with ownership
router.RegisterRoutes[Project](b, "/projects",
    router.AllWithOwnershipUnless([]string{"OwnerID"}, "admin"),
    func(b *router.Builder) {
        router.RegisterRoutes[Item](b, "/items",
            router.AllWithOwnershipUnless([]string{"OwnerID"}, "admin"),
        )
    },
)
```

### Different Ownership per Registration

```go
// Same Item model with different ownership rules per parent
router.RegisterRoutes[User](b, "/users",
    router.IsAuthenticated(),
    func(b *router.Builder) {
        // User's items - ownership filtered by user
        router.RegisterRoutes[Item](b, "/items",
            router.AllWithOwnershipUnless([]string{"OwnerID"}, "admin"),
        )
    },
)

router.RegisterRoutes[Project](b, "/projects",
    router.IsAuthenticated(),
    func(b *router.Builder) {
        // Project's items - different ownership, moderator bypass
        router.RegisterRoutes[Item](b, "/items",
            router.AllWithOwnershipUnless([]string{"OwnerID"}, "moderator"),
        )
    },
)
```

**How It Works:**
- Each registration creates independent metadata stored in request context
- Parent filtering is based on the URL path, not a global registry
- Ownership and auth configs are per-registration
- No conflicts between registrations of the same type

## Validation

go-restgen supports custom validation for create, update, and delete operations. Validation runs **after** authentication and authorization checks, ensuring validators only see authorized requests.

### Basic Usage

```go
router.RegisterRoutes[Task](b, "/tasks",
    router.AllPublic(),
    router.WithValidator(func(vc metadata.ValidationContext[Task]) error {
        switch vc.Operation {
        case metadata.OpCreate:
            if vc.New.Priority < 1 || vc.New.Priority > 5 {
                return errors.New("priority must be between 1 and 5")
            }
        case metadata.OpUpdate:
            // Access both old and new values for transition validation
            if vc.Old.Status == "completed" {
                return errors.New("cannot modify completed tasks")
            }
        case metadata.OpDelete:
            if vc.Old.Status == "completed" {
                return errors.New("cannot delete completed tasks")
            }
        }
        return nil
    }),
)
```

### ValidationContext

The validator receives a `ValidationContext[T]` with:

| Field | Type | Description |
|-------|------|-------------|
| `Operation` | `metadata.Operation` | One of `OpCreate`, `OpUpdate`, `OpDelete` |
| `New` | `*T` | The incoming item (nil for delete) |
| `Old` | `*T` | The existing item (nil for create) |
| `Ctx` | `context.Context` | Request context (contains AuthInfo, parent IDs, etc.) |

### State Machine Example

Validate status transitions using the old and new values:

```go
var validTransitions = map[string][]string{
    "pending":     {"in_progress", "cancelled"},
    "in_progress": {"completed", "cancelled"},
    "completed":   {},  // Final state
    "cancelled":   {},  // Final state
}

func isValidTransition(from, to string) bool {
    if from == to { return true }
    for _, allowed := range validTransitions[from] {
        if allowed == to { return true }
    }
    return false
}

router.WithValidator(func(vc metadata.ValidationContext[Task]) error {
    if vc.Operation == metadata.OpUpdate {
        if !isValidTransition(vc.Old.Status, vc.New.Status) {
            return fmt.Errorf("invalid transition from '%s' to '%s'",
                vc.Old.Status, vc.New.Status)
        }
    }
    return nil
})
```

### Error Handling

- Return an `error` to reject the operation with a 400 Bad Request
- The error message is returned to the client
- Return `nil` to allow the operation to proceed

**Important Notes:**
- Validators are **read-only** - they cannot modify the input
- Validators run **after** auth checks (so you know the request is authorized)
- For create operations, ownership fields are already set when the validator runs
- Validators should be fast (no database queries) as they run on every mutation

See the [validator example](./examples/validator) for a complete working example with state machine validation.

## Audit

go-restgen supports custom audit logging for create, update, and delete operations. Audit records are inserted within the same database transaction as the main operation, ensuring consistency.

### Basic Usage

```go
// Define your audit log model
type JobAuditLog struct {
    bun.BaseModel `bun:"table:job_audit_logs"`
    ID            int       `bun:"id,pk,autoincrement" json:"id"`
    JobID         int       `bun:"job_id,notnull" json:"job_id"`
    Operation     string    `bun:"operation,notnull" json:"operation"`
    OldData       string    `bun:"old_data" json:"old_data"`
    NewData       string    `bun:"new_data" json:"new_data"`
    CreatedAt     time.Time `bun:"created_at,notnull" json:"created_at"`
}

// Register routes with audit
router.RegisterRoutes[Job](b, "/jobs",
    router.AllPublic(),
    router.WithAudit(func(ac metadata.AuditContext[Job]) any {
        return &JobAuditLog{
            JobID:     ac.New.ID,  // or ac.Old.ID for delete
            Operation: string(ac.Operation),
            OldData:   toJSON(ac.Old),
            NewData:   toJSON(ac.New),
            CreatedAt: time.Now(),
        }
    }),
)
```

### AuditContext

The audit function receives an `AuditContext[T]` with:

| Field | Type | Description |
|-------|------|-------------|
| `Operation` | `metadata.Operation` | One of `OpCreate`, `OpUpdate`, `OpDelete` |
| `New` | `*T` | The item after operation (nil for delete) |
| `Old` | `*T` | The item before operation (nil for create) |
| `Ctx` | `context.Context` | Request context (contains AuthInfo, parent IDs, etc.) |

### How It Works

- **Transaction**: Audit insert runs in the same transaction as the main operation
- **Rollback**: If audit insert fails, the main operation is rolled back
- **Skip Audit**: Return `nil` from the audit function to skip audit for that operation
- **Flexible**: You define the audit model - can include user info, timestamps, JSON snapshots, etc.

### Conditional Auditing

Skip audit for certain operations by returning `nil`:

```go
router.WithAudit(func(ac metadata.AuditContext[Job]) any {
    // Only audit status changes
    if ac.Operation == metadata.OpUpdate {
        if ac.Old.Status == ac.New.Status {
            return nil  // Skip audit - no status change
        }
    }

    return &JobAuditLog{
        JobID:     getJobID(ac),
        Operation: string(ac.Operation),
        OldStatus: getStatus(ac.Old),
        NewStatus: getStatus(ac.New),
    }
})
```

### Accessing User Info

Access the authenticated user from the context:

```go
router.WithAudit(func(ac metadata.AuditContext[Job]) any {
    // Get user ID from auth context
    var userID string
    if authInfo, ok := ac.Ctx.Value(router.AuthInfoKey).(*router.AuthInfo); ok {
        userID = authInfo.UserID
    }

    return &JobAuditLog{
        JobID:     ac.New.ID,
        Operation: string(ac.Operation),
        UserID:    userID,
        CreatedAt: time.Now(),
    }
})
```

See the [audit example](./examples/audit) for a complete working example.

## Custom Handlers

go-restgen allows you to override the default CRUD behavior with custom handler functions while still benefiting from the framework's request parsing, authentication, error handling, and response formatting.

### Use Cases

- **`/me` endpoint**: Get the current user from auth token instead of URL parameter
- **Auto-set ownership**: Automatically set `owner_id` from authenticated user on create
- **Custom filtering**: Filter GetAll results based on the authenticated user
- **Custom validation**: Add business logic validation in the handler
- **Prevent operations**: Block certain operations (e.g., prevent deletion of audit logs)

### Custom Function Signatures

Each custom function receives the service, metadata, auth info, and parsed inputs:

```go
// CustomGetFunc - for GET /resource/{id}
type CustomGetFunc[T any] func(
    ctx context.Context,
    svc *service.Common[T],
    meta *metadata.TypeMetadata,
    auth *metadata.AuthInfo,  // nil if not authenticated
    id string,
) (*T, error)

// CustomGetAllFunc - for GET /resource
// Query options are available via metadata.QueryOptionsFromContext(ctx) if needed.
type CustomGetAllFunc[T any] func(
    ctx context.Context,
    svc *service.Common[T],
    meta *metadata.TypeMetadata,
    auth *metadata.AuthInfo,
) ([]*T, int, error)

// CustomCreateFunc - for POST /resource
// Includes file parameters for file resource support (nil for regular JSON requests).
type CustomCreateFunc[T any] func(
    ctx context.Context,
    svc *service.Common[T],
    meta *metadata.TypeMetadata,
    auth *metadata.AuthInfo,
    item T,
    file io.Reader,
    fileMeta filestore.FileMetadata,
) (*T, error)

// CustomUpdateFunc - for PUT /resource/{id}
type CustomUpdateFunc[T any] func(
    ctx context.Context,
    svc *service.Common[T],
    meta *metadata.TypeMetadata,
    auth *metadata.AuthInfo,
    id string,
    item T,
) (*T, error)

// CustomDeleteFunc - for DELETE /resource/{id}
type CustomDeleteFunc[T any] func(
    ctx context.Context,
    svc *service.Common[T],
    meta *metadata.TypeMetadata,
    auth *metadata.AuthInfo,
    id string,
) error
```

### Example: /me Endpoint

Get the current user from auth token instead of URL parameter:

```go
// Custom Get that uses auth.UserID instead of URL id
func customGetMe(ctx context.Context, svc *service.Common[User], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string) (*User, error) {
    if auth == nil {
        return nil, fmt.Errorf("not authenticated")
    }
    // Find user by external_id (auth UserID) instead of primary key
    var user User
    err := db.GetDB().NewSelect().Model(&user).Where("external_id = ?", auth.UserID).Scan(ctx)
    return &user, err
}

router.RegisterRoutes[User](b, "/me",
    router.AsSingleRouteWithPut(""),  // Empty string = no parent FK, ID from custom logic
    router.IsAuthenticated(),
    router.WithCustomGet(customGetMe),
)
```

### Example: Auto-Set Owner on Create

```go
func customCreateTask(ctx context.Context, svc *service.Common[Task], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, item Task, _ io.Reader, _ filestore.FileMetadata) (*Task, error) {
    if auth == nil {
        return nil, fmt.Errorf("not authenticated")
    }
    // Auto-set owner to current user
    item.OwnerID = auth.UserID
    return svc.Create(ctx, item)
}

router.RegisterRoutes[Task](b, "/tasks",
    router.IsAuthenticated(),
    router.WithCustomCreate(customCreateTask),
)
```

### Example: Filter GetAll by Owner

```go
func customGetMyTasks(ctx context.Context, svc *service.Common[Task], meta *metadata.TypeMetadata, auth *metadata.AuthInfo) ([]*Task, int, error) {
    if auth == nil {
        return nil, 0, fmt.Errorf("not authenticated")
    }
    // Return only tasks owned by current user
    tasks := []*Task{}
    err := db.GetDB().NewSelect().Model(&tasks).Where("owner_id = ?", auth.UserID).Scan(ctx)
    return tasks, len(tasks), err
}

router.RegisterRoutes[Task](b, "/my-tasks",
    router.IsAuthenticated(),
    router.WithCustomGetAll(customGetMyTasks),
)
```

### Example: Prevent Deletion

```go
func customDeleteAuditLog(ctx context.Context, svc *service.Common[AuditLog], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string) error {
    return fmt.Errorf("audit logs cannot be deleted")
}

router.RegisterRoutes[AuditLog](b, "/audit-logs",
    router.IsAuthenticated(),
    router.WithCustomDelete(customDeleteAuditLog),
)
```

### Combining Custom Handlers

You can mix custom handlers with standard behavior and other options:

```go
router.RegisterRoutes[Project](b, "/projects",
    router.IsAuthenticated(),
    router.WithCustomCreate(customCreateProject),  // Custom create
    router.WithFilters("Status", "Name"),          // Standard filtering
    func(b *router.Builder) {
        router.RegisterRoutes[Item](b, "/items",
            router.IsAuthenticated(),
            router.WithCustomUpdate(customUpdateItem),  // Custom update with validation
        )
    },
)
```

**Key Points:**
- Custom handlers receive the service (`svc`) so you can still use standard operations
- Auth info is passed explicitly - check for `nil` if the route allows unauthenticated access
- The framework handles JSON parsing, response encoding, and error formatting
- Custom handlers work with nested routes, auth configs, validators, and auditors

See the [custom handlers example](./examples/custom) for a complete working example.

## Action Endpoints

go-restgen supports custom action endpoints for operations beyond standard CRUD. Actions are custom operations on a single resource, like `POST /orders/{id}/cancel` or `POST /tasks/{id}/complete`.

### Defining Actions

Register actions using `WithAction()` when setting up routes:

```go
// Action handler signature
func cancelOrder(
    ctx context.Context,
    svc *service.Common[Order],
    meta *metadata.TypeMetadata,
    auth *metadata.AuthInfo,
    id string,
    item *Order,      // Pre-fetched item (validates existence/ownership)
    payload []byte,   // Raw request body
) (*Order, error) {
    // Parse action-specific payload
    var req CancelRequest
    if err := json.Unmarshal(payload, &req); err != nil {
        return nil, err
    }

    // Perform action
    item.Status = "cancelled"
    item.CancelReason = req.Reason
    return svc.Update(ctx, id, *item)
}

// Register routes with actions
router.RegisterRoutes[Order](b, "/orders",
    router.AllPublic(),
    router.WithAction("cancel", cancelOrder, router.AuthConfig{
        Scopes: []string{"user"},
    }),
    router.WithAction("refund", refundOrder, router.AuthConfig{
        Scopes: []string{"admin"},
    }),
)
```

### How Actions Work

1. **Framework fetches the item first** - Validates existence, parent chain, and ownership
2. **Raw payload passed to handler** - Decode into action-specific struct as needed
3. **Return updated item or nil** - Returns 200 + JSON or 204 No Content

### Auth Per Action

Each action has its own auth configuration. Actions are **blocked by default** if no auth is provided:

```go
// Different auth per action
router.WithAction("cancel", cancelOrder, router.AuthConfig{
    Scopes: []string{"user"},
    Ownership: &router.OwnershipConfig{
        Fields:       []string{"OwnerID"},
        BypassScopes: []string{"admin"},
    },
}),
router.WithAction("approve", approveOrder, router.AuthConfig{
    Scopes: []string{"manager", "admin"},
}),
```

### Generated Endpoints

```
POST /orders/{id}/cancel   → cancelOrder handler
POST /orders/{id}/refund   → refundOrder handler
POST /orders/{id}/approve  → approveOrder handler
```

### Response Handling

| Handler Returns | HTTP Response |
|----------------|---------------|
| `(*T, nil)` | 200 OK + JSON body |
| `(nil, nil)` | 204 No Content |
| `(_, error)` | Error response (400, 404, 500, etc.) |

## Batch Operations

go-restgen supports batch create, update, and delete operations via `/resource/batch` endpoints. Batch operations run in a transaction for all-or-nothing semantics.

### Enabling Batch Operations

Use `AllScopedWithBatch()` or `AllPublicWithBatch()` to enable batch endpoints:

```go
// All operations including batch require admin scope
router.RegisterRoutes[Post](b, "/posts",
    router.AllScopedWithBatch("admin"),
    router.WithBatchLimit(100),  // Optional: limit batch size
)

// Or: public single ops, admin-only batch
router.RegisterRoutes[Post](b, "/posts",
    router.AllPublic(),
    router.AuthConfig{
        Methods: []string{router.MethodBatchCreate, router.MethodBatchUpdate, router.MethodBatchDelete},
        Scopes:  []string{"admin"},
    },
)
```

**Important**: `MethodAll` does NOT include batch methods (safe default). Use `MethodAllWithBatch` or specific batch methods.

### Generated Endpoints

```
POST   /posts/batch  → Batch create
PUT    /posts/batch  → Batch update
DELETE /posts/batch  → Batch delete
```

### Batch Create

```bash
POST /posts/batch
Content-Type: application/json

[
    {"title": "Post 1", "content": "..."},
    {"title": "Post 2", "content": "..."},
    {"title": "Post 3", "content": "..."}
]

# Response: 201 Created
[
    {"id": 1, "title": "Post 1", ...},
    {"id": 2, "title": "Post 2", ...},
    {"id": 3, "title": "Post 3", ...}
]
```

### Batch Update

```bash
PUT /posts/batch
Content-Type: application/json

[
    {"id": 1, "title": "Updated Post 1", "content": "..."},
    {"id": 2, "title": "Updated Post 2", "content": "..."}
]

# Response: 200 OK
[
    {"id": 1, "title": "Updated Post 1", ...},
    {"id": 2, "title": "Updated Post 2", ...}
]
```

### Batch Delete

```bash
DELETE /posts/batch
Content-Type: application/json

[{"id": 1}, {"id": 2}, {"id": 3}]

# Response: 204 No Content
```

### Batch Limit

Optionally limit batch size to prevent abuse:

```go
router.RegisterRoutes[Post](b, "/posts",
    router.AllScopedWithBatch("admin"),
    router.WithBatchLimit(100),  // Max 100 items per batch
)
```

Exceeding the limit returns 400 Bad Request.

### Transaction Semantics

- **All-or-nothing**: If any item fails, the entire batch is rolled back
- **Ownership validated per-item**: Each item is checked individually
- **Validation runs per-item**: Custom validators are invoked for each item
- **Audit runs per-item**: Audit records are created for each item in the same transaction

### Custom Batch Handlers

Override default batch behavior with custom handlers:

```go
router.RegisterRoutes[Post](b, "/posts",
    router.AllScopedWithBatch("admin"),
    router.WithCustomBatchCreate(func(ctx context.Context, svc *service.Common[Post], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, items []Post) ([]*Post, error) {
        // Custom batch create logic
        for i := range items {
            items[i].CreatedBy = auth.UserID
        }
        return svc.BatchCreate(ctx, items)
    }),
)
```

### File Resources

Batch operations have special handling for file resources:
- **Batch DELETE works** - Legitimate use case for bulk cleanup
- **Batch CREATE returns 501** - File uploads require multipart form
- **Batch UPDATE returns 501** - File resources don't support update

### Method Constants

```go
router.MethodBatchCreate    // "BATCH_CREATE"
router.MethodBatchUpdate    // "BATCH_UPDATE"
router.MethodBatchDelete    // "BATCH_DELETE"
router.MethodAllWithBatch   // Expands to all methods including batch
```

### Helper Functions

```go
// Includes batch methods
router.AllPublicWithBatch()              // All methods public including batch
router.AllScopedWithBatch("admin")       // All methods require scope including batch

// Does NOT include batch (existing helpers unchanged)
router.AllPublic()                       // All standard methods public
router.AllScoped("admin")                // All standard methods require scope
```

## File Resources

go-restgen supports file upload and download with pluggable storage backends. Files are stored in external storage (local filesystem, S3, etc.) while metadata is stored in the database.

### Features

- **Multipart form upload** - Standard form-based file uploads with optional metadata
- **Pluggable storage** - Implement `filestore.FileStorage` for any backend
- **Two download modes**:
  - **Proxy mode**: Files stream through your server (full auth control on downloads)
  - **Signed URL mode**: Clients download directly from storage (scalable, CDN-friendly)
- **Automatic metadata handling** - Filename, content type, and size tracked automatically

### Defining a File Resource

Embed `filestore.FileFields` in your model to make it a file resource:

```go
import "github.com/sjgoldie/go-restgen/filestore"

type Image struct {
    bun.BaseModel `bun:"table:images"`
    ID            int       `bun:"id,pk,autoincrement" json:"id"`
    PostID        int       `bun:"post_id,notnull" json:"post_id"`
    Post          *Post     `bun:"rel:belongs-to,join:post_id=id" json:"-"`
    filestore.FileFields    // Embeds StorageKey, Filename, ContentType, Size, DownloadURL
    AltText       string    `bun:"alt_text" json:"alt_text,omitempty"`
    CreatedAt     time.Time `bun:"created_at,notnull" json:"created_at"`
}
```

`FileFields` embeds these fields:
- `StorageKey` - Internal key for locating the file in storage (not exposed in JSON)
- `Filename` - Original filename from upload
- `ContentType` - MIME type of the file
- `Size` - File size in bytes
- `DownloadURL` - Generated URL for downloading (populated at response time)

### Implementing Storage

Implement the `filestore.FileStorage` interface:

```go
type FileStorage interface {
    Store(ctx context.Context, r io.Reader, meta FileMetadata) (key string, err error)
    Retrieve(ctx context.Context, key string) (io.ReadCloser, FileMetadata, error)
    Delete(ctx context.Context, key string) error
    GenerateSignedURL(ctx context.Context, key string) (string, error)
}
```

#### GenerateSignedURL Controls Download Mode

The `GenerateSignedURL` method determines how files are downloaded:

- **Return a URL** → Signed URL mode: Clients download directly from storage
- **Return empty string `""`** → Proxy mode: Files stream through your server via `/download` endpoint

This design gives you full control per-storage-implementation, and even per-file if needed.

**Proxy mode implementation** (downloads through your server):

```go
func (s *LocalStorage) GenerateSignedURL(ctx context.Context, key string) (string, error) {
    return "", nil  // Empty string = proxy mode
}
```

**Signed URL mode implementation** (direct download from storage):

```go
func (s *LocalStorage) GenerateSignedURL(ctx context.Context, key string) (string, error) {
    return s.urlPrefix + "/" + key, nil  // URL = signed URL mode
}
```

#### Complete Example

```go
type LocalStorage struct {
    basePath  string
    urlPrefix string  // Set for signed URL mode, empty for proxy mode
}

func (s *LocalStorage) Store(ctx context.Context, r io.Reader, meta filestore.FileMetadata) (string, error) {
    key := uuid.New().String()
    filePath := filepath.Join(s.basePath, key)
    f, err := os.Create(filePath)
    if err != nil {
        return "", err
    }
    defer f.Close()
    _, err = io.Copy(f, r)
    return key, err
}

func (s *LocalStorage) Retrieve(ctx context.Context, key string) (io.ReadCloser, filestore.FileMetadata, error) {
    f, err := os.Open(filepath.Join(s.basePath, key))
    if err != nil {
        return nil, filestore.FileMetadata{}, err
    }
    stat, _ := f.Stat()
    return f, filestore.FileMetadata{Size: stat.Size()}, nil
}

func (s *LocalStorage) Delete(ctx context.Context, key string) error {
    return os.Remove(filepath.Join(s.basePath, key))
}

func (s *LocalStorage) GenerateSignedURL(ctx context.Context, key string) (string, error) {
    if s.urlPrefix == "" {
        return "", nil  // Proxy mode
    }
    return s.urlPrefix + "/" + key, nil  // Signed URL mode
}
```

### Registering File Routes

Initialize file storage (global singleton like datastore), then use `AsFileResource()` option with `RegisterRoutes`:

```go
// Create your storage implementation (this is YOUR code, not the framework)
storage := &MyS3Storage{bucket: "my-bucket", client: s3Client}

// Initialize file storage globally (like datastore)
if err := filestore.Initialize(storage); err != nil {
    log.Fatal(err)
}

// Register parent with nested file resource
b := router.NewBuilder(r)
router.RegisterRoutes[Post](b, "/posts",
    router.AllPublic(),
    func(b *router.Builder) {
        router.RegisterRoutes[Image](b, "/images",
            router.AsFileResource(),  // Mark as file resource
            router.AllPublic(),
            router.WithFilters("Filename", "ContentType"),
            router.WithSorts("Filename", "CreatedAt"),
        )
    },
)
```

### Generated Endpoints

**Proxy mode** generates:
- `POST /posts/{id}/images` - Upload file (multipart form)
- `GET /posts/{id}/images` - List files
- `GET /posts/{id}/images/{id}` - Get file metadata
- `GET /posts/{id}/images/{id}/download` - Download file (streams through server)
- `DELETE /posts/{id}/images/{id}` - Delete file

**Signed URL mode** generates the same endpoints except no `/download` endpoint. The `download_url` in responses points directly to storage.

### Uploading Files

Upload using multipart form with a `file` field and optional `metadata` JSON field:

```bash
curl -X POST http://localhost:8080/posts/1/images \
  -F "file=@photo.jpg" \
  -F 'metadata={"alt_text":"A beautiful photo"}'
```

Response includes the generated download URL:

```json
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
```

### Proxy vs Signed URL Mode

The mode is determined by your `GenerateSignedURL` implementation:

| Feature | Proxy Mode (`return ""`) | Signed URL Mode (`return url`) |
|---------|--------------------------|--------------------------------|
| Download endpoint | `/resource/{id}/download` | Direct URL to storage |
| Server load | All downloads through server | Minimal (only metadata) |
| Auth on download | Full control | URL is the auth (time-limited in production) |
| Best for | Small files, strict auth | Large files, CDN, S3/Minio |

### S3/Minio Integration

For production use with S3 or Minio:

```go
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

func (s *S3Storage) GenerateSignedURL(ctx context.Context, key string) (string, error) {
    presignClient := s3.NewPresignClient(s.client)
    req, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
        Bucket: &s.bucket,
        Key:    &key,
    }, s3.WithPresignExpires(15*time.Minute))
    return req.URL, err
}
```

See the [files_proxy example](./examples/files_proxy) and [files_signed example](./examples/files_signed) for complete working implementations.

## Architecture

go-restgen follows a clean layered architecture:

```
Handler Layer (HTTP)
    ↓
Service Layer (Business Logic)
    ↓
Datastore Layer (Database Operations)
    ↓
Database (PostgreSQL via Bun ORM)
```

### Packages

- **`router`** - Convenience functions for registering routes
- **`handler`** - HTTP handlers for CRUD operations
- **`service`** - Business logic and CRUD services
- **`datastore`** - Generic database operations

## Custom Database Implementation

You can add support for other databases by implementing the `datastore.Store` interface:

```go
type Store interface {
    GetDB() *bun.DB
    GetTimeout() time.Duration
    Cleanup()
}
```

Example:

```go
type MySQL struct {
    db    *bun.DB
    sqlDB *sql.DB
}

func (s *MySQL) GetDB() *bun.DB {
    return s.db
}

func (s *MySQL) GetTimeout() time.Duration {
    return 5 * time.Second
}

func (s *MySQL) Cleanup() {
    s.db.Close()
    s.sqlDB.Close()
}
```

## Advanced Usage

### Observability (Logging, Tracing, Metrics)

go-restgen is designed to work with OpenTelemetry (OTEL) for comprehensive observability.

#### Logging

All logging uses context-aware `slog` methods (`slog.ErrorContext`, `slog.WarnContext`, etc.) which propagate trace IDs when OTEL is configured. Log levels are:

| Level | Purpose | Examples |
|-------|---------|----------|
| Error | Server failures needing attention | DB errors, service failures |
| Warn | Recoverable issues | Auth rejections, validation failures, cleanup errors |
| Debug | Client errors (already returning 4xx) | Bad JSON, missing parameters |

Configure logging in your application:

```go
import (
    "log/slog"
    "os"
)

func main() {
    // Configure log level (Error, Warn, Info, Debug)
    logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
        Level: slog.LevelWarn,
    }))
    slog.SetDefault(logger)

    // For OTEL trace propagation, use a bridge like slog-otel:
    // logger := slogmulti.Fanout(
    //     slog.NewTextHandler(os.Stderr, nil),
    //     otelslog.NewHandler(otelslog.HandlerConfig{}),
    // )
}
```

#### Tracing

go-restgen is OTEL-ready but doesn't include tracing itself. Add tracing via middleware and Bun hooks:

| Component | How to Add | What You Get |
|-----------|------------|--------------|
| HTTP requests | Use `otelchi` middleware | Request spans with method, path, status |
| Database queries | Use `bunotel.NewQueryHook()` | DB spans with query details |
| Trace context in logs | Configure slog with OTEL bridge | trace_id/span_id in log entries |

```go
import (
    "github.com/riandyrn/otelchi"
    "github.com/uptrace/bun/extra/bunotel"
)

func main() {
    r := chi.NewRouter()

    // Add OTEL tracing middleware
    r.Use(otelchi.Middleware("my-service"))

    // Add Bun query tracing
    db.AddQueryHook(bunotel.NewQueryHook())
}
```

#### Metrics

go-restgen includes optional metrics middleware that records request duration and count:

```go
import "github.com/sjgoldie/go-restgen/metrics"

func main() {
    // Initialize with your meter provider (or nil for global)
    metrics.Initialize(meterProvider)

    r := chi.NewRouter()
    r.Use(metrics.Middleware())
}
```

**Recorded metrics:**

| Metric | Type | Attributes | Description |
|--------|------|------------|-------------|
| `restgen.request.duration` | Histogram | resource, method, status | Request duration in ms |
| `restgen.request.count` | Counter | resource, method, status | Total request count |

The `resource` attribute uses the go-restgen type name (e.g., "Post", "User") when available, falling back to the URL path.

**Use case:** Track slow endpoints and error rates per resource type.

```go
// Custom metric recording for actions or custom handlers
metrics.RecordCustom(ctx, "Order", "cancel", 200, 45.2)
```

If OTEL is not configured, metrics are no-ops with negligible overhead.

### Mix with Custom Handlers

```go
// Register standard CRUD
router.RegisterCRUD[User](r, "/users")

// Add custom endpoints
r.Post("/users/bulk", bulkCreateHandler)
r.Get("/users/search", searchUsersHandler)
```

### Direct Access to Services

```go
import "github.com/sjgoldie/go-restgen/service"

func myCustomHandler(w http.ResponseWriter, r *http.Request) {
    svc, err := service.New[User]()
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    users, err := svc.GetAll(r.Context(), []string{})
    // ... handle response
}
```

## Examples

See the [`examples/`](./examples) directory for complete working examples:

- **[Simple CRUD](./examples/simple)** - Basic CRUD operations with SQLite
- **[Nested Routes](./examples/nested_routes)** - 3-level nesting (Users → Posts → Comments) with automatic parent validation
- **[UUID Primary Keys](./examples/uuid_pk)** - Using UUID primary keys instead of auto-increment integers
- **[Authentication & Authorization](./examples/auth)** - Comprehensive auth patterns including scopes, ownership, admin bypass, and multi-ownership
- **[Validation](./examples/validator)** - Business rule validation with state machine transitions
- **[Audit](./examples/audit)** - Audit logging with transactional consistency
- **[Relations](./examples/relations)** - Loading related resources via `?include=` with auth
- **[File Upload (Proxy)](./examples/files_proxy)** - File upload/download with proxy mode (files stream through server)
- **[File Upload (Signed URL)](./examples/files_signed)** - File upload/download with signed URL mode (direct to storage)
- **[Action Endpoints](./examples/actions)** - Custom actions on resources (cancel, complete)
- **[Batch Operations](./examples/batch)** - Bulk create, update, and delete operations
- **[Custom Handlers](./examples/custom)** - Override default CRUD behavior with custom handler functions

All examples include comprehensive Bruno API tests. See [`bruno/README.md`](./bruno/README.md) for details.

## Testing

go-restgen uses SQLite in-memory databases for fast, isolated tests:

```go
import (
    "context"
    "testing"
    "github.com/sjgoldie/go-restgen/datastore"
    "github.com/sjgoldie/go-restgen/service"
)

func TestMyHandler(t *testing.T) {
    // Create in-memory test database
    db, err := datastore.NewSQLite(":memory:")
    if err != nil {
        t.Fatal(err)
    }

    // Initialize datastore
    datastore.Initialize(db)
    defer datastore.Cleanup()

    // Create test tables
    _, err = db.GetDB().NewCreateTable().Model((*User)(nil)).IfNotExists().Exec(context.Background())
    if err != nil {
        t.Fatal(err)
    }

    // Get service and test
    svc, err := service.New[User]()
    // ... your tests
}
```

Run tests (excluding examples):

```bash
go test ./metadata ./datastore ./router ./service ./handler ./errors -coverprofile=/tmp/coverage.out
go tool cover -func=/tmp/coverage.out
```

For end-to-end API testing, see the [Bruno tests](./bruno/README.md) with 58 comprehensive API tests covering filtering, sorting, pagination, and ownership.

## Dependencies

go-restgen builds on these excellent projects:

- [chi](https://github.com/go-chi/chi) - Lightweight HTTP router (MIT)
- [bun](https://github.com/uptrace/bun) - SQL-first ORM (BSD-2-Clause)

## Roadmap

- [x] SQLite support
- [x] Nested resource support with automatic parent validation
- [x] Multi-registration support (same model at different routes with different configs)
- [x] Query parameter filtering and sorting
- [x] Pagination with limit/offset and total count
- [x] Custom validation for business rules
- [x] Transactional audit logging
- [x] UUID primary key support
- [x] Relation includes via `?include=` with auth enforcement
- [x] Single routes for belongs-to relations (`AsSingleRoute`)
- [x] File upload/download with pluggable storage (proxy and signed URL modes)
- [x] Action endpoints for custom operations (`POST /resource/{id}/action`)
- [x] Batch operations for bulk create/update/delete (`/resource/batch`)
- [ ] MySQL support
- [ ] OpenAPI/Swagger generation
- [ ] Standalone examples (separate go.mod per example to avoid polluting main module)

## Contributing

We welcome contributions! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

### Quick Start for Contributors

```bash
# Clone and setup
git clone https://github.com/sjgoldie/go-restgen.git
cd go-restgen
./scripts/setup-hooks.sh

# Run tests
go test ./metadata ./datastore ./router ./service ./handler ./errors

# Run linting
golangci-lint run

# Create PR
git checkout -b feature/my-feature
# ... make changes ...
git commit -m "feat: add my feature"
git push origin feature/my-feature
```

See [DEVELOPER.md](DEVELOPER.md) for detailed development guide.

## License

MIT License - see [LICENSE](LICENSE) for details

## Acknowledgments

Special thanks to the Go team for generics support and the maintainers of Chi and Bun for their excellent libraries.

## Author

Scott Goldie ([@sjgoldie](https://github.com/sjgoldie))
