# Authentication & Authorization Example

This example demonstrates all authentication and authorization features of go-restgen using a blog platform scenario.

## Overview

A simple blog platform demonstrating:
- Public access (reading articles, posts, comments)
- Scope-based authorization (publisher, moderator, admin)
- Ownership-based access control (users manage their own resources)
- Admin bypass (admins can manage everything)
- Method-specific auth (different auth per HTTP method)
- Nested routes with different auth at each level
- Multiple ownership fields (OR logic)
- MethodAll override pattern

## Running the Example

```bash
cd examples/auth
go run main.go
```

The server starts on `http://localhost:8080` with an in-memory SQLite database.

## Testing

This example includes 12 comprehensive Bruno API tests that verify all auth patterns. See [`../../bruno/README.md`](../../bruno/README.md) for instructions on running the tests.

## Authentication

This example uses a simple fake bearer token system for demonstration. In production, you would replace this with real JWT validation, OAuth, Auth0, etc.

### Token Format

```
Authorization: Bearer user:<userID>:<scope1>,<scope2>,...
```

### Example Tokens

```bash
# Alice - Regular user
Authorization: Bearer user:alice:user

# Bob - Admin user
Authorization: Bearer user:bob:user,admin

# Charlie - Publisher
Authorization: Bearer user:charlie:user,publisher

# Diana - Moderator
Authorization: Bearer user:diana:user,moderator
```

## Models & Auth Patterns

### 1. Article - Scope-Based Authorization

Public reads, requires `publisher` scope for writes.

**Routes:**
- `GET /articles` - Public
- `POST /articles` - Requires `publisher` scope
- `GET /articles/{id}` - Public
- `PUT /articles/{id}` - Requires `publisher` scope
- `DELETE /articles/{id}` - Requires `publisher` scope

**Examples:**

```bash
# Anyone can read articles (no auth)
curl http://localhost:8080/articles

# Create article (requires publisher scope)
curl -X POST http://localhost:8080/articles \
  -H "Authorization: Bearer user:charlie:user,publisher" \
  -H "Content-Type: application/json" \
  -d '{"title":"My Article","content":"Article content here"}'

# Without publisher scope - fails with 403
curl -X POST http://localhost:8080/articles \
  -H "Authorization: Bearer user:alice:user" \
  -H "Content-Type: application/json" \
  -d '{"title":"My Article","content":"Content"}'
# Response: forbidden (403)
```

### 2. Author - Admin-Only Writes

Public reads, requires `admin` scope for all write operations.

**Routes:**
- `GET /authors` - Public
- `POST /authors` - Requires `admin` scope
- `GET /authors/{id}` - Public
- `PUT /authors/{id}` - Requires `admin` scope
- `DELETE /authors/{id}` - Requires `admin` scope

**Examples:**

```bash
# Anyone can read authors
curl http://localhost:8080/authors

# Create author (requires admin)
curl -X POST http://localhost:8080/authors \
  -H "Authorization: Bearer user:bob:user,admin" \
  -H "Content-Type: application/json" \
  -d '{"name":"John Doe","bio":"Tech writer"}'

# Without admin scope - fails
curl -X POST http://localhost:8080/authors \
  -H "Authorization: Bearer user:alice:user" \
  -H "Content-Type: application/json" \
  -d '{"name":"John Doe","bio":"Tech writer"}'
# Response: forbidden (403)
```

### 3. Blog - Ownership-Based Access

Users can only see and manage their own blogs. Admins can see/manage all blogs.

**Auth Pattern:** Ownership with admin bypass

**Routes:**
- `GET /blogs` - Returns only user's blogs (admin sees all)
- `POST /blogs` - Creates blog owned by authenticated user
- `GET /blogs/{id}` - Returns blog if owned by user (or admin)
- `PUT /blogs/{id}` - Updates blog if owned by user (or admin)
- `DELETE /blogs/{id}` - Deletes blog if owned by user (or admin)

