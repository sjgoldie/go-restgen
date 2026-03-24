# Testing Guide

go-restgen uses a combination of unit tests, integration tests, and end-to-end API tests to ensure code quality.

## Test Coverage Summary

**Core Framework Coverage: 83.0%** (excluding examples)

- **metadata**: 98.4% - Registry and ownership configuration tests
- **router**: 90.2% - Route registration, middleware, and auth tests
- **filestore**: 88.9% - File storage abstraction tests
- **service**: 82.7% - Comprehensive CRUD operation tests
- **handler**: 80.7% - HTTP handler tests
- **datastore**: 78.6% - Database operations (uses SQLite in-memory)
- **errors**: 100.0% - Domain error types

**Integration Test Coverage: ~197 end-to-end API tests** (Bruno) across 12 examples

All unit tests use SQLite in-memory databases - no external database required!

## Running Tests

### Unit Tests (Core Framework Only)

Run tests excluding example applications:

```bash
go test ./metadata ./datastore ./router ./service ./handler ./errors ./filestore -coverprofile=/tmp/coverage.out
go tool cover -func=/tmp/coverage.out
```

This gives accurate framework coverage (75.3%) without including example applications.

### All Tests (Including Examples)

```bash
go test ./...
```

All tests use SQLite in-memory databases, so no external database setup is required!

### With Coverage HTML Report

```bash
go test ./metadata ./datastore ./router ./service ./handler ./errors ./filestore -coverprofile=/tmp/coverage.out
go tool cover -html=/tmp/coverage.out
```

### Verbose Output

```bash
go test ./... -v
```

### Coverage HTML Report

```bash
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## Test Structure

All tests use SQLite in-memory databases for fast, isolated testing.

### Unit Tests

Located in:
- `service/service_test.go` - Service layer CRUD operations
- `handler/handler_test.go` - HTTP handler tests
- `router/router_test.go` - Route registration and middleware

### Integration Tests (Go)

Located in:
- `datastore/wrapper_test.go` - Full CRUD with SQLite, ownership filtering
- `router/*_test.go` - Auth middleware and route registration

Tests verify:
- ✅ Full CRUD operations (Create, Read, Update, Delete)
- ✅ Nested resource operations with 3-level nesting
- ✅ Parent-child relationship validation
- ✅ Foreign key auto-population from URL paths
- ✅ Security (path IDs take precedence over JSON)
- ✅ Authentication and authorization patterns
- ✅ Ownership-based access control
- ✅ Timestamp handling (CreatedAt, UpdatedAt)

### End-to-End API Tests (Bruno)

Located in `bruno/` directory with 300 tests across 16 example applications.

**Running All Tests:**
```bash
# Automated script (recommended)
./scripts/run-bruno-tests.sh all

# Run specific example
./scripts/run-bruno-tests.sh simple
./scripts/run-bruno-tests.sh nested
./scripts/run-bruno-tests.sh auth
./scripts/run-bruno-tests.sh custom
```

**Examples and Test Counts:**
- **simple** (19 tests) - CRUD, filtering, sorting, pagination
- **nested** (16 tests) - 3-level nested resources, parent validation
- **auth** (48 tests) - Scopes, ownership, admin bypass
- **uuid** (14 tests) - UUID primary keys
- **validator** (16 tests) - Custom validation
- **audit** (8 tests) - Audit logging
- **relations** (24 tests) - Relation includes (?include=)
- **files-proxy** (13 tests) - File upload with proxy mode
- **files-signed** (13 tests) - File upload with signed URLs
- **actions** (12 tests) - Custom action endpoints
- **batch** (15 tests) - Batch create/update/delete, nested batch create
- **custom** (16 tests) - Custom handler functions
- **custom-join** (11 tests) - Custom join fields
- **query** (36 tests) - Filtering, sorting, pagination, sum
- **tenant** (29 tests) - Multi-tenant isolation
- **anything** (10 tests) - Anything funcs, SSE, webhooks

See `bruno/README.md` for running instructions.

## Writing Tests

### Test Pattern with SQLite

```go
func TestYourFeature(t *testing.T) {
    // Setup in-memory database
    db, err := datastore.NewSQLite(":memory:")
    if err != nil {
        t.Fatal(err)
    }

    if err := datastore.Initialize(db); err != nil {
        t.Fatal(err)
    }
    defer datastore.Cleanup()

    // Create schema
    _, err = db.GetDB().NewCreateTable().Model((*YourModel)(nil)).Exec(context.Background())
    if err != nil {
        t.Fatal(err)
    }

    // Run your tests
    svc, err := service.New[YourModel]()
    if err != nil {
        t.Fatal(err)
    }

    // ... your test code
}
```

## Continuous Integration

For CI/CD pipelines:

```yaml
# Example GitHub Actions
- name: Run tests with coverage
  run: |
    go test -coverprofile=coverage.out ./...
    go tool cover -func=coverage.out
```

## Coverage Goals

- **errors**: 100% (achieved: 100.0%) ✅
- **metadata**: > 95% (achieved: 98.4%) ✅
- **router**: > 85% (achieved: 90.2%) ✅
- **filestore**: > 85% (achieved: 88.9%) ✅
- **service**: > 80% (achieved: 82.7%) ✅
- **handler**: > 75% (achieved: 80.7%) ✅
- **datastore**: > 75% (achieved: 78.6%) ✅

Main coverage gaps are in error path testing that requires mocking (service unavailability, rare edge cases).

## Testing Philosophy

go-restgen follows a pragmatic testing approach:

1. **All tests use SQLite** - No external dependencies required
2. **In-memory databases** - Fast, isolated test execution
3. **Integration tests** - Verify database operations work correctly
4. **Type safety** - Verified at compile time through generic usage

This allows:
- ✅ Fast test execution in development
- ✅ No setup required - works out of the box
- ✅ Comprehensive integration testing
- ✅ Confidence in production deployment

## Test Maintenance

When adding new features:

1. Add tests using SQLite in-memory database
2. Ensure proper cleanup with defer
3. Test both happy path and error cases
4. Update coverage goals if needed
