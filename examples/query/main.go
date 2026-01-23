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

// Product model with various field types for comprehensive filter testing
type Product struct {
	bun.BaseModel `bun:"table:products"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	Name          string    `bun:"name,notnull" json:"name"`
	Category      string    `bun:"category,notnull" json:"category"`
	Price         int       `bun:"price,notnull" json:"price"`
	Stock         int       `bun:"stock,notnull" json:"stock"`
	Active        bool      `bun:"active,notnull" json:"active"`
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
}

// BeforeAppendModel hook for timestamps
func (p *Product) BeforeAppendModel(ctx context.Context, query bun.Query) error {
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

	if err := datastore.Initialize(db); err != nil {
		log.Fatal("Failed to initialize datastore:", err)
	}
	defer datastore.Cleanup()

	// Create schema
	if _, err := db.GetDB().NewCreateTable().Model((*Product)(nil)).IfNotExists().Exec(context.Background()); err != nil {
		log.Fatal("Failed to create schema:", err)
	}

	// Setup router
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Register CRUD routes with comprehensive filter/sort/pagination options
	b := router.NewBuilder(r)
	router.RegisterRoutes[Product](b, "/products",
		router.AllPublic(),
		router.WithFilters("Name", "Category", "Price", "Stock", "Active"),
		router.WithSorts("Name", "Category", "Price", "Stock", "CreatedAt"),
		router.WithPagination(20, 100),
		router.WithDefaultSort("Name"),
	)

	// Start server
	fmt.Println("Server starting on :8080")
	fmt.Println("Using SQLite in-memory database")
	fmt.Println("\nThis example demonstrates query filtering, sorting, and pagination.")
	fmt.Println("\nAvailable endpoints:")
	fmt.Println("  POST   http://localhost:8080/products")
	fmt.Println("  GET    http://localhost:8080/products")
	fmt.Println("  GET    http://localhost:8080/products/{id}")
	fmt.Println("  PUT    http://localhost:8080/products/{id}")
	fmt.Println("  DELETE http://localhost:8080/products/{id}")
	fmt.Println("\nFilter operators:")
	fmt.Println("  eq   - Equal (default):     filter[Price]=100")
	fmt.Println("  neq  - Not equal:           filter[Price][neq]=100")
	fmt.Println("  gt   - Greater than:        filter[Price][gt]=100")
	fmt.Println("  gte  - Greater or equal:    filter[Price][gte]=100")
	fmt.Println("  lt   - Less than:           filter[Price][lt]=100")
	fmt.Println("  lte  - Less or equal:       filter[Price][lte]=100")
	fmt.Println("  like - Pattern match:       filter[Name][like]=Widget%")
	fmt.Println("  in   - In list:             filter[Category][in]=Electronics,Books")
	fmt.Println("  nin  - Not in list:         filter[Category][nin]=Electronics,Books")
	fmt.Println("  bt   - Between:             filter[Price][bt]=50,150")
	fmt.Println("  nbt  - Not between:         filter[Price][nbt]=50,150")
	log.Fatal(http.ListenAndServe(":8080", r))
}
