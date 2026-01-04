//nolint:gosec,gocritic,unparam,errcheck,goconst // Example code - simplified for demonstration
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/sjgoldie/go-restgen/datastore"
	apperrors "github.com/sjgoldie/go-restgen/errors"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/router"
	"github.com/sjgoldie/go-restgen/service"
	"github.com/uptrace/bun"
)

// Order model representing an order that can be cancelled or completed
type Order struct {
	bun.BaseModel `bun:"table:orders"`
	ID            int          `bun:"id,pk,autoincrement" json:"id"`
	CustomerName  string       `bun:"customer_name,notnull" json:"customer_name"`
	Total         float64      `bun:"total,notnull" json:"total"`
	Status        string       `bun:"status,notnull" json:"status"` // pending, completed, cancelled
	CancelReason  string       `bun:"cancel_reason" json:"cancel_reason,omitempty"`
	CompletedAt   time.Time    `bun:"completed_at" json:"completed_at,omitempty"`
	CreatedAt     time.Time    `bun:"created_at,notnull,skipupdate" json:"created_at"`
	UpdatedAt     time.Time    `bun:"updated_at,notnull" json:"updated_at"`
	Items         []*OrderItem `bun:"rel:has-many,join:id=order_id" json:"items,omitempty"`
}

// OrderItem model representing items in an order
type OrderItem struct {
	bun.BaseModel `bun:"table:order_items"`
	ID            int     `bun:"id,pk,autoincrement" json:"id"`
	OrderID       int     `bun:"order_id,notnull" json:"order_id"`
	Order         *Order  `bun:"rel:belongs-to,join:order_id=id" json:"-"`
	ProductName   string  `bun:"product_name,notnull" json:"product_name"`
	Quantity      int     `bun:"quantity,notnull" json:"quantity"`
	Price         float64 `bun:"price,notnull" json:"price"`
}

// BeforeAppendModel hook for timestamps
func (o *Order) BeforeAppendModel(ctx context.Context, query bun.Query) error {
	now := time.Now()
	switch query.(type) {
	case *bun.InsertQuery:
		o.CreatedAt = now
		o.UpdatedAt = now
		if o.Status == "" {
			o.Status = "pending"
		}
	case *bun.UpdateQuery:
		o.UpdatedAt = now
	}
	return nil
}

// CancelRequest is the payload for the cancel action
type CancelRequest struct {
	Reason string `json:"reason"`
}

// cancelOrder is the action handler for POST /orders/{id}/cancel
func cancelOrder(
	ctx context.Context,
	svc *service.Common[Order],
	meta *metadata.TypeMetadata,
	auth *metadata.AuthInfo,
	id string,
	item *Order,
	payload []byte,
) (*Order, error) {
	// Validate current status
	if item.Status != "pending" {
		return nil, apperrors.NewValidationError(fmt.Sprintf("can only cancel pending orders, current status: %s", item.Status))
	}

	// Parse action payload
	var req CancelRequest
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, fmt.Errorf("invalid cancel request: %w", err)
		}
	}

	// Update order
	item.Status = "cancelled"
	item.CancelReason = req.Reason
	return svc.Update(ctx, id, *item)
}

// completeOrder is the action handler for POST /orders/{id}/complete
func completeOrder(
	ctx context.Context,
	svc *service.Common[Order],
	meta *metadata.TypeMetadata,
	auth *metadata.AuthInfo,
	id string,
	item *Order,
	payload []byte,
) (*Order, error) {
	// Validate current status
	if item.Status != "pending" {
		return nil, apperrors.NewValidationError(fmt.Sprintf("can only complete pending orders, current status: %s", item.Status))
	}

	// Update order
	item.Status = "completed"
	item.CompletedAt = time.Now()
	return svc.Update(ctx, id, *item)
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
	if _, err := db.GetDB().NewCreateTable().Model((*Order)(nil)).IfNotExists().Exec(context.Background()); err != nil {
		log.Fatal("Failed to create schema:", err)
	}
	if _, err := db.GetDB().NewCreateTable().Model((*OrderItem)(nil)).IfNotExists().Exec(context.Background()); err != nil {
		log.Fatal("Failed to create schema:", err)
	}

	// Setup router
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Register routes with actions
	b := router.NewBuilder(r)
	router.RegisterRoutes[Order](b, "/orders",
		router.AllPublic(),
		router.WithFilters("Status", "CustomerName"),
		router.WithSorts("CreatedAt", "Total"),
		// Actions - each with its own auth (public for this demo)
		router.WithAction("cancel", cancelOrder, router.AuthConfig{
			Scopes: []string{router.ScopePublic},
		}),
		router.WithAction("complete", completeOrder, router.AuthConfig{
			Scopes: []string{router.ScopePublic},
		}),
		// Nested OrderItems with relation name for ?include=Items support
		func(b *router.Builder) {
			router.RegisterRoutes[OrderItem](b, "/items",
				router.AllPublic(),
				router.WithRelationName("Items"),
			)
		},
	)

	// Start server
	fmt.Println("Server starting on :8080")
	fmt.Println("Using SQLite in-memory database")
	fmt.Println("\nAvailable endpoints:")
	fmt.Println("  POST   http://localhost:8080/orders              Create order")
	fmt.Println("  GET    http://localhost:8080/orders              List orders")
	fmt.Println("  GET    http://localhost:8080/orders/{id}         Get order")
	fmt.Println("  PUT    http://localhost:8080/orders/{id}         Update order")
	fmt.Println("  DELETE http://localhost:8080/orders/{id}         Delete order")
	fmt.Println("  POST   http://localhost:8080/orders/{id}/cancel  Cancel order (action)")
	fmt.Println("  POST   http://localhost:8080/orders/{id}/complete Complete order (action)")
	fmt.Println("\nExample usage:")
	fmt.Println("  # Create an order")
	fmt.Println("  curl -X POST http://localhost:8080/orders -H 'Content-Type: application/json' \\")
	fmt.Println("    -d '{\"customer_name\": \"Alice\", \"total\": 99.99}'")
	fmt.Println("")
	fmt.Println("  # Cancel the order with a reason")
	fmt.Println("  curl -X POST http://localhost:8080/orders/1/cancel -H 'Content-Type: application/json' \\")
	fmt.Println("    -d '{\"reason\": \"Customer requested cancellation\"}'")
	fmt.Println("")
	fmt.Println("  # Or complete the order")
	fmt.Println("  curl -X POST http://localhost:8080/orders/1/complete")
	log.Fatal(http.ListenAndServe(":8080", r))
}
