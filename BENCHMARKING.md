# Performance Benchmarking Guide

This guide explains how to run and interpret performance benchmarks for go-restgen.

## Quick Start

```bash
# Run all benchmarks with profiling
./scripts/run-benchmarks.sh

# Run specific benchmark
BENCH_PATTERN="BenchmarkNestedDepth" ./scripts/run-benchmarks.sh

# Run with custom duration
BENCH_TIME=10s ./scripts/run-benchmarks.sh
```

## Benchmark Categories

### 1. Nested Depth Benchmarks (`BenchmarkNestedDepth`)

Tests performance across different nesting levels:

- **Depth1_Blog** - Root level resources (`/blogs/{id}`)
- **Depth2_Post** - 2-level nesting (`/blogs/{id}/posts/{id}`)
- **Depth3_Comment** - 3-level nesting (`/blogs/{id}/posts/{id}/comments/{id}`)
- **Depth4_Reaction** - 4-level nesting (`/blogs/{id}/posts/{id}/comments/{id}/reactions/{id}`)

**Example:**
```bash
go test -bench=BenchmarkNestedDepth -benchtime=5s -benchmem ./handler
```

**What to look for:**
- Time increase per nesting level
- Memory allocations scaling
- Identify if parent validation adds significant overhead

### 2. Auth Permutations (`BenchmarkAuthPermutations`)

Tests different authentication scenarios:

- **NoAuth** - No authentication provided (should fail)
- **ValidUser** - User with basic scope (should pass)
- **ValidAdmin** - Admin with bypass capabilities (should pass)
- **WrongScope** - Insufficient scopes (should fail)
- **NonOwner** - Valid user but doesn't own resource (should fail)

**Example:**
```bash
go test -bench=BenchmarkAuthPermutations -benchtime=3s -benchmem ./handler
```

**What to look for:**
- Overhead of auth checking
- Performance difference between owner/non-owner
- Admin bypass performance

### 3. CRUD Operations (`BenchmarkOperations`)

Tests different operation types with varying data sizes:

- **GetAll_Empty** - List with no data
- **GetAll_100Items** - List with 100 items
- **GetAll_1000Items** - List with 1000 items
- **Get_Single** - Single item retrieval
- **Post_Create** - Create operation
- **Put_Update** - Update operation

**Example:**
```bash
go test -bench=BenchmarkOperations -benchtime=3s -benchmem ./handler
```

**What to look for:**
- Scaling with dataset size
- Memory usage for large collections
- Create vs read vs update performance

### 4. Nested Collections (`BenchmarkNestedCollections`)

Tests GetAll on nested resources with varying sizes:

- **Posts_10** - 10 posts under a blog
- **Posts_100** - 100 posts under a blog
- **Posts_1000** - 1000 posts under a blog

**Example:**
```bash
go test -bench=BenchmarkNestedCollections -benchtime=3s -benchmem ./handler
```

## Using the Benchmark Script

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `BENCH_TIME` | `3s` | How long to run each benchmark |
| `BENCH_PATTERN` | `.` | Regex pattern to filter benchmarks |
| `OUTPUT_DIR` | `./bench-results` | Where to save results |
| `ENABLE_CPU_PROF` | `true` | Enable CPU profiling |
| `ENABLE_MEM_PROF` | `true` | Enable memory profiling |
| `ENABLE_TRACE` | `false` | Enable execution tracing |

### Examples

**Run specific benchmarks:**
```bash
# Only nested depth benchmarks
BENCH_PATTERN="NestedDepth" ./scripts/run-benchmarks.sh

# Only auth benchmarks
BENCH_PATTERN="Auth" ./scripts/run-benchmarks.sh

# Only specific depth
BENCH_PATTERN="Depth3" ./scripts/run-benchmarks.sh
```

**Long-running benchmarks:**
```bash
# Run for 30 seconds each
BENCH_TIME=30s ./scripts/run-benchmarks.sh
```

**Disable profiling (faster runs):**
```bash
ENABLE_CPU_PROF=false ENABLE_MEM_PROF=false ./scripts/run-benchmarks.sh
```

**Enable tracing (detailed execution analysis):**
```bash
ENABLE_TRACE=true ./scripts/run-benchmarks.sh
```

## Analyzing Results

### Benchmark Output

```
BenchmarkNestedDepth/Depth1_Blog-8      50000    35421 ns/op    4512 B/op    67 allocs/op
BenchmarkNestedDepth/Depth2_Post-8      30000    48932 ns/op    6234 B/op    89 allocs/op
```

Reading the output:
- `50000` - Number of iterations
- `35421 ns/op` - Nanoseconds per operation
- `4512 B/op` - Bytes allocated per operation
- `67 allocs/op` - Number of allocations per operation

### CPU Profiling

**Interactive web UI:**
```bash
go tool pprof -http=:8080 bench-results/cpu_20240115_120000.prof
```

**Command line - top functions:**
```bash
go tool pprof -top10 -cum bench-results/cpu_20240115_120000.prof
```

**Command line - specific function:**
```bash
go tool pprof -list=GetAll bench-results/cpu_20240115_120000.prof
```

**What to look for:**
- Functions consuming >10% of CPU
- Unexpected allocations
- Database query time vs handler overhead
- JSON marshaling/unmarshaling time

### Memory Profiling

**Interactive web UI:**
```bash
go tool pprof -http=:8080 bench-results/mem_20240115_120000.prof
```

**Command line - top allocators:**
```bash
go tool pprof -top10 -alloc_space bench-results/mem_20240115_120000.prof
```

**What to look for:**
- Functions allocating >1MB
- Unnecessary allocations in hot paths
- Potential for object pooling
- Large slice/map growth

### Execution Tracing

**View trace:**
```bash
go tool trace bench-results/trace_20240115_120000.out
```

