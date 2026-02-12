# Nested Routes Example

This example demonstrates how to create nested REST APIs with automatic parent validation using go-restgen.

## Overview

The Builder API allows you to register nested resources that automatically:
- Generate unique URL parameters for each resource type
- Validate parent relationships in queries
- Use JOINs to ensure data integrity across the parent chain

## Models

Define your models with standard bun relationship tags:

```go
type User struct {
    bun.BaseModel `bun:"table:users"`
    ID            int       `bun:"id,pk,autoincrement" json:"id"`
    Name          string    `bun:"name,notnull" json:"name"`
    Email         string    `bun:"email,unique,notnull" json:"email"`
    CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
    UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
    Posts         []*Post   `bun:"rel:has-many,join:id=user_id" json:"-"`  // Excluded from JSON
}

type Post struct {
    bun.BaseModel `bun:"table:posts"`
    ID            int        `bun:"id,pk,autoincrement" json:"id"`
    UserID        int        `bun:"user_id,notnull,skipupdate" json:"user_id"`  // Foreign key
    User          *User      `bun:"rel:belongs-to,join:user_id=id" json:"-"`    // Parent relation (excluded)
    Title         string     `bun:"title,notnull" json:"title"`
    Content       string     `bun:"content" json:"content"`
    CreatedAt     time.Time  `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
    UpdatedAt     time.Time  `bun:"updated_at,notnull" json:"updated_at,omitempty"`
    Comments      []*Comment `bun:"rel:has-many,join:id=post_id" json:"-"`     // Excluded from JSON
}

type Comment struct {
    bun.BaseModel `bun:"table:comments"`
    ID            int       `bun:"id,pk,autoincrement" json:"id"`
    PostID        int       `bun:"post_id,notnull,skipupdate" json:"post_id"`  // Foreign key
    Post          *Post     `bun:"rel:belongs-to,join:post_id=id" json:"-"`    // Parent relation (excluded)
    Text          string    `bun:"text,notnull" json:"text"`
    Author        string    `bun:"author,notnull" json:"author"`
    CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
    UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
}
```

**Key Model Features:**
- **Foreign keys** (`user_id`, `post_id`) use `skipupdate` to prevent modification after creation
- **Relation fields** (`User`, `Post`, `Posts`, `Comments`) use `json:"-"` to exclude from API responses (RESTful)
- **Timestamps** use `skipupdate` on `CreatedAt`, and both are managed via `BeforeAppendModel` hooks

## Registration

Use the Builder API to register nested routes with authentication:

```go
b := router.NewBuilder(r, db.GetDB())
router.RegisterRoutes[User](b, "/users", router.AuthConfig{
    Methods: []string{router.MethodAll},
    Scopes:  []string{router.ScopePublic},  // Public for this example
}, func(b *router.Builder) {
    router.RegisterRoutes[Post](b, "/posts", router.AuthConfig{
        Methods: []string{router.MethodAll},
        Scopes:  []string{router.ScopePublic},
    }, func(b *router.Builder) {
        router.RegisterRoutes[Comment](b, "/comments", router.AuthConfig{
            Methods: []string{router.MethodAll},
            Scopes:  []string{router.ScopePublic},
        })
    })
})
```

**Note**: This example uses public routes for simplicity. In production, you would typically require authentication and/or use ownership to scope resources to authenticated users.

## Generated Routes

### Users
- `GET /users` - List all users
- `POST /users` - Create user
- `GET /users/{userId}` - Get user
- `PUT /users/{userId}` - Update user
- `DELETE /users/{userId}` - Delete user

### Posts (nested under users)
- `GET /users/{userId}/posts` - List posts **for this user only**
- `POST /users/{userId}/posts` - Create post for user
- `GET /users/{userId}/posts/{postId}` - Get post (validates belongs to user)
- `PUT /users/{userId}/posts/{postId}` - Update post
- `DELETE /users/{userId}/posts/{postId}` - Delete post

### Comments (nested under posts)
- `GET /users/{userId}/posts/{postId}/comments` - List comments **for this post**
- `POST /users/{userId}/posts/{postId}/comments` - Create comment
- `GET /users/{userId}/posts/{postId}/comments/{commentId}` - Get comment
- `PUT /users/{userId}/posts/{postId}/comments/{commentId}` - Update comment
- `DELETE /users/{userId}/posts/{postId}/comments/{commentId}` - Delete comment

## Parent Validation

All nested routes automatically validate the parent chain:

```
GET /users/1/posts/5
→ Returns 404 if post 5 doesn't belong to user 1

