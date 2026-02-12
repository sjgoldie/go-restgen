//nolint:gosec,gocritic,unparam,errcheck // Example code - simplified for demonstration
package main

import (
	"context"
	"errors"
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
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/router"
)

// Task model - demonstrates validation with state machine transitions
type Task struct {
	bun.BaseModel `bun:"table:tasks"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	Title         string    `bun:"title,notnull" json:"title"`
	Description   string    `bun:"description" json:"description"`
	Status        string    `bun:"status,notnull" json:"status"`     // pending, in_progress, completed, cancelled
	Priority      int       `bun:"priority,notnull" json:"priority"` // 1-5
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
}

func (t *Task) BeforeAppendModel(ctx context.Context, query bun.Query) error {
	now := time.Now()
	switch query.(type) {
	case *bun.InsertQuery:
		t.CreatedAt = now
		t.UpdatedAt = now
		// Default status to pending if not set
		if t.Status == "" {
			t.Status = "pending"
		}
	case *bun.UpdateQuery:
		t.UpdatedAt = now
	}
	return nil
}

// Valid status transitions:
//
//	pending -> in_progress, cancelled
//	in_progress -> completed, cancelled
//	completed -> (none, final state)
//	cancelled -> (none, final state)
var validTransitions = map[string][]string{
	"pending":     {"in_progress", "cancelled"},
	"in_progress": {"completed", "cancelled"},
	"completed":   {},
	"cancelled":   {},
}

func isValidTransition(from, to string) bool {
	if from == to {
		return true // No change is always valid
	}
	allowed, ok := validTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

func taskValidator(vc metadata.ValidationContext[Task]) error {
	switch vc.Operation {
	case metadata.OpCreate:
		// New tasks must start in pending status
		if vc.New.Status != "" && vc.New.Status != "pending" {
			return errors.New("new tasks must start in 'pending' status")
		}
		// Priority must be 1-5
		if vc.New.Priority < 1 || vc.New.Priority > 5 {
			return errors.New("priority must be between 1 and 5")
		}
		// Title is required
		if vc.New.Title == "" {
			return errors.New("title is required")
		}

	case metadata.OpUpdate:
		// Priority must be 1-5
		if vc.New.Priority < 1 || vc.New.Priority > 5 {
			return errors.New("priority must be between 1 and 5")
		}
		// Completed/cancelled tasks cannot be modified (except this is the transition itself)
		if vc.Old.Status == "completed" || vc.Old.Status == "cancelled" {
			return fmt.Errorf("cannot modify a task in '%s' status", vc.Old.Status)
		}
		// Validate status transitions
		if !isValidTransition(vc.Old.Status, vc.New.Status) {
			return fmt.Errorf("invalid status transition from '%s' to '%s'", vc.Old.Status, vc.New.Status)
		}

	case metadata.OpDelete:
		// Cannot delete completed tasks (they should be archived instead)
		if vc.Old.Status == "completed" {
			return errors.New("cannot delete completed tasks")
		}
	}
	return nil
}

func main() {
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
	if _, err := db.GetDB().NewCreateTable().Model((*Task)(nil)).IfNotExists().Exec(context.Background()); err != nil {
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

	// Register Task routes with validation
	b := router.NewBuilder(r, db.GetDB())
	router.RegisterRoutes[Task](b, "/tasks",
		router.AllPublic(),
		router.WithFilters("Status", "Priority"),
		router.WithSorts("Title", "Priority", "CreatedAt"),
		router.WithPagination(20, 100),
		router.WithValidator(taskValidator),
	)

	fmt.Println("Server starting on :8080")
	fmt.Println("Using SQLite in-memory database")
	fmt.Println("\nThis example demonstrates validation features:")
	fmt.Println("  - New tasks must start in 'pending' status")
	fmt.Println("  - Priority must be between 1 and 5")
	fmt.Println("  - Title is required")
	fmt.Println("  - Status transitions follow a state machine:")
	fmt.Println("      pending -> in_progress, cancelled")
	fmt.Println("      in_progress -> completed, cancelled")
	fmt.Println("      completed -> (final, cannot modify)")
	fmt.Println("      cancelled -> (final, cannot modify)")
	fmt.Println("  - Completed tasks cannot be deleted")
	fmt.Println("\nAvailable endpoints:")
	fmt.Println("  POST   http://localhost:8080/tasks")
	fmt.Println("  GET    http://localhost:8080/tasks")
	fmt.Println("  GET    http://localhost:8080/tasks/{id}")
	fmt.Println("  PUT    http://localhost:8080/tasks/{id}")
	fmt.Println("  DELETE http://localhost:8080/tasks/{id}")
	fmt.Println("\nExamples:")
	fmt.Println("  # Create a task (must be pending)")
	fmt.Println("  curl -X POST http://localhost:8080/tasks -H 'Content-Type: application/json' \\")
	fmt.Println("    -d '{\"title\": \"My Task\", \"priority\": 3}'")
	fmt.Println("")
	fmt.Println("  # Try to create with invalid status (will fail)")
	fmt.Println("  curl -X POST http://localhost:8080/tasks -H 'Content-Type: application/json' \\")
	fmt.Println("    -d '{\"title\": \"My Task\", \"status\": \"completed\", \"priority\": 3}'")
	fmt.Println("")
	fmt.Println("  # Transition from pending to in_progress")
	fmt.Println("  curl -X PUT http://localhost:8080/tasks/1 -H 'Content-Type: application/json' \\")
	fmt.Println("    -d '{\"title\": \"My Task\", \"status\": \"in_progress\", \"priority\": 3}'")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, r))
}
