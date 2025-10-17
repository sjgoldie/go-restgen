# Testing Guide

go-restgen uses a combination of unit tests, integration tests, and end-to-end API tests to ensure code quality.

## Test Coverage Summary

**Core Framework Coverage: 75.3%** (excluding examples)

- **metadata**: 100.0% - Complete registry and ownership configuration tests
- **router**: 90.1% - Route registration, middleware, and auth tests
- **service**: 88.9% - Comprehensive CRUD operation tests
- **datastore**: 71.6% - Database operations (uses SQLite in-memory)
- **handler**: 62.7% - HTTP handler tests including context error handling

**Integration Test Coverage: 35 end-to-end API tests** (Bruno)

- Simple example: 7 tests
- Nested routes example: 16 tests
- Auth example: 12 tests

All unit tests use SQLite in-memory databases - no external database required!

## Running Tests

### Unit Tests (Core Framework Only)

Run tests excluding example applications:

```bash
go test ./metadata ./datastore ./router ./service ./handler ./errors -coverprofile=/tmp/coverage.out
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
go test ./metadata ./datastore ./router ./service ./handler ./errors -coverprofile=/tmp/coverage.out
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

Located in `bruno/` directory with 35 tests across 3 example applications:

**Simple Example (7 tests)**
- Full CRUD lifecycle
- Basic HTTP operations

**Nested Routes Example (16 tests)**
- 3-level nested resources (Users → Posts → Comments)
- Parent validation (404 when accessing under wrong parent)
- Parent chain validation
- CRUD operations on nested resources

**Auth Example (12 tests)**
- Public vs authenticated endpoints
- Scope-based authorization
- Ownership filtering (users see only their resources)
- Admin bypass (admins see all resources)
- Multi-ownership (author OR editor)
- Mixed auth patterns

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

- **metadata**: 100% (achieved: 100.0%) ✅
- **router**: > 85% (achieved: 90.1%) ✅
- **service**: > 85% (achieved: 88.9%) ✅
- **datastore**: > 65% (achieved: 71.6%) ✅
- **handler**: > 60% (achieved: 62.7%) ✅

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
