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

// Product model
type Product struct {
	bun.BaseModel `bun:"table:products"`
	ID            int               `bun:"id,pk,autoincrement" json:"id"`
	Name          string            `bun:"name,notnull" json:"name"`
	SKU           string            `bun:"sku,notnull" json:"sku"`
	Price         float64           `bun:"price,notnull" json:"price"`
	Stock         int               `bun:"stock,notnull" json:"stock"`
	CreatedAt     time.Time         `bun:"created_at,notnull,skipupdate" json:"created_at"`
	UpdatedAt     time.Time         `bun:"updated_at,notnull" json:"updated_at"`
	Variants      []*ProductVariant `bun:"rel:has-many,join:id=product_id" json:"variants,omitempty"`
}

// ProductVariant model representing product variants (e.g., size, color)
type ProductVariant struct {
	bun.BaseModel `bun:"table:product_variants"`
	ID            int      `bun:"id,pk,autoincrement" json:"id"`
	ProductID     int      `bun:"product_id,notnull" json:"product_id"`
	Product       *Product `bun:"rel:belongs-to,join:product_id=id" json:"-"`
	Name          string   `bun:"name,notnull" json:"name"`
	SKUSuffix     string   `bun:"sku_suffix,notnull" json:"sku_suffix"`
	PriceAdjust   float64  `bun:"price_adjust" json:"price_adjust"`
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
	if _, err := db.GetDB().NewCreateTable().Model((*ProductVariant)(nil)).IfNotExists().Exec(context.Background()); err != nil {
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

	// Register routes with batch operations enabled
	// AllPublicWithBatch enables all CRUD + batch operations as public
	b := router.NewBuilder(r)
	router.RegisterRoutes[Product](b, "/products",
		router.AllPublicWithBatch(), // Enable batch operations
		router.WithBatchLimit(100),  // Optional: limit batch size to 100 items
		router.WithFilters("Name", "SKU"),
		router.WithSorts("Name", "Price", "CreatedAt"),
		// Nested ProductVariants with relation name for ?include=Variants support
		func(b *router.Builder) {
			router.RegisterRoutes[ProductVariant](b, "/variants",
				router.AllPublicWithBatch(),
				router.WithBatchLimit(100),
				router.WithRelationName("Variants"),
			)
		},
	)

	// Start server
	fmt.Println("Server starting on :8080")
	fmt.Println("Using SQLite in-memory database")
	fmt.Println("\nAvailable endpoints:")
	fmt.Println("  POST   http://localhost:8080/products           Create product")
	fmt.Println("  GET    http://localhost:8080/products           List products")
	fmt.Println("  GET    http://localhost:8080/products/{id}      Get product")
	fmt.Println("  PUT    http://localhost:8080/products/{id}      Update product")
	fmt.Println("  PATCH  http://localhost:8080/products/{id}      Partial update product")
	fmt.Println("  DELETE http://localhost:8080/products/{id}      Delete product")
	fmt.Println("  POST   http://localhost:8080/products/batch     Batch create")
	fmt.Println("  PUT    http://localhost:8080/products/batch     Batch update")
	fmt.Println("  PATCH  http://localhost:8080/products/batch     Batch patch")
	fmt.Println("  DELETE http://localhost:8080/products/batch     Batch delete")
	fmt.Println("\nExample usage:")
	fmt.Println("")
	fmt.Println("  # Batch create products")
	fmt.Println("  curl -X POST http://localhost:8080/products/batch -H 'Content-Type: application/json' \\")
	fmt.Println("    -d '[")
	fmt.Println("      {\"name\": \"Widget A\", \"sku\": \"WA001\", \"price\": 9.99, \"stock\": 100},")
	fmt.Println("      {\"name\": \"Widget B\", \"sku\": \"WB001\", \"price\": 14.99, \"stock\": 50},")
	fmt.Println("      {\"name\": \"Widget C\", \"sku\": \"WC001\", \"price\": 19.99, \"stock\": 25}")
	fmt.Println("    ]'")
	fmt.Println("")
	fmt.Println("  # Batch update products (must include id)")
	fmt.Println("  curl -X PUT http://localhost:8080/products/batch -H 'Content-Type: application/json' \\")
	fmt.Println("    -d '[")
	fmt.Println("      {\"id\": 1, \"name\": \"Widget A\", \"sku\": \"WA001\", \"price\": 8.99, \"stock\": 150},")
	fmt.Println("      {\"id\": 2, \"name\": \"Widget B\", \"sku\": \"WB001\", \"price\": 12.99, \"stock\": 75}")
	fmt.Println("    ]'")
	fmt.Println("")
	fmt.Println("  # Batch delete products (just need id)")
	fmt.Println("  curl -X DELETE http://localhost:8080/products/batch -H 'Content-Type: application/json' \\")
	fmt.Println("    -d '[{\"id\": 1}, {\"id\": 2}]'")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, r))
}
