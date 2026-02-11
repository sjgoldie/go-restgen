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
	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/router"
)

// Article model - demonstrates public reads with scoped writes
type Article struct {
	bun.BaseModel `bun:"table:articles"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	Title         string    `bun:"title,notnull" json:"title"`
	Content       string    `bun:"content" json:"content"`
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
}

func (a *Article) BeforeAppendModel(ctx context.Context, query bun.Query) error {
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

// Author model - demonstrates public reads, admin-only writes
type Author struct {
	bun.BaseModel `bun:"table:authors"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	Name          string    `bun:"name,notnull" json:"name"`
	Bio           string    `bun:"bio" json:"bio"`
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
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

// Blog model - demonstrates ownership with admin bypass, plus filtering and sorting
type Blog struct {
	bun.BaseModel `bun:"table:blogs"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	AuthorID      string    `bun:"author_id,notnull" json:"author_id"` // External user ID
	Name          string    `bun:"name,notnull" json:"name"`
	Description   string    `bun:"description" json:"description"`
	Status        string    `bun:"status,notnull,default:'draft'" json:"status"` // draft, published, archived
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
	Posts         []*Post   `bun:"rel:has-many,join:id=blog_id" json:"posts,omitempty"`
}

func (b *Blog) BeforeAppendModel(ctx context.Context, query bun.Query) error {
	now := time.Now()
	switch query.(type) {
	case *bun.InsertQuery:
		b.CreatedAt = now
		b.UpdatedAt = now
	case *bun.UpdateQuery:
		b.UpdatedAt = now
	}
	return nil
}

// Post model - demonstrates multiple ownership fields (author OR editor)
type Post struct {
	bun.BaseModel `bun:"table:posts"`
	ID            int        `bun:"id,pk,autoincrement" json:"id"`
	BlogID        int        `bun:"blog_id,notnull,skipupdate" json:"blog_id"`
	Blog          *Blog      `bun:"rel:belongs-to,join:blog_id=id" json:"blog,omitempty"`
	AuthorID      string     `bun:"author_id,notnull" json:"author_id"` // Owner field 1
	EditorID      string     `bun:"editor_id" json:"editor_id"`         // Owner field 2 (optional)
	Title         string     `bun:"title,notnull" json:"title"`
	Content       string     `bun:"content" json:"content"`
	CreatedAt     time.Time  `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	UpdatedAt     time.Time  `bun:"updated_at,notnull" json:"updated_at,omitempty"`
	Comments      []*Comment `bun:"rel:has-many,join:id=post_id" json:"comments,omitempty"`
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

// Comment model - demonstrates MethodAll override pattern
type Comment struct {
	bun.BaseModel `bun:"table:comments"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	PostID        int       `bun:"post_id,notnull,skipupdate" json:"post_id"`
	Post          *Post     `bun:"rel:belongs-to,join:post_id=id" json:"post,omitempty"`
	AuthorName    string    `bun:"author_name,notnull" json:"author_name"`
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

// ModeratorAction model - demonstrates specific scope requirement
type ModeratorAction struct {
	bun.BaseModel `bun:"table:moderator_actions"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	ModeratorID   string    `bun:"moderator_id,notnull" json:"moderator_id"`
	Action        string    `bun:"action,notnull" json:"action"`
	TargetType    string    `bun:"target_type,notnull" json:"target_type"`
	TargetID      int       `bun:"target_id,notnull" json:"target_id"`
	Reason        string    `bun:"reason" json:"reason"`
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
}

func (m *ModeratorAction) BeforeAppendModel(ctx context.Context, query bun.Query) error {
	now := time.Now()
	switch query.(type) {
	case *bun.InsertQuery:
		m.CreatedAt = now
	}
	return nil
}

// Report model - demonstrates MethodList vs MethodGet differentiation
// Use case: Can't browse all reports, but can access individual report via shareable link
type Report struct {
	bun.BaseModel `bun:"table:reports"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	Title         string    `bun:"title,notnull" json:"title"`
	Content       string    `bun:"content" json:"content"`
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
}

func (r *Report) BeforeAppendModel(ctx context.Context, query bun.Query) error {
	now := time.Now()
	switch query.(type) {
	case *bun.InsertQuery:
		r.CreatedAt = now
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
			// No auth header - continue without auth info
			next.ServeHTTP(w, r)
			return
		}

		// Parse "Bearer <token>"
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			http.Error(w, "invalid authorization header format", http.StatusUnauthorized)
			return
		}

		token := parts[1]

		// Parse token format: user:<userID>:<scopes>
		tokenParts := strings.SplitN(token, ":", 3)
		if len(tokenParts) != 3 || tokenParts[0] != "user" {
			http.Error(w, "invalid token format", http.StatusUnauthorized)
			return
		}

		userID := tokenParts[1]
		scopesPart := tokenParts[2]

		// Parse scopes (comma-separated)
		var scopes []string
		if scopesPart != "" {
			scopes = strings.Split(scopesPart, ",")
		}

		// Create AuthInfo and add to context
		authInfo := &router.AuthInfo{
			UserID: userID,
			Scopes: scopes,
		}

		ctx := context.WithValue(r.Context(), router.AuthInfoKey, authInfo)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func main() {
	// Configure logging
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
	models := []interface{}{
		(*Article)(nil),
		(*Author)(nil),
		(*Blog)(nil),
		(*Post)(nil),
		(*Comment)(nil),
		(*ModeratorAction)(nil),
		(*Report)(nil),
	}

	for _, model := range models {
		if _, err := db.GetDB().NewCreateTable().Model(model).IfNotExists().Exec(ctx); err != nil {
			log.Fatal("Failed to create table:", err)
		}
	}

	// Create router with auth middleware
	r := chi.NewRouter()
	r.Use(authMiddleware)

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	b := router.NewBuilder(r)

	// Article - public reads, requires "publisher" scope for writes
	router.RegisterRoutes[Article](b, "/articles",
		router.PublicReadOnly(),
		router.AuthConfig{
			Methods: []string{router.MethodPost, router.MethodPut, router.MethodDelete},
			Scopes:  []string{"publisher"},
		},
	)

	// Author - public reads, admin scope for writes
	router.RegisterRoutes[Author](b, "/authors",
		router.PublicReadOnly(),
		router.AllScoped("admin"),
	)

	// Blog → Post → Comment (nested with different auth at each level)
	// Blog demonstrates ownership + filtering + sorting combined
	router.RegisterRoutes[Blog](b, "/blogs",
		router.AllWithOwnershipUnless([]string{"AuthorID"}, "admin"),
		router.WithFilters("Status", "Name"),
		router.WithSorts("Name", "Status", "CreatedAt"),
		router.WithDefaultSort("-CreatedAt"),
		func(b *router.Builder) {
			// Post - multiple ownership fields (AuthorID OR EditorID), admin bypass
			router.RegisterRoutes[Post](b, "/posts",
				router.AllWithOwnershipUnless([]string{"AuthorID", "EditorID"}, "admin"),
				router.WithRelationName("Posts"),
				func(b *router.Builder) {
					// Comment - MethodAll override: default auth-only, but GET is public
					router.RegisterRoutes[Comment](b, "/comments",
						router.IsAuthenticated(), // Default: all methods require auth
						router.PublicReadOnly(),  // Override: GET is public
						router.WithRelationName("Comments"),
					)
				},
			)
		},
	)

	// ModeratorAction - requires "moderator" scope
	router.RegisterRoutes[ModeratorAction](b, "/moderator-actions",
		router.AllScoped("moderator"),
	)

	// Report - demonstrates MethodList vs MethodGet differentiation
	// List requires auth (can't browse all reports)
	// Get is public (shareable links work without auth)
	// Create/Update/Delete require auth
	router.RegisterRoutes[Report](b, "/reports",
		router.IsAuthenticated(), // Default: all methods require auth
		router.PublicGet(),       // Override: GET single item is public
	)

	// Print usage information
	fmt.Println("Auth Example Server starting on :8080")
	fmt.Println("Using SQLite in-memory database")
	fmt.Println("\n=== Authentication ===")
	fmt.Println("Use fake bearer tokens with format: user:<userID>:<scope1>,<scope2>,...")
	fmt.Println("\nExample tokens:")
	fmt.Println("  - Alice (regular user):    Bearer user:alice:user")
	fmt.Println("  - Bob (admin):             Bearer user:bob:user,admin")
	fmt.Println("  - Charlie (publisher):     Bearer user:charlie:user,publisher")
	fmt.Println("  - Diana (moderator):       Bearer user:diana:user,moderator")
	fmt.Println("\n=== Resources & Auth Patterns ===")
	fmt.Println("\n1. Articles - Public reads, publisher scope for writes")
	fmt.Println("   GET    /articles           (public)")
	fmt.Println("   POST   /articles           (requires 'publisher' scope)")
	fmt.Println("   PUT    /articles/{id}      (requires 'publisher' scope)")
	fmt.Println("   DELETE /articles/{id}      (requires 'publisher' scope)")
	fmt.Println("\n2. Authors - Public reads, admin-only writes")
	fmt.Println("   GET    /authors            (public)")
	fmt.Println("   POST   /authors            (requires 'admin' scope)")
	fmt.Println("   PUT    /authors/{id}       (requires 'admin' scope)")
	fmt.Println("   DELETE /authors/{id}       (requires 'admin' scope)")
	fmt.Println("\n3. Blogs - Ownership-based (author owns blog), admin bypass, with filtering/sorting")
	fmt.Println("   GET    /blogs              (shows only user's blogs, admin sees all)")
	fmt.Println("   GET    /blogs?filter[Status]=published  (filter by status)")
	fmt.Println("   GET    /blogs?sort=Name    (sort by name)")
	fmt.Println("   POST   /blogs              (creates blog owned by user)")
	fmt.Println("   PUT    /blogs/{id}         (owner or admin only)")
	fmt.Println("   DELETE /blogs/{id}         (owner or admin only)")
	fmt.Println("\n4. Posts - Multiple ownership (author OR editor), admin bypass")
	fmt.Println("   POST   /blogs/{blogId}/posts        (owned by author)")
	fmt.Println("   GET    /blogs/{blogId}/posts/{id}   (author, editor, or admin)")
	fmt.Println("   PUT    /blogs/{blogId}/posts/{id}   (author, editor, or admin)")
	fmt.Println("\n5. Comments - MethodAll override (default auth, GET public)")
	fmt.Println("   GET    /blogs/{blogId}/posts/{postId}/comments     (public)")
	fmt.Println("   POST   /blogs/{blogId}/posts/{postId}/comments     (requires auth)")
	fmt.Println("   PUT    /blogs/{blogId}/posts/{postId}/comments/{id} (requires auth)")
	fmt.Println("\n6. Moderator Actions - Specific scope requirement")
	fmt.Println("   GET    /moderator-actions  (requires 'moderator' scope)")
	fmt.Println("   POST   /moderator-actions  (requires 'moderator' scope)")
	fmt.Println("\n7. Reports - MethodList vs MethodGet differentiation")
	fmt.Println("   GET    /reports            (requires auth - can't browse)")
	fmt.Println("   GET    /reports/{id}       (public - shareable links)")
	fmt.Println("   POST   /reports            (requires auth)")
	fmt.Println("   PUT    /reports/{id}       (requires auth)")
	fmt.Println("   DELETE /reports/{id}       (requires auth)")
	fmt.Println("\nSee README.md for complete curl examples")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, r))
}
