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
	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/router"
)

// User model
type User struct {
	bun.BaseModel `bun:"table:users"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	Name          string    `bun:"name,notnull" json:"name"`
	Email         string    `bun:"email,notnull" json:"email"`
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
}

// BeforeAppendModel hook is called before inserting or updating a User
// This automatically sets timestamps
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
	if _, err := db.GetDB().NewCreateTable().Model((*User)(nil)).IfNotExists().Exec(context.Background()); err != nil {
		log.Fatal("Failed to create schema:", err)
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

	// Register CRUD routes using Builder API (public for this simple example)
	// Configure filtering, sorting, and pagination
	b := router.NewBuilder(r)
	router.RegisterRoutes[User](b, "/users",
		router.AllPublic(),
		router.WithFilters("Name", "Email"),            // Allow filtering by Name and Email
		router.WithSorts("Name", "Email", "CreatedAt"), // Allow sorting by these fields
		router.WithPagination(20, 100),                 // Default 20 items, max 100
		router.WithDefaultSort("-CreatedAt"),           // Default sort by CreatedAt descending
		router.WithMaxBodySize(1024),                   // Limit JSON body to 1 KB
	)

	// Start server
	fmt.Println("Server starting on :8080")
	fmt.Println("Using SQLite in-memory database")
	fmt.Println("\nAvailable endpoints:")
	fmt.Println("  POST   http://localhost:8080/users")
	fmt.Println("  GET    http://localhost:8080/users")
	fmt.Println("  GET    http://localhost:8080/users/{id}")
	fmt.Println("  PUT    http://localhost:8080/users/{id}")
	fmt.Println("  PATCH  http://localhost:8080/users/{id}")
	fmt.Println("  DELETE http://localhost:8080/users/{id}")
	fmt.Println("\nQuery parameters for GET /users:")
	fmt.Println("  filter[Name]=value     Filter by name (eq, neq, gt, gte, lt, lte, like)")
	fmt.Println("  filter[Email]=value    Filter by email")
	fmt.Println("  sort=Name,-Email       Sort by fields (- prefix for descending)")
	fmt.Println("  limit=10               Limit results (max 100)")
	fmt.Println("  offset=20              Skip results for pagination")
	fmt.Println("  count=true             Include X-Total-Count header")
	fmt.Println("\nExamples:")
	fmt.Println("  curl 'http://localhost:8080/users?filter[Name]=Alice'")
	fmt.Println("  curl 'http://localhost:8080/users?sort=-CreatedAt&limit=10'")
	fmt.Println("  curl 'http://localhost:8080/users?limit=10&offset=20&count=true'")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, r))
}