**Examples:**

```bash
# Alice creates her blog (author_id auto-set to "alice")
curl -X POST http://localhost:8080/blogs \
  -H "Authorization: Bearer user:alice:user" \
  -H "Content-Type: application/json" \
  -d '{"name":"Alice's Blog","description":"My personal blog"}'
# Response: {"id":1,"author_id":"alice","name":"Alice's Blog",...}

# Alice sees only her blogs
curl http://localhost:8080/blogs \
  -H "Authorization: Bearer user:alice:user"
# Response: [{"id":1,"author_id":"alice",...}]

# Bob (different user) sees only his blogs (empty)
curl http://localhost:8080/blogs \
  -H "Authorization: Bearer user:bob:user"
# Response: []

# Admin sees all blogs
curl http://localhost:8080/blogs \
  -H "Authorization: Bearer user:bob:user,admin"
# Response: [{"id":1,"author_id":"alice",...}]

# Alice can't access Bob's blog (returns 404, not 403, to avoid leaking existence)
curl http://localhost:8080/blogs/2 \
  -H "Authorization: Bearer user:alice:user"
# Response: not found (404)

# Admin can access anyone's blog
curl http://localhost:8080/blogs/1 \
  -H "Authorization: Bearer user:bob:user,admin"
# Response: {"id":1,"author_id":"alice",...}
```

### 4. Post - Multiple Ownership Fields

Nested under blogs. Can be owned by `author_id` OR `editor_id` (OR logic). Admins bypass ownership.

**Auth Pattern:** Multiple ownership fields with admin bypass

**Routes:**
- `GET /blogs/{blogId}/posts` - Returns posts owned by user (author OR editor)
- `POST /blogs/{blogId}/posts` - Creates post owned by user (author_id auto-set)
- `GET /blogs/{blogId}/posts/{id}` - Returns post if user is author OR editor (or admin)
- `PUT /blogs/{blogId}/posts/{id}` - Updates post if user is author OR editor (or admin)
- `DELETE /blogs/{blogId}/posts/{id}` - Deletes post if user is author OR editor (or admin)

**Examples:**

```bash
# Alice creates a post (author_id auto-set to "alice")
curl -X POST http://localhost:8080/blogs/1/posts \
  -H "Authorization: Bearer user:alice:user" \
  -H "Content-Type: application/json" \
  -d '{"title":"My First Post","content":"Post content"}'
# Response: {"id":1,"blog_id":1,"author_id":"alice","editor_id":"",...}

# Alice can access her post (she's the author)
curl http://localhost:8080/blogs/1/posts/1 \
  -H "Authorization: Bearer user:alice:user"
# Response: {"id":1,"author_id":"alice",...}

# Charlie (not author or editor) cannot access
curl http://localhost:8080/blogs/1/posts/1 \
  -H "Authorization: Bearer user:charlie:user"
# Response: not found (404)

# Alice adds Charlie as editor
curl -X PUT http://localhost:8080/blogs/1/posts/1 \
  -H "Authorization: Bearer user:alice:user" \
  -H "Content-Type: application/json" \
  -d '{"title":"My First Post","content":"Updated content","editor_id":"charlie"}'

# Now Charlie can access the post (he's the editor)
curl http://localhost:8080/blogs/1/posts/1 \
  -H "Authorization: Bearer user:charlie:user"
# Response: {"id":1,"author_id":"alice","editor_id":"charlie",...}

# Admin can access any post
curl http://localhost:8080/blogs/1/posts/1 \
  -H "Authorization: Bearer user:bob:user,admin"
# Response: {"id":1,...}
```

### 5. Comment - MethodAll Override Pattern

Nested under posts. Default is auth-required for all methods, but GET is overridden to be public.

**Auth Pattern:** MethodAll override (default auth-only, GET public)

