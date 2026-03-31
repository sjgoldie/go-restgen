# Simple CRUD Example

This example demonstrates basic CRUD operations using go-restgen with an in-memory SQLite database.

## Prerequisites

None! This example uses SQLite in-memory database, so no database setup is required.

## Running

```bash
go run main.go
```

## Testing

This example includes 19 comprehensive Bruno API tests that verify the full CRUD lifecycle plus filtering, sorting, pagination, and request body size limits. See [`../../bruno/README.md`](../../bruno/README.md) for instructions on running the tests.

Run tests with the automated script:
```bash
./scripts/run-bruno-tests.sh simple
```

## Usage

The server will start on port 8080 with the following endpoints:

### Create a user
```bash
curl -X POST http://localhost:8080/users \
  -H "Content-Type: application/json" \
  -d '{"name":"John Doe","email":"john@example.com"}'
```

### Get all users
```bash
curl http://localhost:8080/users
```

### Filtering, Sorting, and Pagination
```bash
# Filter by name
curl 'http://localhost:8080/users?filter[Name]=John%20Doe'

# Filter with operators
curl 'http://localhost:8080/users?filter[Name][like]=John%'

# Sort by name descending
curl 'http://localhost:8080/users?sort=-Name'

# Cursor pagination (default) with total count
curl 'http://localhost:8080/users?limit=10&count=true'

# Next page using cursor from previous response
curl 'http://localhost:8080/users?limit=10&after=<next_cursor>'

# Offset pagination (opt-in)
curl 'http://localhost:8080/users?limit=10&offset=20&count=true'

# Combined
curl 'http://localhost:8080/users?filter[Name][like]=J%&sort=-Name&limit=10&count=true'
```

### Get a specific user
```bash
curl http://localhost:8080/users/1
```

### Update a user
```bash
curl -X PUT http://localhost:8080/users/1 \
  -H "Content-Type: application/json" \
  -d '{"name":"John Updated","email":"john.updated@example.com"}'
```

**Note**: The ID in the URL path is always used. Any ID in the JSON body is ignored (security feature).

### Delete a user
```bash
curl -X DELETE http://localhost:8080/users/1
```

## Code Overview

This example shows:

1. **In-memory SQLite database** - No setup required
2. **Model with timestamps** - Automatic `created_at` and `updated_at` handling via `BeforeAppendModel` hook
3. **Builder API** - Register routes with support for nesting:
   ```go
   b := router.NewBuilder(r)
   router.RegisterRoutes[User](b, "/users",
       router.AllPublic(),
       router.WithFilters("Name", "Email"),
       router.WithSorts("Name", "Email"),
       router.WithPagination(20, 100),
       router.WithMaxBodySize(1024),
   )
   ```
4. **Query parameters** - Filtering, sorting, and pagination with `WithFilters`, `WithSorts`, `WithPagination`
5. **Request body size limits** - Configurable per-resource limit via `WithMaxBodySize` (default: 1 MB)
6. **Security by default** - Path IDs always take precedence over JSON body IDs

## Using PostgreSQL Instead

To use PostgreSQL, replace the datastore initialization:

```go
// Instead of SQLite
db, err := datastore.NewSQLite(":memory:")

// Use PostgreSQL
db, err := datastore.NewPostgres("postgres://user:pass@localhost:5432/dbname?sslmode=disable")
```
