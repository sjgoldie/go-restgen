//nolint:gosec,gocritic,unparam,errcheck // Example code - simplified for demonstration
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
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/router"
)

// Blog model with UUID primary key
type Blog struct {
	bun.BaseModel `bun:"table:blogs"`
	ID            uuid.UUID `bun:"id,pk,type:uuid" json:"id"`
	Name          string    `bun:"name,notnull" json:"name"`
	Description   string    `bun:"description" json:"description"`
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
}

// BeforeAppendModel hook generates UUID for new blogs and sets timestamps
func (b *Blog) BeforeAppendModel(ctx context.Context, query bun.Query) error {
	now := time.Now()
	switch query.(type) {
	case *bun.InsertQuery:
		if b.ID == uuid.Nil {
			b.ID = uuid.New() // Generate UUID v4 for SQLite
		}
		b.CreatedAt = now
		b.UpdatedAt = now
	case *bun.UpdateQuery:
		b.UpdatedAt = now
	}
	return nil
}

// Post model with UUID primary key and UUID foreign key to Blog
type Post struct {
	bun.BaseModel `bun:"table:posts"`
	ID            uuid.UUID `bun:"id,pk,type:uuid" json:"id"`
	BlogID        uuid.UUID `bun:"blog_id,notnull,type:uuid,skipupdate" json:"blog_id"`
	Blog          *Blog     `bun:"rel:belongs-to,join:blog_id=id" json:"blog,omitempty"`
	Title         string    `bun:"title,notnull" json:"title"`
	Content       string    `bun:"content" json:"content"`
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
}

// BeforeAppendModel hook generates UUID for new posts and sets timestamps
func (p *Post) BeforeAppendModel(ctx context.Context, query bun.Query) error {
	now := time.Now()
	switch query.(type) {
	case *bun.InsertQuery:
		if p.ID == uuid.Nil {
			p.ID = uuid.New() // Generate UUID v4 for SQLite
		}
		p.CreatedAt = now
		p.UpdatedAt = now
	case *bun.UpdateQuery:
		p.UpdatedAt = now
	}
	return nil
}

func main() {
	// Configure logging to show warnings in development
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
	if _, err := db.GetDB().NewCreateTable().Model((*Blog)(nil)).IfNotExists().Exec(ctx); err != nil {
		log.Fatal("Failed to create blogs schema:", err)
	}
	if _, err := db.GetDB().NewCreateTable().Model((*Post)(nil)).IfNotExists().Exec(ctx); err != nil {
		log.Fatal("Failed to create posts schema:", err)
	}

	// Setup router
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Register CRUD routes with UUID primary keys
	// Blogs are the parent resource, Posts are nested under blogs
	b := router.NewBuilder(r, db.GetDB())
	router.RegisterRoutes[Blog](b, "/blogs",
		router.AllPublic(),
		router.WithFilters("Name"),
		router.WithSorts("Name", "CreatedAt"),
		router.WithPagination(20, 100),
		router.WithDefaultSort("-CreatedAt"),
		func(b *router.Builder) {
			// Nested posts under blogs: /blogs/{id}/posts
			router.RegisterRoutes[Post](b, "/posts",
				router.AllPublic(),
				router.WithFilters("Title"),
				router.WithSorts("Title", "CreatedAt"),
				router.WithPagination(20, 100),
				router.WithDefaultSort("-CreatedAt"),
			)
		},
	)

	// Start server
	fmt.Println("Server starting on :8080")
	fmt.Println("Using SQLite in-memory database with UUID primary keys")
	fmt.Println("\nAvailable endpoints:")
	fmt.Println("  POST   http://localhost:8080/blogs")
	fmt.Println("  GET    http://localhost:8080/blogs")
	fmt.Println("  GET    http://localhost:8080/blogs/{uuid}")
	fmt.Println("  PUT    http://localhost:8080/blogs/{uuid}")
	fmt.Println("  DELETE http://localhost:8080/blogs/{uuid}")
	fmt.Println("\n  Nested posts under blogs:")
	fmt.Println("  POST   http://localhost:8080/blogs/{uuid}/posts")
	fmt.Println("  GET    http://localhost:8080/blogs/{uuid}/posts")
	fmt.Println("  GET    http://localhost:8080/blogs/{uuid}/posts/{uuid}")
	fmt.Println("  PUT    http://localhost:8080/blogs/{uuid}/posts/{uuid}")
	fmt.Println("  DELETE http://localhost:8080/blogs/{uuid}/posts/{uuid}")
	fmt.Println("\nExamples:")
	fmt.Println("  # Create a blog")
	fmt.Println("  curl -X POST http://localhost:8080/blogs -H 'Content-Type: application/json' -d '{\"name\":\"My Blog\",\"description\":\"A test blog\"}'")
	fmt.Println("\n  # List all blogs")
	fmt.Println("  curl http://localhost:8080/blogs")
	fmt.Println("\n  # Create a post under a blog (replace {uuid} with actual blog ID)")
	fmt.Println("  curl -X POST http://localhost:8080/blogs/{uuid}/posts -H 'Content-Type: application/json' -d '{\"title\":\"First Post\",\"content\":\"Hello World\"}'")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, r))
}