**Routes:**
- `GET /blogs/{blogId}/posts/{postId}/comments` - Public
- `POST /blogs/{blogId}/posts/{postId}/comments` - Requires authentication
- `GET /blogs/{blogId}/posts/{postId}/comments/{id}` - Public
- `PUT /blogs/{blogId}/posts/{postId}/comments/{id}` - Requires authentication
- `DELETE /blogs/{blogId}/posts/{postId}/comments/{id}` - Requires authentication

**Examples:**

```bash
# Anyone can read comments (no auth)
curl http://localhost:8080/blogs/1/posts/1/comments

# Anyone can read a specific comment
curl http://localhost:8080/blogs/1/posts/1/comments/1

# Create comment (requires auth, any scope)
curl -X POST http://localhost:8080/blogs/1/posts/1/comments \
  -H "Authorization: Bearer user:alice:user" \
  -H "Content-Type: application/json" \
  -d '{"author_name":"Alice","text":"Great post!"}'

# Without auth - fails
curl -X POST http://localhost:8080/blogs/1/posts/1/comments \
  -H "Content-Type: application/json" \
  -d '{"author_name":"Anonymous","text":"Comment"}'
# Response: unauthorized (401)

# Update comment (requires auth)
curl -X PUT http://localhost:8080/blogs/1/posts/1/comments/1 \
  -H "Authorization: Bearer user:bob:user" \
  -H "Content-Type: application/json" \
  -d '{"author_name":"Bob","text":"Updated comment"}'
```

### 6. ModeratorAction - Specific Scope Requirement

Requires `moderator` scope for all operations.

**Routes:**
- `GET /moderator-actions` - Requires `moderator` scope
- `POST /moderator-actions` - Requires `moderator` scope
- `GET /moderator-actions/{id}` - Requires `moderator` scope
- `PUT /moderator-actions/{id}` - Requires `moderator` scope
- `DELETE /moderator-actions/{id}` - Requires `moderator` scope

**Examples:**

```bash
# Create moderator action (requires moderator scope)
curl -X POST http://localhost:8080/moderator-actions \
  -H "Authorization: Bearer user:diana:user,moderator" \
  -H "Content-Type: application/json" \
  -d '{"action":"delete","target_type":"comment","target_id":1,"reason":"Spam"}'

# Without moderator scope - fails
curl -X POST http://localhost:8080/moderator-actions \
  -H "Authorization: Bearer user:alice:user" \
  -H "Content-Type: application/json" \
  -d '{"action":"delete","target_type":"comment","target_id":1,"reason":"Spam"}'
# Response: forbidden (403)

# Admin scope doesn't work (needs moderator specifically)
curl -X POST http://localhost:8080/moderator-actions \
  -H "Authorization: Bearer user:bob:user,admin" \
  -H "Content-Type: application/json" \
  -d '{"action":"delete","target_type":"comment","target_id":1,"reason":"Spam"}'
# Response: forbidden (403)
```

## Complete Workflow Example

