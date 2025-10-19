//nolint:gosec,gocritic,unparam // Example code - simplified for demonstration
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/router"
	"github.com/uptrace/bun"
)

// User model
type User struct {
	bun.BaseModel `bun:"table:users"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	Name          string    `bun:"name,notnull" json:"name"`
	Email         string    `bun:"email,unique,notnull" json:"email"`
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
	Posts         []*Post   `bun:"rel:has-many,join:id=user_id" json:"-"`
}

// BeforeAppendModel hook for User timestamps
func (u *User) BeforeAppendModel(ctx context.Context, query bun.Query) error {
	now := time.Now()
	switch query.(type) {
	case *bun.InsertQuery:
		u.CreatedAt = now
		u.UpdatedAt = now
	case *bun.UpdateQuery:
		u.UpdatedAt = now
	}
	return nil
}

// Post model - belongs to User
type Post struct {
	bun.BaseModel `bun:"table:posts"`
	ID            int        `bun:"id,pk,autoincrement" json:"id"`
	UserID        int        `bun:"user_id,notnull,skipupdate" json:"user_id"`
	User          *User      `bun:"rel:belongs-to,join:user_id=id" json:"-"`
	Title         string     `bun:"title,notnull" json:"title"`
	Content       string     `bun:"content" json:"content"`
	CreatedAt     time.Time  `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	UpdatedAt     time.Time  `bun:"updated_at,notnull" json:"updated_at,omitempty"`
	Comments      []*Comment `bun:"rel:has-many,join:id=post_id" json:"-"`
}

// BeforeAppendModel hook for Post timestamps
func (p *Post) BeforeAppendModel(ctx context.Context, query bun.Query) error {
	now := time.Now()
	switch query.(type) {
	case *bun.InsertQuery:
		p.CreatedAt = now
		p.UpdatedAt = now
	case *bun.UpdateQuery:
		p.UpdatedAt = now
	}
	return nil
}

// Comment model - belongs to Post
type Comment struct {
	bun.BaseModel `bun:"table:comments"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	PostID        int       `bun:"post_id,notnull,skipupdate" json:"post_id"`
	Post          *Post     `bun:"rel:belongs-to,join:post_id=id" json:"-"`
	Text          string    `bun:"text,notnull" json:"text"`
	Author        string    `bun:"author,notnull" json:"author"`
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
}

// BeforeAppendModel hook for Comment timestamps
func (c *Comment) BeforeAppendModel(ctx context.Context, query bun.Query) error {
	now := time.Now()
	switch query.(type) {
	case *bun.InsertQuery:
		c.CreatedAt = now
		c.UpdatedAt = now
	case *bun.UpdateQuery:
		c.UpdatedAt = now
	}
	return nil
}

func main() {
	// Configure logging to show warnings in development
	// In production, set level to Error to suppress warnings
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))
	slog.SetDefault(logger)

	// Create SQLite in-memory database
	db, err := datastore.NewSQLite(":memory:")
	if err != nil {
		log.Fatal("Failed to create datastore:", err)
	}

	// Initialize the global datastore
	if err := datastore.Initialize(db); err != nil {
		log.Fatal("Failed to initialize datastore:", err)
	}
	defer datastore.Cleanup()

	// Create schema
	ctx := context.Background()
	if _, err := db.GetDB().NewCreateTable().Model((*User)(nil)).IfNotExists().Exec(ctx); err != nil {
		log.Fatal("Failed to create users table:", err)
	}
	if _, err := db.GetDB().NewCreateTable().Model((*Post)(nil)).IfNotExists().Exec(ctx); err != nil {
		log.Fatal("Failed to create posts table:", err)
	}
	if _, err := db.GetDB().NewCreateTable().Model((*Comment)(nil)).IfNotExists().Exec(ctx); err != nil {
		log.Fatal("Failed to create comments table:", err)
	}

	// Create router
	r := chi.NewRouter()

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Register nested routes using the Builder API
	b := router.NewBuilder(r)
	router.RegisterRoutes[User](b, "/users", router.AllPublic(), func(b *router.Builder) {
		router.RegisterRoutes[Post](b, "/posts", router.AllPublic(), func(b *router.Builder) {
			router.RegisterRoutes[Comment](b, "/comments", router.AllPublic())
		})
	})

	// This automatically creates the following routes:
	//
	// Users:
	//   GET    /users           - List all users
	//   POST   /users           - Create new user
	//   GET    /users/{userId}  - Get specific user
	//   PUT    /users/{userId}  - Update user
	//   DELETE /users/{userId}  - Delete user
	//
	// Posts (nested under users):
	//   GET    /users/{userId}/posts           - List posts for user
	//   POST   /users/{userId}/posts           - Create post for user
	//   GET    /users/{userId}/posts/{postId}  - Get specific post (validates belongs to user)
	//   PUT    /users/{userId}/posts/{postId}  - Update post
	//   DELETE /users/{userId}/posts/{postId}  - Delete post
	//
	// Comments (nested under posts):
	//   GET    /users/{userId}/posts/{postId}/comments              - List comments for post
	//   POST   /users/{userId}/posts/{postId}/comments              - Create comment for post
	//   GET    /users/{userId}/posts/{postId}/comments/{commentId}  - Get comment (validates full chain)
	//   PUT    /users/{userId}/posts/{postId}/comments/{commentId}  - Update comment
	//   DELETE /users/{userId}/posts/{postId}/comments/{commentId}  - Delete comment
	//
	// Parent validation is automatic:
	//   - GET /users/1/posts/5 will return 404 if post 5 doesn't belong to user 1
	//   - GET /users/1/posts/2/comments/10 will return 404 if comment 10 doesn't belong to post 2,
	//     or if post 2 doesn't belong to user 1

	// Start server
	fmt.Println("Server starting on :8080")
	fmt.Println("Using SQLite in-memory database")
	fmt.Println("\nExample requests:")
	fmt.Println("  POST /users")
	fmt.Println("       {\"name\": \"Alice\", \"email\": \"alice@example.com\"}")
	fmt.Println("  POST /users/1/posts")
	fmt.Println("       {\"title\": \"My First Post\", \"content\": \"Hello world\"}")
	fmt.Println("  POST /users/1/posts/1/comments")
	fmt.Println("       {\"text\": \"Great post\", \"author\": \"Bob\"}")
	fmt.Println("  GET /users/1/posts/1/comments")
	log.Fatal(http.ListenAndServe(":8080", r))
}
