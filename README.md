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

## Nested Resources

go-restgen automatically handles parent-child relationships with full chain validation:

**Security Features:**
- Foreign keys are automatically set from the URL path
- Foreign keys in JSON body are ignored (path takes precedence)
- IDs in JSON body are ignored (path takes precedence)
- Parent chain is validated at database level with JOINs
- Returns 404 if resource doesn't belong to parent chain

See the [nested routes example](./examples/nested_routes) for a complete working example with 3-level nesting.

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

### Configure Logging

Handlers log warnings using `log/slog`. **By default, warnings are suppressed** (log level set to Error).

**Enable warnings in development:**

```go
import (
    "log/slog"
    "os"
)

func main() {
    // Enable warnings for debugging
    logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
        Level: slog.LevelWarn,
    }))
    slog.SetDefault(logger)

    // ... rest of your setup
}
```

**Note**: Warning logs may contain sensitive information (IDs, error details). Only enable in development/debugging environments.

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
- **[Authentication & Authorization](./examples/auth)** - Comprehensive auth patterns including scopes, ownership, admin bypass, and multi-ownership

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
- [ ] Optionally retrieve an object's relations when retrieving the object itself
- [ ] MySQL support
- [ ] OpenAPI/Swagger generation

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
