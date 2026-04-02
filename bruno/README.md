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

### Automated Script (Recommended)

The easiest way to run all Bruno tests is using the automated script:

```bash
# Run all example tests
./scripts/run-bruno-tests.sh all

# Override port (default 8080)
PORT=9090 ./scripts/run-bruno-tests.sh all

# Run specific example tests
./scripts/run-bruno-tests.sh simple
./scripts/run-bruno-tests.sh nested
./scripts/run-bruno-tests.sh auth
./scripts/run-bruno-tests.sh validator
./scripts/run-bruno-tests.sh audit
./scripts/run-bruno-tests.sh uuid
./scripts/run-bruno-tests.sh custom
./scripts/run-bruno-tests.sh relations
./scripts/run-bruno-tests.sh files-proxy
./scripts/run-bruno-tests.sh files-signed
./scripts/run-bruno-tests.sh actions
./scripts/run-bruno-tests.sh batch
./scripts/run-bruno-tests.sh query
```

The script automatically:
- Checks if the port is in use (fails gracefully if occupied)
- Starts the appropriate example server
- Runs the Bruno tests
- Cleans up the server when done

### Using Bruno GUI

**Start a server and run tests interactively:**

1. Start one of the example servers (see sections below)
2. Open Bruno GUI
3. Navigate to the desired example folder
4. Click "Run Collection" to run all tests

### Using Bruno CLI (Manual)

**Run tests from command line:**

```bash
# Install Bruno CLI
npm install -g @usebruno/cli

# Start the server in one terminal
cd examples/simple && go run main.go

# Run tests in another terminal
bru run bruno/simple-example --env local
```

**CI/CD Integration:**

