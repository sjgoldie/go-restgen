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
	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/handler"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/router"
	"github.com/sjgoldie/go-restgen/service"
)

// Order model
type Order struct {
	bun.BaseModel `bun:"table:orders"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	CustomerName  string    `bun:"customer_name,notnull" json:"customer_name"`
	Total         float64   `bun:"total,notnull" json:"total"`
	Status        string    `bun:"status,notnull" json:"status"`
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at"`
}

// BeforeAppendModel hook for timestamps
func (o *Order) BeforeAppendModel(_ context.Context, query bun.Query) error {
	switch query.(type) {
	case *bun.InsertQuery:
		o.CreatedAt = time.Now()
		if o.Status == "" {
			o.Status = "pending"
		}
	}
	return nil
}

// WorkflowStatus is a non-model return type for the endpoint
type WorkflowStatus struct {
	OrderID string `json:"order_id"`
	State   string `json:"state"`
	Steps   []Step `json:"steps"`
}

// Step represents a workflow step
type Step struct {
	Name      string `json:"name"`
	Completed bool   `json:"completed"`
}

// SystemInfo is the return type for the root endpoint
type SystemInfo struct {
	Version string `json:"version"`
	Uptime  string `json:"uptime"`
}

var startTime = time.Now()

// getWorkflowStatus is an item-level endpoint — GET /orders/{id}/wf-status
// Returns a WorkflowStatus (not an Order), demonstrating non-model return types.
func getWorkflowStatus(
	ctx context.Context,
	svc *service.Common[Order],
	meta *metadata.TypeMetadata,
	auth *metadata.AuthInfo,
	id string,
	item *Order,
	payload []byte,
) (any, int, error) {
	steps := []Step{
		{Name: "received", Completed: true},
		{Name: "validated", Completed: item.Status != "pending"},
		{Name: "fulfilled", Completed: item.Status == "shipped"},
	}
	return &WorkflowStatus{
		OrderID: id,
		State:   item.Status,
		Steps:   steps,
	}, http.StatusOK, nil
}

// processPayment is an item-level endpoint — POST /orders/{id}/pay
// Accepts a JSON payload with payment details and returns a receipt.
func processPayment(
	ctx context.Context,
	svc *service.Common[Order],
	meta *metadata.TypeMetadata,
	auth *metadata.AuthInfo,
	id string,
	item *Order,
	payload []byte,
) (any, int, error) {
	var req struct {
		Method string `json:"method"`
	}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &req); err != nil {
			return map[string]string{"error": "invalid payment request"}, http.StatusBadRequest, nil
		}
	}
	if req.Method == "" {
		req.Method = "card"
	}

	return map[string]any{
		"order_id":       id,
		"amount":         item.Total,
		"payment_method": req.Method,
		"status":         "paid",
	}, http.StatusOK, nil
}

// streamOrderEvents is an item-level SSE func — GET /orders/{id}/events
func streamOrderEvents(
	ctx context.Context,
	svc *service.Common[Order],
	meta *metadata.TypeMetadata,
	auth *metadata.AuthInfo,
	id string,
	item *Order,
	events chan<- handler.SSEEvent,
) error {
	events <- handler.SSEEvent{
		Event: "status",
		Data:  map[string]string{"order_id": id, "state": item.Status},
		ID:    "1",
	}
	events <- handler.SSEEvent{
		Event: "complete",
		Data:  map[string]string{"msg": "stream ended"},
		ID:    "2",
	}
	return nil
}

// getSystemInfo is a root-level endpoint — GET /system/info
func getSystemInfo(
	ctx context.Context,
	auth *metadata.AuthInfo,
	r *http.Request,
) (any, int, error) {
	return &SystemInfo{
		Version: "1.0.0",
		Uptime:  time.Since(startTime).Truncate(time.Second).String(),
	}, http.StatusOK, nil
}

// handleWebhook is a root-level endpoint — POST /webhooks/notify
func handleWebhook(
	ctx context.Context,
	auth *metadata.AuthInfo,
	r *http.Request,
) (any, int, error) {
	return map[string]any{"received": true}, http.StatusAccepted, nil
}

// streamSystemEvents is a root-level SSE func — GET /events/system
func streamSystemEvents(
	ctx context.Context,
	auth *metadata.AuthInfo,
	r *http.Request,
	events chan<- handler.SSEEvent,
) error {
	events <- handler.SSEEvent{
		Event: "heartbeat",
		Data:  map[string]string{"status": "ok"},
	}
	return nil
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

	if _, err := db.GetDB().NewCreateTable().Model((*Order)(nil)).IfNotExists().Exec(context.Background()); err != nil {
		log.Fatal("Failed to create schema:", err)
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	b := router.NewBuilder(r, db.GetDB())

	// Item-level endpoints and SSE on orders
	router.RegisterRoutes[Order](b, "/orders",
		router.AllPublic(),
		router.WithEndpoint("GET", "wf-status", getWorkflowStatus, router.AuthConfig{
			Scopes: []string{router.ScopePublic},
		}),
		router.WithEndpoint("POST", "pay", processPayment, router.AuthConfig{
			Scopes: []string{router.ScopePublic},
		}),
		router.WithSSE("events", streamOrderEvents, router.AuthConfig{
			Scopes: []string{router.ScopePublic},
		}),
	)

	// Root-level endpoints
	router.RegisterRootEndpoint(b, "GET", "/system/info", getSystemInfo, router.AllPublic())
	router.RegisterRootEndpoint(b, "POST", "/webhooks/notify", handleWebhook, router.AllPublic())

	// Root-level SSE
	router.RegisterRootSSE(b, "/events/system", streamSystemEvents, router.AllPublic())

	fmt.Println("Anything Funcs Example")
	fmt.Println("Server starting on :8080")
	fmt.Println("\nEndpoints:")
	fmt.Println("  CRUD   /orders                        Standard CRUD")
	fmt.Println("  GET    /orders/{id}/wf-status          Workflow status (endpoint)")
	fmt.Println("  POST   /orders/{id}/pay                Process payment (endpoint)")
	fmt.Println("  GET    /orders/{id}/events              SSE event stream")
	fmt.Println("  GET    /system/info                    System info (root endpoint)")
	fmt.Println("  POST   /webhooks/notify                Webhook receiver (root endpoint)")
	fmt.Println("  GET    /events/system                  System SSE stream (root SSE)")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, r))
}
