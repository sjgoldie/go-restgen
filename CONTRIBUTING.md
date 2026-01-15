# Contributing to go-restgen

Thank you for your interest in contributing to go-restgen! This document provides guidelines and information for contributors.

## Development Setup

### Prerequisites

- Go 1.24 or higher
- Git
- Node.js 18+ (for Bruno CLI)

### Quick Setup

Run the automated setup script:

```bash
./scripts/setup-hooks.sh
```

This installs:
- pre-commit hooks (automatic code quality checks)
- golangci-lint (comprehensive linting)
- Bruno CLI (API testing)

### Manual Setup

1. Fork the repository
2. Clone your fork:
   ```bash
   git clone https://github.com/YOUR_USERNAME/go-restgen.git
   cd go-restgen
   ```
3. Install development tools:
   ```bash
   # Install pre-commit
   pip3 install pre-commit
   pre-commit install

   # Install golangci-lint
   curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin

   # Install Bruno CLI
   npm install -g @usebruno/cli
   ```
4. Create a branch for your changes:
   ```bash
   git checkout -b feature/your-feature-name
   ```

## Project Structure

```
go-restgen/
├── metadata/          # Type metadata and context helpers
├── datastore/         # Database operations layer
├── service/           # Business logic layer
├── handler/           # HTTP handlers
├── router/            # Route registration helpers
├── filestore/         # File storage abstraction
├── errors/            # Domain error types
├── examples/          # 12 example applications
│   ├── simple/        # Basic CRUD
│   ├── nested_routes/ # Nested resources
│   ├── auth/          # Authentication patterns
│   ├── validator/     # Custom validation
│   ├── audit/         # Audit logging
│   ├── relations/     # Relation includes
│   ├── files_proxy/   # File upload (proxy mode)
│   ├── files_signed/  # File upload (signed URLs)
│   ├── actions/       # Custom actions
│   ├── batch/         # Batch operations
│   ├── custom/        # Custom handlers
│   └── uuid_pk/       # UUID primary keys
└── bruno/             # API integration tests
```

## Development Workflow

### Running Tests

```bash
# Run core framework tests (excluding examples)
go test ./metadata ./datastore ./router ./service ./handler ./errors ./filestore

# Run with coverage (83%)
go test ./metadata ./datastore ./router ./service ./handler ./errors ./filestore -coverprofile=/tmp/coverage.out
go tool cover -func=/tmp/coverage.out

# Run with verbose output
go test -v ./metadata ./datastore ./router ./service ./handler ./errors ./filestore
```

All unit tests use SQLite in-memory databases - no external database required!

### Running Examples

All examples use SQLite in-memory databases - no setup required!

```bash
# Run any example
cd examples/simple && go run main.go
cd examples/auth && go run main.go
cd examples/batch && go run main.go
# etc.
```

Or use the Bruno test script which starts/stops servers automatically:
```bash
./scripts/run-bruno-tests.sh simple
./scripts/run-bruno-tests.sh all
```

### Running API Tests (Bruno)

1. Install Bruno: https://www.usebruno.com/downloads
2. Open Bruno and import the `/bruno` directory
3. Start an example server (see above)
4. Run the corresponding test collection in Bruno

See `bruno/README.md` for detailed instructions.

### Code Style

We enforce code quality through automated checks:

**Pre-commit hooks run automatically:**
- `go fmt` - Code formatting
- `go imports` - Import organization
- `go vet` - Static analysis
- Unit tests - Core package tests
- `go mod tidy` - Dependency management

**Additional checks (CI):**
- `golangci-lint` - Comprehensive linting
- Security scanning (gosec, govulncheck)
- Coverage threshold (≥70%)

**Commit messages:**
- Use conventional commits: `feat:`, `fix:`, `docs:`, `test:`, `chore:`
- Be descriptive: "fix: resolve parent validation in nested routes"
- Reference issues: "closes #42"

## Pull Request Process

### Before Submitting

1. ✅ Run tests: `go test ./metadata ./datastore ./router ./service ./handler ./errors ./filestore`
2. ✅ Run linting: `golangci-lint run`
3. ✅ Check coverage: Ensure ≥70% overall
4. ✅ Add Bruno tests for new examples
5. ✅ Update documentation
6. ✅ Pre-commit hooks pass

### PR Description

Use the PR template and include:
- **Type of change** (bug fix, feature, breaking change, etc.)
- **Description** of what and why
- **Test coverage** percentage
- **Related issues** (closes #X)

### Review Process

1. Automated checks run (tests, lint, security)
2. Coverage is validated (≥70%)
3. Maintainer reviews code
4. Address feedback
5. Merge when approved

See [DEVELOPER.md](DEVELOPER.md) for detailed development guide.

## Testing Guidelines

- Write unit tests for new functions
- Add integration tests for new features
- Ensure tests are deterministic and don't depend on external state
- Use SQLite in-memory databases for testing
- Add Bruno API tests for new example features

Example test:

```go
func TestNewFeature(t *testing.T) {
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
    _, err = db.GetDB().NewCreateTable().Model((*YourModel)(nil)).IfNotExists().Exec(context.Background())
    if err != nil {
        t.Fatal(err)
    }

    // Test
    svc, err := service.New[YourModel]()
    // ... your test code
}
```

## Adding New Features

### Adding a New Handler Method

1. Add method to `handler/handler.go` or `handler/nested.go`
2. Add corresponding service method in `service/service.go`
3. Add datastore method in `datastore/wrapper.go` if needed
4. Write tests
5. Update examples if applicable
6. Update README.md

### Adding Database Support

To add support for a new database:

1. Ensure the database has a Bun dialect
2. Update `datastore.Server` interface if needed
3. Add example implementation
4. Add tests
5. Update documentation

## Documentation

- Keep README.md up to date
- Add code comments for exported functions
- Update examples when adding features
- Include usage examples in function documentation

## Questions?

Feel free to open an issue for:
- Bug reports
- Feature requests
- Documentation improvements
- General questions

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
