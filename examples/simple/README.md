# Simple CRUD Example

This example demonstrates basic CRUD operations using go-restgen with an in-memory SQLite database.

## Prerequisites

None! This example uses SQLite in-memory database, so no database setup is required.

## Running

```bash
go run main.go
```

## Testing

This example includes 7 comprehensive Bruno API tests that verify the full CRUD lifecycle. See [`../../bruno/README.md`](../../bruno/README.md) for instructions on running the tests.

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
   router.RegisterRoutes[User](b, "/users")
   ```
4. **Security by default** - Path IDs always take precedence over JSON body IDs

## Using PostgreSQL Instead

To use PostgreSQL, replace the datastore initialization:

```go
// Instead of SQLite
db, err := datastore.NewSQLite(":memory:")

// Use PostgreSQL
db, err := datastore.NewPostgres("postgres://user:pass@localhost:5432/dbname?sslmode=disable")
```
