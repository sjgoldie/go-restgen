# Claude Code Instructions for go-restgen

## Before Starting a Feature

Before implementing any new feature that affects handler, datastore, or service packages:

1. **Save baseline benchmarks** for comparison after the feature:
   ```bash
   go test -run='^$' -bench=. -benchmem ./handler > /tmp/bench_before.txt
   ```

2. **Run and review current test coverage**:
   ```bash
   go test ./metadata ./datastore ./router ./service ./handler ./errors -coverprofile=/tmp/coverage.out
   go tool cover -func=/tmp/coverage.out | tail -10
   ```

## After Implementing a Feature

1. **Run unit tests**:
   ```bash
   go test ./metadata ./datastore ./router ./service ./handler ./errors
   ```

2. **Compare benchmarks** to ensure no performance regression:
   ```bash
   go test -run='^$' -bench=. -benchmem ./handler > /tmp/bench_after.txt
   # If benchstat is installed:
   benchstat /tmp/bench_before.txt /tmp/bench_after.txt
   ```

3. **Run Bruno integration tests**:
   ```bash
   ./scripts/run-bruno-tests.sh simple   # Just simple example
   ./scripts/run-bruno-tests.sh all      # All examples
   ```

4. **Check coverage hasn't dropped**:
   ```bash
   go test ./metadata ./datastore ./router ./service ./handler ./errors -coverprofile=/tmp/coverage.out
   go tool cover -func=/tmp/coverage.out | grep total
   ```

## Key Benchmark Thresholds

Watch for regressions in these key metrics:
- `BenchmarkNestedDepth/Depth1_Blog`: Should be ~12-15μs
- `BenchmarkOperations/GetAll_100Items`: Should be ~200μs
- `BenchmarkOperations/GetAll_1000Items`: Should be ~1.5-2ms

A 2x slowdown or 2x memory increase warrants investigation.

## Running Tests

### Unit Tests
```bash
go test ./metadata ./datastore ./router ./service ./handler ./errors -v
```

### Bruno Integration Tests
Use the script which handles server start/stop automatically:
```bash
./scripts/run-bruno-tests.sh simple   # Simple CRUD example
./scripts/run-bruno-tests.sh nested   # Nested routes example
./scripts/run-bruno-tests.sh auth     # Auth example
./scripts/run-bruno-tests.sh all      # All examples
```

### Full Benchmark Suite
```bash
./scripts/run-benchmarks.sh
```

## Project Structure

- `handler/` - HTTP handlers (CRUD operations)
- `service/` - Business logic layer
- `datastore/` - Database operations (Bun ORM)
- `router/` - Route registration and middleware
- `metadata/` - Type metadata and context helpers
- `examples/` - Working example applications
- `bruno/` - API integration tests (Bruno format)

## Common Tasks

### Adding a New Query Feature
1. Extend `metadata/metadata.go` with new options
2. Add parser in `handler/handler.go`
3. Implement in `datastore/wrapper.go`
4. Add unit tests in corresponding `*_test.go` files
5. Update Bruno tests for affected examples
6. Update README.md documentation

### Adding New Route Options
1. Add to `router/query.go` or create new options file
2. Update `router/builder.go` to process options
3. Add tests in `router/*_test.go`