```bash
# Run tests with output formatting
bru run bruno/simple-example --env local --reporter json
bru run bruno/simple-example --env local --reporter junit
bru run bruno/simple-example --env local --reporter html

# Note: Bruno v3+ defaults to safe sandbox mode.
# Use --sandbox=developer for tests that use bru.setVar() or script blocks:
bru run bruno/simple-example --env local --sandbox=developer
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

**Tests cover (17 tests):**
- Health check
- Create user
- Get all users
- Get single user
- Update user
- Delete user
- Verify deletion (404)
- Filtering (equality, comparison operators, LIKE patterns)
- Sorting (ascending, descending, multiple fields)
- Pagination (limit, offset, count)

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

**Tests cover (16 tests):**
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

**Tests cover (48 tests):**
1. **Articles** - Public reads, publisher-only writes
2. **Blogs** - Ownership filtering with query parameters:
   - Users see only their blogs, admin sees all
   - Filter by status (ownership + filter combined)
   - Sort by name (ownership + sort combined)
   - Combined filter + sort with ownership
3. **Posts** - Multi-ownership (accessible by author OR editor)
4. **Comments** - Mixed auth (GET is public, POST/PUT/DELETE require auth)
5. **Reports** - MethodList vs MethodGet differentiation
6. **Parent ownership cascade** - Nested resources blocked when parent ownership fails
7. **Relation includes with auth** - `?include=Posts`, `?include=Posts.Comments`, `?include=Blog`:
   - Owner includes child/parent relations
   - Admin bypasses ownership on includes
   - No auth blocked (401)
   - Nested includes through ownership chain

**Test users (bearer tokens):**
- `user:alice:user` - Regular user
- `user:bob:user,admin` - Admin user
- `user:charlie:user,publisher` - Publisher user
- `user:diana:user,moderator` - Moderator user

### UUID Example Tests

Tests UUID primary keys with nested resources.

**Start the server:**
```bash
cd examples/uuid_pk
go run main.go
```

**Run tests in Bruno:**
- Open the `uuid-example` folder
- Click "Run Collection" to run all tests in sequence

**Tests cover (14 tests):**
1. **Blog CRUD** - Create, read, update, delete blogs with UUID PKs
2. **Nested Posts** - Create posts under blogs with UUID foreign keys
3. **Parent Validation** - Posts are filtered by parent blog UUID
4. **UUID in URLs** - Full UUID support in route parameters

### Validator Example Tests

Tests custom validation with state machine transitions.

**Start the server:**
```bash
cd examples/validator
go run main.go
```

**Tests cover (16 tests):**
- Create validation (status must be pending, priority 1-5)
- Status transitions (pending → in_progress → completed)
- Invalid transitions rejected
- Delete restrictions on completed tasks

### Audit Example Tests

Tests transactional audit logging.

**Start the server:**
```bash
cd examples/audit
go run main.go
```

**Tests cover (8 tests):**
- Audit records created for all mutations
- Old and new state captured
- Audit runs in same transaction

### Query Example Tests

Tests comprehensive filtering, sorting, pagination, and sum aggregation.

**Start the server:**
```bash
cd examples/query
go run main.go
```

**Tests cover (36 tests):**
- All filter operators (eq, neq, gt, gte, lt, lte, like, ilike, in, nin, bt, nbt)
- Combined filters with sort and pagination
- Boolean and string edge cases
- Sum aggregation (single, multiple, with filters, with count)
- Sum of non-numeric fields (returns 0)
- Sum of non-allowed and non-existent fields

### Custom Handlers Example Tests

Tests custom handler overrides for Get, GetAll, Create, Update, Delete.

**Start the server:**
```bash
cd examples/custom
go run main.go
```

**Tests cover (16 tests):**
- Custom `/me` endpoint (get/update current user)
- Custom GetAll filtering by owner
- Custom update with validation logic
- Custom delete prevention (audit logs)

### Relations Example Tests

Tests relation includes (`?include=`) and single routes (belongs-to).

**Start the server:**
```bash
cd examples/relations
go run main.go
```

**Tests cover (23 tests):**
- Include child relations (has-many)
- Include parent relations (belongs-to)
- Multiple includes, invalid includes (silently ignored)
- Ownership filtering on includes
- Single routes (GET/PUT) for belongs-to relations
- `/me` endpoint with custom get/update
- Method restrictions on single routes (no POST/DELETE)

### Files Proxy Example Tests

Tests file upload/download with proxy mode (files stream through server).

**Start the server:**
```bash
cd examples/files_proxy
go run main.go
```

**Tests cover (13 tests):**
- Upload files via multipart form
- Get file metadata
- List files
- Download via `/download` endpoint (proxy mode)
- Delete files

### Files Signed URL Example Tests

Tests file upload/download with signed URL mode (direct download from storage).

**Start the server:**
```bash
cd examples/files_signed
go run main.go
```

**Tests cover (13 tests):**
- Upload files via multipart form
- Get file metadata with `download_url` pointing to storage
- Download via signed URL (direct)
- Delete files

### Actions Example Tests

Tests custom action endpoints on resources.

**Start the server:**
```bash
cd examples/actions
go run main.go
```

**Tests cover (12 tests):**
- Cancel and complete actions on orders
- Invalid state transitions rejected
- Action on non-existent resource (404)
- Action with `?include=` on response

### Batch Example Tests

Tests bulk create, update, and delete operations.

**Start the server:**
```bash
cd examples/batch
go run main.go
```

**Tests cover (15 tests):**
- Batch create, update, delete
- Empty batch handling
- Batch update/delete with non-existent IDs
- Batch create with `?include=` on response
- Nested batch create (variants under a product)

## Test Coverage

**Total: 300 end-to-end API tests** across 16 example applications.

These Bruno tests provide **end-to-end API coverage** for the example applications. They complement the unit tests by:

- Testing the full HTTP request/response cycle
- Validating authentication and authorization
- Ensuring proper error responses
- Verifying the framework works correctly in example applications

Combined with unit tests, these integration tests ensure the framework functions correctly in various scenarios.

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