```bash
# 1. Create authors (as admin)
curl -X POST http://localhost:8080/authors \
  -H "Authorization: Bearer user:bob:user,admin" \
  -H "Content-Type: application/json" \
  -d '{"name":"Alice Smith","bio":"Tech enthusiast"}'

curl -X POST http://localhost:8080/authors \
  -H "Authorization: Bearer user:bob:user,admin" \
  -H "Content-Type: application/json" \
  -d '{"name":"Charlie Brown","bio":"Professional writer"}'

# 2. Publish articles (as publisher)
curl -X POST http://localhost:8080/articles \
  -H "Authorization: Bearer user:charlie:user,publisher" \
  -H "Content-Type: application/json" \
  -d '{"title":"Getting Started with Go","content":"Go is a statically typed language..."}'

# 3. Alice creates her blog
curl -X POST http://localhost:8080/blogs \
  -H "Authorization: Bearer user:alice:user" \
  -H "Content-Type: application/json" \
  -d '{"name":"Tech Insights","description":"My thoughts on technology"}'

# 4. Alice creates a post on her blog
curl -X POST http://localhost:8080/blogs/1/posts \
  -H "Authorization: Bearer user:alice:user" \
  -H "Content-Type: application/json" \
  -d '{"title":"My First Blog Post","content":"Welcome to my blog!"}'

# 5. Alice adds Charlie as editor
curl -X PUT http://localhost:8080/blogs/1/posts/1 \
  -H "Authorization: Bearer user:alice:user" \
  -H "Content-Type: application/json" \
  -d '{"title":"My First Blog Post","content":"Welcome to my blog! (Edited)","editor_id":"charlie"}'

# 6. Anyone can comment (with auth)
curl -X POST http://localhost:8080/blogs/1/posts/1/comments \
  -H "Authorization: Bearer user:diana:user,moderator" \
  -H "Content-Type: application/json" \
  -d '{"author_name":"Diana","text":"Great start!"}'

# 7. Anyone can read comments (no auth)
curl http://localhost:8080/blogs/1/posts/1/comments

# 8. Moderator takes action on spam comment
curl -X POST http://localhost:8080/moderator-actions \
  -H "Authorization: Bearer user:diana:user,moderator" \
  -H "Content-Type: application/json" \
  -d '{"action":"flag","target_type":"comment","target_id":1,"reason":"Review needed"}'
```

## Auth Pattern Summary

| Resource | Pattern | Description |
|----------|---------|-------------|
| Article | Scope-based | Public reads, `publisher` scope for writes |
| Author | Admin-only writes | Public reads, `admin` scope for writes |
| Blog | Ownership | User owns their blogs, admin bypass |
| Post | Multiple ownership | `author_id` OR `editor_id`, admin bypass |
| Comment | MethodAll override | Default auth-only, GET overridden to public |
| ModeratorAction | Specific scope | Requires `moderator` scope |

## Code Highlights

### Simple Auth Middleware

```go
func authMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Parse Authorization header
        // Create AuthInfo with UserID and Scopes
        authInfo := &router.AuthInfo{
            UserID: userID,
            Scopes: scopes,
        }
        ctx := context.WithValue(r.Context(), router.AuthInfoKey, authInfo)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}
```

### Using Convenience Helpers

```go
// Public route
router.RegisterRoutes[Article](b, "/articles", router.AllPublic())

// Auth-only route
router.RegisterRoutes[Profile](b, "/profiles", router.IsAuthenticated())

// Scope-based route
router.RegisterRoutes[Author](b, "/authors", router.AllScoped("admin"))

// Ownership route
router.RegisterRoutes[Blog](b, "/blogs",
    router.AllWithOwnershipUnless([]string{"AuthorID"}, "admin"))

// Mixed patterns
router.RegisterRoutes[Article](b, "/articles",
    router.PublicReadOnly(),
    router.AuthConfig{
        Methods: []string{router.MethodPost, router.MethodPut, router.MethodDelete},
        Scopes:  []string{"publisher"},
    },
)
```

## Security Features

1. **Secure by Default** - Routes without auth config are blocked (401)
2. **Path IDs Take Precedence** - IDs and foreign keys from JSON body are ignored
3. **Ownership Filtering** - Queries automatically filtered by authenticated user
4. **Admin Bypass** - Configurable bypass scopes for privileged users
5. **404 Not 403** - Returns 404 when accessing other user's resources (doesn't leak existence)
6. **Multiple Ownership** - OR logic for resources with multiple owner fields

## Production Usage

In production, replace the fake token middleware with real authentication:

```go
// Example with Auth0
func auth0Middleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Validate JWT token
        token := extractAndValidateJWT(r)

        // Extract user info and scopes
        authInfo := &router.AuthInfo{
            UserID: token.Subject,  // Auth0 user ID
            Scopes: token.Scopes,   // Permissions from token
        }

        ctx := context.WithValue(r.Context(), router.AuthInfoKey, authInfo)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}
```
