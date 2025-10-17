# Bruno API Tests for go-restgen Examples

This directory contains API integration tests for the example applications using [Bruno](https://www.usebruno.com/).

## Setup

### GUI (Recommended for Development)

1. Install Bruno: https://www.usebruno.com/downloads
2. Open Bruno and import this collection by selecting the `/bruno` directory

### CLI (Recommended for CI/CD)

Install Bruno CLI via npm:

```bash
npm install -g @usebruno/cli
```

Or use npx without installing:

```bash
npx @usebruno/cli run /path/to/bruno/folder
```

## Running Tests

### Using Bruno GUI

**Start a server and run tests interactively:**

1. Start one of the example servers (see sections below)
2. Open Bruno GUI
3. Navigate to the example folder (simple-example, nested-example, or auth-example)
4. Click "Run Collection" to run all tests

### Using Bruno CLI

**Run tests from command line:**

```bash
# Install Bruno CLI
npm install -g @usebruno/cli

# Start the server in one terminal
cd examples/simple && go run main.go

# Run tests in another terminal
bru run bruno/simple-example --env local

# Or run all examples (requires all servers running)
bru run bruno --env local
```

**CI/CD Integration:**

```bash
# Run tests with output formatting
bru run bruno/simple-example --env local --reporter json
bru run bruno/simple-example --env local --reporter junit
bru run bruno/simple-example --env local --reporter html
```

---

### Simple Example Tests

Tests the basic CRUD operations with no authentication.

**Start the server:**
```bash
cd examples/simple
go run main.go
```

**Run tests in Bruno:**
- Open the `simple-example` folder
- Click "Run Collection" to run all tests in sequence

**Tests cover:**
- Health check
- Create user
- Get all users
- Get single user
- Update user
- Delete user
- Verify deletion (404)

### Nested Routes Example Tests

Tests 3-level nested resources (Users → Posts → Comments) with automatic parent validation.

**Start the server:**
```bash
cd examples/nested_routes
go run main.go
```

**Run tests in Bruno:**
- Open the `nested-example` folder
- Click "Run Collection" to run all tests in sequence

**Tests cover:**
1. **User Creation** - Create multiple users
2. **Nested Post Creation** - Create posts under specific users
3. **Parent Filtering** - Posts are filtered by parent user ID
4. **Parent Validation** - Accessing post under wrong user returns 404
5. **3-Level Nesting** - Create comments under specific posts
6. **Parent Chain Validation** - Full path validation (user → post → comment)
7. **CRUD on Nested Resources** - Update/delete comments with parent validation
8. **Cascade Verification** - Verify deletions work correctly in nested context

### Auth Example Tests

Tests various authentication and authorization patterns including:
- Public vs authenticated endpoints
- Scope-based authorization
- Ownership-based access control
- Admin bypass
- Multi-ownership (author OR editor)

**Start the server:**
```bash
cd examples/auth
go run main.go
```

**Run tests in Bruno:**
- Open the `auth-example` folder
- Click "Run Collection" to run all tests in sequence

**Tests cover:**
1. **Articles** - Public reads, publisher-only writes
2. **Blogs** - Ownership filtering (users see only their blogs, admin sees all)
3. **Posts** - Multi-ownership (accessible by author OR editor)
4. **Comments** - Mixed auth (GET is public, POST/PUT/DELETE require auth)

**Test users (bearer tokens):**
- `user:alice:user` - Regular user
- `user:bob:user,admin` - Admin user
- `user:charlie:user,publisher` - Publisher user
- `user:diana:user,moderator` - Moderator user

## Test Coverage

These Bruno tests provide **end-to-end API coverage** for the example applications. They complement the unit tests by:

- Testing the full HTTP request/response cycle
- Validating authentication and authorization
- Ensuring proper error responses
- Verifying the framework works correctly in real applications

Combined with unit tests (75.3% code coverage), these integration tests ensure the framework functions correctly in production scenarios.

## Coverage Summary

**Unit Test Coverage (Core Framework):**
- metadata: 100.0%
- router: 90.1%
- service: 88.9%
- datastore: 71.6%
- handler: 62.7%
- **Total: 75.3%**

**Integration Test Coverage (Bruno):**
- Simple example: 7 tests (full CRUD lifecycle)
- Nested routes example: 16 tests (nested resources, parent validation)
- Auth example: 12 tests (auth patterns, ownership, scopes)
- **Total: 35 end-to-end API tests**

## Continuous Integration

Bruno CLI can be easily integrated into CI/CD pipelines:

### GitHub Actions Example

```yaml
name: API Tests

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Setup Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'

      - name: Install Bruno CLI
        run: npm install -g @usebruno/cli

      - name: Start Simple Example Server
        run: |
          cd examples/simple
          go run main.go &
          sleep 2  # Wait for server to start

      - name: Run Simple Example Tests
        run: bru run bruno/simple-example --env local --reporter json

      - name: Run All Tests
        run: |
          # Start all servers and run tests
          # Add similar steps for other examples
```

### Local Testing Script

Create a `test-api.sh` script for running all API tests:

```bash
#!/bin/bash
set -e

echo "Starting servers..."
cd examples/simple && go run main.go &
SIMPLE_PID=$!
cd ../..

cd examples/nested_routes && go run main.go &
NESTED_PID=$!
cd ../..

cd examples/auth && go run main.go &
AUTH_PID=$!
cd ../..

sleep 3  # Wait for servers to start

echo "Running Bruno tests..."
bru run bruno --env local --reporter json

# Cleanup
kill $SIMPLE_PID $NESTED_PID $AUTH_PID
echo "Tests complete!"
```

## Adding More Tests

To add new tests:

1. Create a new `.bru` file in the appropriate folder
2. Use sequential naming (e.g., `13-new-test.bru`)
3. Set the `seq` value in the meta block for execution order
4. Use `{{variables}}` for dynamic values
5. Use `script:post-response` to save values for subsequent tests

Example:
```
meta {
  name: Test Name
  type: http
  seq: 13
}

get {
  url: {{baseUrl}}/endpoint
  body: none
  auth: bearer
}

auth:bearer {
  token: user:alice:user
}

assert {
  res.status: eq 200
}

script:post-response {
  bru.setVar("myVar", res.body.id);
}
```