GET /users/1/posts/2/comments/10
→ Returns 404 if:
  - Comment 10 doesn't belong to post 2, OR
  - Post 2 doesn't belong to user 1
```

This is done via SQL JOINs at the database level:

```sql
-- For GET /users/1/posts/2/comments/10
SELECT comments.* FROM comments
JOIN posts ON comments.post_id = posts.id
JOIN users ON posts.user_id = users.id
WHERE comments.id = 10
  AND comments.post_id = 2
  AND users.id = 1
```

## How It Works

1. **Registration Time**: The parent type is known from the nesting context, and the bun `rel:belongs-to` tag is read to identify the foreign key
2. **Metadata Storage**: A registry stores the parent chain for each type
3. **Middleware**: Each nested level adds middleware to extract parent IDs from the URL
4. **Query Building**: The datastore wrapper automatically adds JOINs based on metadata
5. **Validation**: Parent IDs are validated via WHERE clauses on the JOINs

## Requirements

- Child models must have a parent relation field (e.g., `User *User`) with `bun:"rel:belongs-to,join:..."` tag
- Foreign key fields should use `skipupdate` to prevent modification after creation
- Relation fields should use `json:"-"` to exclude from API responses (RESTful best practice)
- Models must have `bun.BaseModel` with table name
- Timestamps should use `skipupdate` on `CreatedAt` and be managed via `BeforeAppendModel` hook

## Security Features

The framework automatically protects against ID injection attacks:

- **Foreign keys from URL path**: When creating nested resources, foreign keys are set from the URL path, not the JSON body
- **IDs from URL path**: When updating resources, the ID from the URL path takes precedence over any ID in the JSON body
- **Parent chain validation**: All operations validate the complete parent chain at the database level using JOINs

Example - these malicious requests are safely handled:
```bash
# Try to create post for wrong user - user_id ignored, uses path
curl -X POST http://localhost:8080/users/1/posts \
  -d '{"user_id":999,"title":"Attack"}'
# Result: Creates post with user_id=1 (from path)

# Try to update with wrong ID - ID ignored, uses path
curl -X PUT http://localhost:8080/users/1/posts/5 \
  -d '{"id":999,"title":"Attack"}'
# Result: Updates post 5 (from path), not post 999
```

## Running the Example

```bash
cd examples/nested_routes
go run main.go
```

## Testing

This example includes 16 comprehensive Bruno API tests that verify nested resources with parent validation. See [`../../bruno/README.md`](../../bruno/README.md) for instructions on running the tests.

## Manual Testing

Try these curl commands:

```bash
# Create a user
curl -X POST http://localhost:8080/users \
  -H "Content-Type: application/json" \
  -d '{"name": "Alice", "email": "alice@example.com"}'

# Create a post for that user
curl -X POST http://localhost:8080/users/1/posts \
  -H "Content-Type: application/json" \
  -d '{"user_id": 1, "title": "My First Post", "content": "Hello!"}'

# Create a comment on the post
curl -X POST http://localhost:8080/users/1/posts/1/comments \
  -H "Content-Type: application/json" \
  -d '{"post_id": 1, "text": "Great post!", "author": "Bob"}'

# Get all comments for the post
curl http://localhost:8080/users/1/posts/1/comments

# Try to access post with wrong user (will return 404)
curl http://localhost:8080/users/999/posts/1
```
