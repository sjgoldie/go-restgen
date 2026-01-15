# Developer Guide

This guide is for contributors and maintainers of go-restgen.

## Quick Setup

Run the setup script to install all development tools:

```bash
./scripts/setup-hooks.sh
```

This installs:
- **pre-commit** - Git hooks for local validation
- **golangci-lint** - Comprehensive linting
- **Bruno CLI** - API testing (optional)

## Development Workflow

### 1. Local Development

```bash
# Run tests (fast - core packages only)
go test ./metadata ./datastore ./router ./service ./handler ./errors ./filestore

# Run tests with coverage
go test ./metadata ./datastore ./router ./service ./handler ./errors ./filestore -coverprofile=/tmp/coverage.out
go tool cover -func=/tmp/coverage.out

# Run linting
golangci-lint run

# Format code
gofmt -w -s .
goimports -w -local github.com/sjgoldie/go-restgen .

# Run API tests (requires server running)
cd examples/simple && go run main.go &
sleep 2
bru run bruno/simple-example --env local
```

### 2. Pre-commit Hooks

Hooks run automatically before each commit:

✅ **go fmt** - Auto-formats code
✅ **go imports** - Organizes imports
✅ **go vet** - Static analysis
✅ **go test** - Runs unit tests
✅ **go mod tidy** - Cleans dependencies

**Skip hooks** (only if necessary):
```bash
git commit --no-verify
```

### 3. Pull Request Checklist

Before creating a PR:

- [ ] All tests pass locally
- [ ] Code is formatted (`go fmt`, `goimports`)
- [ ] Linting passes (`golangci-lint run`)
- [ ] Coverage is maintained (≥70%)
- [ ] Documentation is updated
- [ ] Bruno tests added for new examples

## CI/CD Pipeline

### GitHub Actions Workflows

**1. Tests** (`.github/workflows/test.yml`)
- Runs on: push to main, PRs
- Unit tests with coverage (70% minimum)
- API integration tests (Bruno CLI)
- Uploads coverage to Codecov

**2. Lint** (`.github/workflows/lint.yml`)
- Runs on: push to main, PRs
- golangci-lint with comprehensive checks
- go fmt validation
- go vet static analysis
- go mod tidy verification

**3. Security** (`.github/workflows/security.yml`)
- Runs on: push, PRs, weekly schedule
- gosec security scanning
- govulncheck vulnerability detection
- gitleaks secret scanning

**4. Release** (`.github/workflows/release.yml`)
- Runs on: version tags (v*)
- Runs tests
- Generates changelog
- Creates GitHub release

## Tools & Configuration

### golangci-lint (`.golangci.yml`)

Enabled linters:
- **bodyclose** - HTTP response body must be closed
- **dupl** - Code duplication detection
- **errcheck** - Unchecked errors
- **gosec** - Security issues
- **govet** - Go vet checks
- **staticcheck** - Advanced static analysis
- **stylecheck** - Code style
- And more...

Run: `golangci-lint run`

### pre-commit (`.pre-commit-config.yaml`)

Local git hooks that run before commit:
- Trailing whitespace removal
- EOF fixing
- YAML validation
- Large file detection
- Go formatting
- Go imports
- Go vet
- Unit tests
- Module tidying

Install: `pre-commit install`
Run manually: `pre-commit run --all-files`

### Dependabot (`.github/dependabot.yml`)

Automated dependency updates:
- Go modules (weekly)
- GitHub Actions (weekly)

## Testing Strategy

### Unit Tests (83% coverage)

```bash
# Core packages only (excludes examples)
go test ./metadata ./datastore ./router ./service ./handler ./errors ./filestore -coverprofile=/tmp/coverage.out

# View coverage by function
go tool cover -func=/tmp/coverage.out

# HTML coverage report
go tool cover -html=/tmp/coverage.out
```

### Integration Tests (~197 Bruno tests across 12 examples)

```bash
# Install Bruno CLI
npm install -g @usebruno/cli

# Run specific example
cd examples/simple && go run main.go &
bru run bruno/simple-example --env local

# Run all tests (start all servers first)
bru run bruno --env local --reporter json
```

### Coverage Requirements

- **Minimum**: 70% overall
- **Target**: 80%+
- **errors**: 100%
- **metadata**: >95%
- **router**: >85%
- **filestore**: >85%
- **service**: >80%
- **handler**: >75%
- **datastore**: >75%

## Release Process

### Creating a Release

1. **Update version** in relevant files
2. **Commit changes**: `git commit -m "chore: bump version to vX.Y.Z"`
3. **Create tag**: `git tag -a vX.Y.Z -m "Release vX.Y.Z"`
4. **Push tag**: `git push origin vX.Y.Z`
5. GitHub Actions automatically creates the release

### Semantic Versioning

- **vX.0.0** - Major (breaking changes)
- **vX.Y.0** - Minor (new features, backward compatible)
- **vX.Y.Z** - Patch (bug fixes)

## Debugging CI Failures

### Tests Failing

```bash
# Run exact CI command locally
go test ./metadata ./datastore ./router ./service ./handler ./errors ./filestore -race -coverprofile=coverage.out

# Check specific package
go test -v ./handler
```

### Linting Failing

```bash
# Run linter locally
golangci-lint run

# Auto-fix issues
golangci-lint run --fix

# Check specific file
golangci-lint run path/to/file.go
```

### Coverage Below Threshold

```bash
# See uncovered code
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Bruno Tests Failing

```bash
# Run with verbose output
bru run bruno/simple-example --env local -r json --verbose

# Check server logs
cd examples/simple && go run main.go
# Look for errors in output
```

## Common Tasks

### Adding a New Feature

1. Create feature branch: `git checkout -b feature/my-feature`
2. Write tests first (TDD)
3. Implement feature
4. Add Bruno tests (if example-related)
5. Update documentation
6. Run full test suite
7. Create PR

### Fixing a Bug

1. Create bug branch: `git checkout -b fix/bug-description`
2. Write failing test that reproduces bug
3. Fix the bug
4. Verify test passes
5. Create PR

### Updating Dependencies

```bash
# Update all dependencies
go get -u ./...
go mod tidy

# Run tests to verify
go test ./...

# Or let Dependabot handle it automatically
```

## Tips

- **Use pre-commit hooks** - Catches issues early
- **Run tests locally** - Faster feedback than CI
- **Check coverage** - Maintain high coverage
- **Write Bruno tests** - For API changes
- **Keep commits small** - Easier to review
- **Write clear commit messages** - Use conventional commits

## Resources

- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- [Effective Go](https://go.dev/doc/effective_go)
- [golangci-lint Linters](https://golangci-lint.run/usage/linters/)
- [pre-commit](https://pre-commit.com/)
- [Bruno Documentation](https://docs.usebruno.com/)

## Questions?

Open an issue or discussion on GitHub!