Opens a web UI showing:
- Goroutine activity
- GC pauses
- Network/syscall blocking
- Per-processor utilization

### Comparing Results

Use `benchstat` to compare runs:

```bash
# Install benchstat
go install golang.org/x/perf/cmd/benchstat@latest

# Compare two runs
benchstat bench-results/bench_before.txt bench-results/bench_after.txt
```

Output shows statistical significance:
```
name                      old time/op    new time/op    delta
NestedDepth/Depth1_Blog   35.4µs ± 2%    28.1µs ± 1%  -20.62%  (p=0.000 n=10+10)
```

## Performance Targets

Based on typical REST API requirements:

| Operation | Target | Good | Needs Work |
|-----------|--------|------|------------|
| Single GET | <1ms | <500µs | >2ms |
| GetAll (100 items) | <5ms | <2ms | >10ms |
| POST/PUT | <2ms | <1ms | >5ms |
| Nested GET (3 levels) | <2ms | <1ms | >5ms |

**Note:** Targets assume in-memory SQLite. Production PostgreSQL will be slower due to network overhead.

## Regression Thresholds

Watch for regressions in these key metrics:

| Benchmark | Expected | Investigate If |
|-----------|----------|----------------|
| `BenchmarkNestedDepth/Depth1_Blog` | ~12-15μs | >30μs |
| `BenchmarkOperations/GetAll_100Items` | ~200μs | >400μs |
| `BenchmarkOperations/GetAll_1000Items` | ~1.5-2ms | >4ms |

**Rule of thumb:** A 2x slowdown or 2x memory increase warrants investigation.

## Continuous Benchmarking

### In CI/CD

Add to your GitHub Actions:

```yaml
- name: Run Benchmarks
  run: |
    go test -bench=. -benchtime=3s -benchmem ./handler > bench-new.txt

- name: Compare with main
  run: |
    git checkout main
    go test -bench=. -benchtime=3s -benchmem ./handler > bench-main.txt
    benchstat bench-main.txt bench-new.txt
```

### Pre-commit Hook

```bash
#!/bin/bash
# .git/hooks/pre-commit

# Run quick benchmarks
BENCH_TIME=1s ENABLE_CPU_PROF=false ENABLE_MEM_PROF=false ./scripts/run-benchmarks.sh

# Fail if major regressions detected
# (implement custom logic based on your needs)
```

## Common Performance Issues

### High Memory Usage

**Symptoms:**
- `B/op` increasing significantly
- Many `allocs/op`

**Common causes:**
- Loading too much data at once (add pagination)
- Not reusing slices/maps
- Creating unnecessary intermediate objects
- JSON encoding allocations

**Solutions:**
```go
// Use streaming JSON encoding for large responses
// Implement cursor-based pagination
// Pool frequently allocated objects
// Use protobuf or msgpack instead of JSON
```

### Slow Nested Queries

**Symptoms:**
- Time increases exponentially with nesting depth
- High database query time

**Common causes:**
- N+1 queries
- Missing database indexes
- Parent validation making separate queries

**Solutions:**
```go
// Use relations to eager-load (when implemented)
// Add composite indexes on foreign keys
// Consider denormalization for deep nesting
```

### Auth Overhead

**Symptoms:**
- Auth scenarios much slower than no-auth
- High time in middleware

**Common causes:**
- Token validation on every request
- Database lookups for permissions
- Expensive scope checking

**Solutions:**
```go
// Cache auth tokens
// Use in-memory permission store
// Batch permission checks
```

## Advanced Usage

### Custom Benchmarks

Add your own benchmarks to `handler/handler_bench_test.go`:

```go
func BenchmarkMyFeature(b *testing.B) {
    // Setup
    if err := setupBenchDB(); err != nil {
        b.Fatal(err)
    }

    // Prepare test data
    cleanupBenchDB(b)
    // ... seed data

    b.ResetTimer()
    b.ReportAllocs()

    for i := 0; i < b.N; i++ {
        // Your code to benchmark
    }
}
```

### Sub-benchmarks

Test multiple scenarios:

```go
func BenchmarkMyFeature(b *testing.B) {
    scenarios := []struct{
        name string
        size int
    }{
        {"Small", 10},
        {"Medium", 100},
        {"Large", 1000},
    }

    for _, sc := range scenarios {
        b.Run(sc.name, func(b *testing.B) {
            // Benchmark with sc.size
        })
    }
}
```

### Memory Baseline

Compare memory before/after:

```go
func BenchmarkMemoryBaseline(b *testing.B) {
    var m runtime.MemStats
    runtime.ReadMemStats(&m)
    before := m.Alloc

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        // Your code
    }
    b.StopTimer()

    runtime.ReadMemStats(&m)
    after := m.Alloc

    b.ReportMetric(float64(after-before)/float64(b.N), "B/op")
}
```

## Troubleshooting

### Benchmarks too fast/slow

```bash
# Increase iterations
go test -bench=. -benchtime=10s

# Or set minimum iterations
go test -bench=. -benchtime=100000x
```

### Inconsistent results

```bash
# Run multiple times and use benchstat
for i in {1..10}; do
  go test -bench=BenchmarkMyTest >> results.txt
done
benchstat results.txt
```

### Out of memory

```bash
# Reduce data size in seedBenchData()
# Or increase available memory:
GOMEMLIMIT=4GiB go test -bench=.
```

## Resources

- [Go Benchmark Documentation](https://pkg.go.dev/testing#hdr-Benchmarks)
- [pprof User Guide](https://github.com/google/pprof/blob/main/doc/README.md)
- [High Performance Go Workshop](https://dave.cheney.net/high-performance-go-workshop/dotgo-paris.html)
- [benchstat Documentation](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat)
