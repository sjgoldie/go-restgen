//nolint:gosec,gocritic,unparam,errcheck // Example code - simplified for demonstration
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
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/router"
)

// Job model - the main resource we're tracking
type Job struct {
	bun.BaseModel `bun:"table:jobs"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	Title         string    `bun:"title,notnull" json:"title"`
	Status        string    `bun:"status,notnull" json:"status"` // pending, running, completed, failed
	Priority      int       `bun:"priority,notnull" json:"priority"`
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
}

func (j *Job) BeforeAppendModel(ctx context.Context, query bun.Query) error {
	now := time.Now()
	switch query.(type) {
	case *bun.InsertQuery:
		j.CreatedAt = now
		j.UpdatedAt = now
		if j.Status == "" {
			j.Status = "pending"
		}
	case *bun.UpdateQuery:
		j.UpdatedAt = now
	}
	return nil
}

// JobAuditLog stores audit records for Job changes
type JobAuditLog struct {
	bun.BaseModel `bun:"table:job_audit_logs"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	JobID         int       `bun:"job_id,notnull" json:"job_id"`
	Operation     string    `bun:"operation,notnull" json:"operation"` // create, update, delete
	OldData       string    `bun:"old_data" json:"old_data"`           // JSON of old state
	NewData       string    `bun:"new_data" json:"new_data"`           // JSON of new state
	CreatedAt     time.Time `bun:"created_at,notnull" json:"created_at"`
}

// jobToJSON converts a Job to JSON string, returning empty string for nil
func jobToJSON(j *Job) string {
	if j == nil {
		return ""
	}
	data, err := json.Marshal(j)
	if err != nil {
		return ""
	}
	return string(data)
}

// jobAuditor creates audit records for all Job mutations
func jobAuditor(ac metadata.AuditContext[Job]) any {
	jobID := 0
	if ac.New != nil {
		jobID = ac.New.ID
	} else if ac.Old != nil {
		jobID = ac.Old.ID
	}

	return &JobAuditLog{
		JobID:     jobID,
		Operation: string(ac.Operation),
		OldData:   jobToJSON(ac.Old),
		NewData:   jobToJSON(ac.New),
		CreatedAt: time.Now(),
	}
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
	if _, err := db.GetDB().NewCreateTable().Model((*Job)(nil)).IfNotExists().Exec(context.Background()); err != nil {
		log.Fatal("Failed to create jobs table:", err)
	}
	if _, err := db.GetDB().NewCreateTable().Model((*JobAuditLog)(nil)).IfNotExists().Exec(context.Background()); err != nil {
		log.Fatal("Failed to create audit logs table:", err)
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

	// Custom endpoint to view audit logs
	r.Get("/audit-logs", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		var logs []JobAuditLog
		err := db.GetDB().NewSelect().Model(&logs).Order("id DESC").Limit(50).Scan(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "failed to fetch audit logs", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(logs)
	})

	// Register Job routes with audit
	b := router.NewBuilder(r)
	router.RegisterRoutes[Job](b, "/jobs",
		router.AllPublic(),
		router.WithFilters("Status", "Priority"),
		router.WithSorts("Title", "Priority", "CreatedAt"),
		router.WithPagination(20, 100),
		router.WithAudit(jobAuditor),
	)

	fmt.Println("Server starting on :8080")
	fmt.Println("Using SQLite in-memory database")
	fmt.Println("\nThis example demonstrates audit functionality:")
	fmt.Println("  - All Create, Update, and Delete operations are audited")
	fmt.Println("  - Audit records include old and new state as JSON")
	fmt.Println("  - Audit runs in the same transaction as the main operation")
	fmt.Println("  - If audit insert fails, the main operation is rolled back")
	fmt.Println("\nAvailable endpoints:")
	fmt.Println("  POST   http://localhost:8080/jobs           - Create a job (audited)")
	fmt.Println("  GET    http://localhost:8080/jobs           - List all jobs")
	fmt.Println("  GET    http://localhost:8080/jobs/{id}      - Get a job")
	fmt.Println("  PUT    http://localhost:8080/jobs/{id}      - Update a job (audited)")
	fmt.Println("  DELETE http://localhost:8080/jobs/{id}      - Delete a job (audited)")
	fmt.Println("  GET    http://localhost:8080/audit-logs     - View audit log")
	fmt.Println("\nExamples:")
	fmt.Println("  # Create a job")
	fmt.Println("  curl -X POST http://localhost:8080/jobs -H 'Content-Type: application/json' \\")
	fmt.Println("    -d '{\"title\": \"Data Processing\", \"priority\": 3}'")
	fmt.Println("")
	fmt.Println("  # Update the job")
	fmt.Println("  curl -X PUT http://localhost:8080/jobs/1 -H 'Content-Type: application/json' \\")
	fmt.Println("    -d '{\"id\": 1, \"title\": \"Data Processing\", \"status\": \"running\", \"priority\": 5}'")
	fmt.Println("")
	fmt.Println("  # View the audit log")
	fmt.Println("  curl http://localhost:8080/audit-logs")
	fmt.Println("")
	fmt.Println("  # Delete the job")
	fmt.Println("  curl -X DELETE http://localhost:8080/jobs/1")
	fmt.Println("")
	fmt.Println("  # View audit log again to see all operations")
	fmt.Println("  curl http://localhost:8080/audit-logs")
	log.Fatal(http.ListenAndServe(":8080", r))
}
