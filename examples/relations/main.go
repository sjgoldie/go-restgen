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
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/router"
	"github.com/sjgoldie/go-restgen/service"
)

// User model - top level resource
type User struct {
	bun.BaseModel `bun:"table:users"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	ExternalID    string    `bun:"external_id,unique,notnull" json:"external_id"` // Maps to auth UserID
	Name          string    `bun:"name,notnull" json:"name"`
	Email         string    `bun:"email,unique,notnull" json:"email"`
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
	// Relations - loaded via ?include=Posts
	Posts []*Post `bun:"rel:has-many,join:id=author_id" json:"posts,omitempty"`
}

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

// Post model - belongs to User (Author), has many Comments
type Post struct {
	bun.BaseModel `bun:"table:posts"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	AuthorID      int       `bun:"author_id,notnull,skipupdate" json:"author_id"`
	Author        *User     `bun:"rel:belongs-to,join:author_id=id" json:"author,omitempty"`
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

// getMe is a custom get function that returns the authenticated user
// Used for /me endpoint where there's no parent FK - the ID comes from auth context
func getMe(ctx context.Context, svc *service.Common[User], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, _ string) (*User, error) {
	db, _ := datastore.Get()
	var user User
	err := db.GetDB().NewSelect().Model(&user).Where("external_id = ?", auth.UserID).Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// updateMe is a custom update function that updates the authenticated user
func updateMe(ctx context.Context, svc *service.Common[User], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, _ string, item User) (*User, error) {
	db, _ := datastore.Get()
	var user User
	err := db.GetDB().NewSelect().Model(&user).Where("external_id = ?", auth.UserID).Scan(ctx)
	if err != nil {
		return nil, err
	}
	item.ID = user.ID
	return svc.Update(ctx, fmt.Sprintf("%d", user.ID), item)
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
		(*User)(nil),
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	b := router.NewBuilder(r)

	// Users - public access
	router.RegisterRoutes[User](b, "/users",
		router.AllPublic(),
	)

	// /me - single route with custom get/put/patch that returns current user from auth
	// This demonstrates AsSingleRouteWithUpdate("") with no parent FK - ID comes from auth context
	router.RegisterRoutes[User](b, "/me",
		router.AsSingleRouteWithUpdate(""),
		router.AuthConfig{Methods: []string{router.MethodGet, router.MethodPut, router.MethodPatch}, Scopes: []string{"user"}},
		router.WithCustomGet(getMe),
		router.WithCustomUpdate(updateMe),
	)

	// /broken-me - single route without custom get/put/patch (should return 500)
	// This demonstrates what happens when AsSingleRouteWithUpdate("") is used without custom handlers
	router.RegisterRoutes[User](b, "/broken-me",
		router.AsSingleRouteWithUpdate(""),
		router.AuthConfig{Methods: []string{router.MethodGet, router.MethodPut, router.MethodPatch}, Scopes: []string{"user"}},
	)

	// Posts - top level, ownership-based with admin bypass
	// Has belongs-to relation to User (Author) and has-many Comments
	router.RegisterRoutes[Post](b, "/posts",
		router.AllWithOwnershipUnless([]string{"OwnerID"}, "admin"),
		func(b *router.Builder) {
			// Author - single route for belongs-to relation (GET/PUT/PATCH /posts/{id}/author)
			// Also enables ?include=Author on Post
			router.RegisterRoutes[User](b, "/author",
				router.WithRelationName("Author"),
				router.AsSingleRouteWithUpdate("AuthorID"),
				router.AllPublic(),
			)
			// Comments - has-many collection route (GET /posts/{id}/comments)
			// Also enables ?include=Comments on Post
			router.RegisterRoutes[Comment](b, "/comments",
				router.AllWithOwnershipUnless([]string{"OwnerID"}, "admin"),
				router.WithRelationName("Comments"),
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
	fmt.Println("\nUsers (public):")
	fmt.Println("  GET    /users                       - List all users")
	fmt.Println("  GET    /users/{id}                  - Get user")
	fmt.Println("  POST   /users                       - Create user")
	fmt.Println("\nPosts (top level, ownership-based):")
	fmt.Println("  GET    /posts                          - List posts (filtered by ownership)")
	fmt.Println("  GET    /posts/{id}                     - Get post")
	fmt.Println("  GET    /posts/{id}?include=Author      - Get post with author")
	fmt.Println("  GET    /posts/{id}?include=Comments    - Get post with comments")
	fmt.Println("  POST   /posts                          - Create post (OwnerID auto-set)")
	fmt.Println("\nAuthor (single route - belongs-to):")
	fmt.Println("  GET    /posts/{id}/author              - Get post's author")
	fmt.Println("\nComments (nested under posts, ownership-based):")
	fmt.Println("  GET    /posts/{id}/comments         - List comments (filtered by ownership)")
	fmt.Println("  POST   /posts/{id}/comments         - Create comment (OwnerID auto-set)")
	fmt.Println("\n=== Relation Include Behavior ===")
	fmt.Println("\n1. ?include= only works for relations registered with WithRelationName()")
	fmt.Println("2. Unknown relation names are silently ignored")
	fmt.Println("3. Ownership filtering is applied to included relations")
	fmt.Println("4. Multiple relations: ?include=Author,Comments")
	fmt.Println("\n=== Single Route (AsSingleRoute) ===")
	fmt.Println("\nUse AsSingleRoute() for belongs-to relations that return a single object.")
	fmt.Println("The child's ID is resolved from the parent's relation field.")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, r))
}
