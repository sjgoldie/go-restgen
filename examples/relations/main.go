//nolint:gosec,gocritic,unparam,staticcheck // Example code - simplified for demonstration
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/router"
	"github.com/uptrace/bun"
)

// Author model - top level resource
type Author struct {
	bun.BaseModel `bun:"table:authors"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	ExternalID    string    `bun:"external_id,unique,notnull" json:"external_id"` // Maps to auth UserID
	Name          string    `bun:"name,notnull" json:"name"`
	Email         string    `bun:"email,unique,notnull" json:"email"`
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
	// Relations - loaded via ?include=Posts
	Posts []*Post `bun:"rel:has-many,join:id=author_id" json:"posts,omitempty"`
}

func (a *Author) BeforeAppendModel(ctx context.Context, query bun.Query) error {
	now := time.Now()
	switch query.(type) {
	case *bun.InsertQuery:
		a.CreatedAt = now
		a.UpdatedAt = now
	case *bun.UpdateQuery:
		a.UpdatedAt = now
	}
	return nil
}

// Post model - belongs to Author, has many Comments
type Post struct {
	bun.BaseModel `bun:"table:posts"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	AuthorID      int       `bun:"author_id,notnull,skipupdate" json:"author_id"`
	Author        *Author   `bun:"rel:belongs-to,join:author_id=id" json:"-"`
	OwnerID       string    `bun:"owner_id,notnull" json:"owner_id"` // For ownership filtering
	Title         string    `bun:"title,notnull" json:"title"`
	Content       string    `bun:"content" json:"content"`
	Published     bool      `bun:"published,default:false" json:"published"`
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
	// Relations - loaded via ?include=Comments
	Comments []*Comment `bun:"rel:has-many,join:id=post_id" json:"comments,omitempty"`
}

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
	OwnerID       string    `bun:"owner_id,notnull" json:"owner_id"` // For ownership filtering
	Text          string    `bun:"text,notnull" json:"text"`
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
}

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

// Simple auth middleware that parses fake bearer tokens
// Token format: user:<userID>:<scope1>,<scope2>,...
// Example: "user:alice:user" or "user:bob:user,admin"
func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			next.ServeHTTP(w, r)
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			http.Error(w, "invalid authorization header format", http.StatusUnauthorized)
			return
		}

		token := parts[1]
		tokenParts := strings.SplitN(token, ":", 3)
		if len(tokenParts) != 3 || tokenParts[0] != "user" {
			http.Error(w, "invalid token format", http.StatusUnauthorized)
			return
		}

		userID := tokenParts[1]
		scopesPart := tokenParts[2]

		var scopes []string
		if scopesPart != "" {
			scopes = strings.Split(scopesPart, ",")
		}

		authInfo := &router.AuthInfo{
			UserID: userID,
			Scopes: scopes,
		}

		ctx := context.WithValue(r.Context(), router.AuthInfoKey, authInfo)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))
	slog.SetDefault(logger)

	db, err := datastore.NewSQLite(":memory:")
	if err != nil {
		log.Fatal("Failed to create datastore:", err)
	}

	if err := datastore.Initialize(db); err != nil {
		log.Fatal("Failed to initialize datastore:", err)
	}
	defer datastore.Cleanup()

	ctx := context.Background()
	models := []interface{}{
		(*Author)(nil),
		(*Post)(nil),
		(*Comment)(nil),
	}

	for _, model := range models {
		if _, err := db.GetDB().NewCreateTable().Model(model).IfNotExists().Exec(ctx); err != nil {
			log.Fatal("Failed to create table:", err)
		}
	}

	r := chi.NewRouter()
	r.Use(authMiddleware)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	b := router.NewBuilder(r)

	// Authors - public access, with Posts relation available via ?include=Posts
	router.RegisterRoutes[Author](b, "/authors",
		router.AllPublic(),
		func(b *router.Builder) {
			// Posts - ownership-based with admin bypass
			// WithRelationName("Posts") enables ?include=Posts on parent
			router.RegisterRoutes[Post](b, "/posts",
				router.AllWithOwnershipUnless([]string{"OwnerID"}, "admin"),
				router.WithRelationName("Posts"),
				func(b *router.Builder) {
					// Comments - ownership-based with admin bypass
					// WithRelationName("Comments") enables ?include=Comments on parent
					router.RegisterRoutes[Comment](b, "/comments",
						router.AllWithOwnershipUnless([]string{"OwnerID"}, "admin"),
						router.WithRelationName("Comments"),
					)
				},
			)
		},
	)

	fmt.Println("Relations Example Server starting on :8080")
	fmt.Println("Using SQLite in-memory database")
	fmt.Println("\n=== Authentication ===")
	fmt.Println("Use fake bearer tokens with format: user:<userID>:<scope1>,<scope2>,...")
	fmt.Println("\nExample tokens:")
	fmt.Println("  - Alice (regular user):    Bearer user:alice:user")
	fmt.Println("  - Bob (regular user):      Bearer user:bob:user")
	fmt.Println("  - Admin:                   Bearer user:admin:user,admin")
	fmt.Println("\n=== Endpoints ===")
	fmt.Println("\nAuthors (public):")
	fmt.Println("  GET    /authors                     - List all authors")
	fmt.Println("  GET    /authors/{id}                - Get author")
	fmt.Println("  GET    /authors/{id}?include=Posts  - Get author with their posts")
	fmt.Println("  POST   /authors                     - Create author")
	fmt.Println("\nPosts (nested under authors, ownership-based):")
	fmt.Println("  GET    /authors/{id}/posts                       - List posts (filtered by ownership)")
	fmt.Println("  GET    /authors/{id}/posts/{id}                  - Get post")
	fmt.Println("  GET    /authors/{id}/posts/{id}?include=Comments - Get post with comments")
	fmt.Println("  POST   /authors/{id}/posts                       - Create post (OwnerID auto-set)")
	fmt.Println("\nComments (nested under posts, ownership-based):")
	fmt.Println("  GET    /authors/{id}/posts/{id}/comments      - List comments (filtered by ownership)")
	fmt.Println("  POST   /authors/{id}/posts/{id}/comments      - Create comment (OwnerID auto-set)")
	fmt.Println("\n=== Relation Include Behavior ===")
	fmt.Println("\n1. ?include= only works for relations registered with WithRelationName()")
	fmt.Println("2. Unknown relation names are silently ignored")
	fmt.Println("3. Ownership filtering is applied to included relations")
	fmt.Println("   - Alice requesting ?include=Posts only sees Alice's posts")
	fmt.Println("   - Admin requesting ?include=Posts sees all posts")
	fmt.Println("4. Multiple relations: ?include=Posts,Comments (if parent has both)")

	log.Fatal(http.ListenAndServe(":8080", r))
}
