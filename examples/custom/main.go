//nolint:gosec,gocritic,unparam,staticcheck // Example code - simplified for demonstration
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/filestore"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/router"
	"github.com/sjgoldie/go-restgen/service"
)

// Global db reference for custom handlers
var db datastore.Store

// User model - demonstrates custom Get handler for /me endpoint
type User struct {
	bun.BaseModel `bun:"table:users"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	ExternalID    string    `bun:"external_id,notnull,unique" json:"external_id"` // Maps to auth UserID
	Name          string    `bun:"name,notnull" json:"name"`
	Email         string    `bun:"email,notnull" json:"email"`
	Bio           string    `bun:"bio" json:"bio"`
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
}

func (u *User) BeforeAppendModel(_ context.Context, query bun.Query) error {
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

// Task model - demonstrates custom GetAll with filtering by owner
type Task struct {
	bun.BaseModel `bun:"table:tasks"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	OwnerID       string    `bun:"owner_id,notnull" json:"owner_id"`
	Title         string    `bun:"title,notnull" json:"title"`
	Description   string    `bun:"description" json:"description"`
	Status        string    `bun:"status,notnull,default:'pending'" json:"status"`
	Priority      int       `bun:"priority,notnull,default:0" json:"priority"`
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
}

func (t *Task) BeforeAppendModel(_ context.Context, query bun.Query) error {
	now := time.Now()
	switch query.(type) {
	case *bun.InsertQuery:
		t.CreatedAt = now
		t.UpdatedAt = now
	case *bun.UpdateQuery:
		t.UpdatedAt = now
	}
	return nil
}

// Project model - demonstrates custom Create that auto-sets owner
type Project struct {
	bun.BaseModel `bun:"table:projects"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	OwnerID       string    `bun:"owner_id,notnull" json:"owner_id"`
	Name          string    `bun:"name,notnull" json:"name"`
	Description   string    `bun:"description" json:"description"`
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
	Items         []*Item   `bun:"rel:has-many,join:id=project_id" json:"-"`
}

func (p *Project) BeforeAppendModel(_ context.Context, query bun.Query) error {
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

// Item model - nested under Project, demonstrates custom Update with validation
type Item struct {
	bun.BaseModel `bun:"table:items"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	ProjectID     int       `bun:"project_id,notnull,skipupdate" json:"project_id"`
	Project       *Project  `bun:"rel:belongs-to,join:project_id=id" json:"-"`
	Name          string    `bun:"name,notnull" json:"name"`
	Quantity      int       `bun:"quantity,notnull,default:1" json:"quantity"`
	Notes         string    `bun:"notes" json:"notes"`
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
}

func (i *Item) BeforeAppendModel(_ context.Context, query bun.Query) error {
	now := time.Now()
	switch query.(type) {
	case *bun.InsertQuery:
		i.CreatedAt = now
		i.UpdatedAt = now
	case *bun.UpdateQuery:
		i.UpdatedAt = now
	}
	return nil
}

// AuditLog model - demonstrates custom Delete that prevents deletion
type AuditLog struct {
	bun.BaseModel `bun:"table:audit_logs"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	Action        string    `bun:"action,notnull" json:"action"`
	Resource      string    `bun:"resource,notnull" json:"resource"`
	ResourceID    int       `bun:"resource_id" json:"resource_id"`
	UserID        string    `bun:"user_id" json:"user_id"`
	Details       string    `bun:"details" json:"details"`
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
}

func (a *AuditLog) BeforeAppendModel(_ context.Context, query bun.Query) error {
	now := time.Now()
	switch query.(type) {
	case *bun.InsertQuery:
		a.CreatedAt = now
	}
	return nil
}

// Simple auth middleware that parses fake bearer tokens
// Token format: user:<userID>:<scope1>,<scope2>,...
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

// Custom handler: Get current user from auth token instead of URL param
func customGetMe(ctx context.Context, svc *service.Common[User], _ *metadata.TypeMetadata, auth *metadata.AuthInfo, _ string) (*User, error) {
	if auth == nil {
		return nil, fmt.Errorf("not authenticated")
	}
	// Find user by external_id (auth UserID) instead of primary key
	var user User
	err := db.GetDB().NewSelect().Model(&user).Where("external_id = ?", auth.UserID).Scan(ctx)
	if err != nil {
		return nil, err
	}
	_ = svc
	return &user, nil
}

// Custom handler: Update current user from auth token
func customUpdateMe(ctx context.Context, svc *service.Common[User], _ *metadata.TypeMetadata, auth *metadata.AuthInfo, _ string, item User) (*User, error) {
	if auth == nil {
		return nil, fmt.Errorf("not authenticated")
	}
	// Find existing user
	var existing User
	err := db.GetDB().NewSelect().Model(&existing).Where("external_id = ?", auth.UserID).Scan(ctx)
	if err != nil {
		return nil, err
	}
	// Update using the existing user's ID
	item.ID = existing.ID
	item.ExternalID = existing.ExternalID // Can't change external ID
	return svc.Update(ctx, fmt.Sprintf("%d", existing.ID), item)
}

// Custom handler: GetAll tasks filtered by current user
func customGetMyTasks(ctx context.Context, svc *service.Common[Task], _ *metadata.TypeMetadata, auth *metadata.AuthInfo) ([]*Task, int, map[string]float64, error) {
	if auth == nil {
		return nil, 0, nil, fmt.Errorf("not authenticated")
	}
	// Get all tasks for current user
	tasks := []*Task{} // Initialize as empty slice, not nil
	err := db.GetDB().NewSelect().Model(&tasks).Where("owner_id = ?", auth.UserID).Order("priority DESC", "created_at DESC").Scan(ctx)
	if err != nil {
		return nil, 0, nil, err
	}
	_ = svc
	return tasks, len(tasks), nil, nil
}

// Custom handler: Create task with auto-set owner
func customCreateTask(ctx context.Context, svc *service.Common[Task], _ *metadata.TypeMetadata, auth *metadata.AuthInfo, item Task, _ io.Reader, _ filestore.FileMetadata) (*Task, error) {
	if auth == nil {
		return nil, fmt.Errorf("not authenticated")
	}
	// Auto-set owner to current user
	item.OwnerID = auth.UserID
	return svc.Create(ctx, item)
}

// Custom handler: Create project with auto-set owner
func customCreateProject(ctx context.Context, svc *service.Common[Project], _ *metadata.TypeMetadata, auth *metadata.AuthInfo, item Project, _ io.Reader, _ filestore.FileMetadata) (*Project, error) {
	if auth == nil {
		return nil, fmt.Errorf("not authenticated")
	}
	item.OwnerID = auth.UserID
	return svc.Create(ctx, item)
}

// Custom handler: Update item with quantity validation
func customUpdateItem(ctx context.Context, svc *service.Common[Item], _ *metadata.TypeMetadata, _ *metadata.AuthInfo, id string, item Item) (*Item, error) {
	if item.Quantity < 0 {
		return nil, fmt.Errorf("quantity cannot be negative")
	}
	if item.Quantity > 1000 {
		return nil, fmt.Errorf("quantity cannot exceed 1000")
	}
	return svc.Update(ctx, id, item)
}

// Custom handler: Prevent deletion of audit logs
func customDeleteAuditLog(_ context.Context, _ *service.Common[AuditLog], _ *metadata.TypeMetadata, _ *metadata.AuthInfo, _ string) error {
	return fmt.Errorf("audit logs cannot be deleted")
}

func main() {
	// Configure logging
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))
	slog.SetDefault(logger)

	// Create SQLite in-memory database
	var err error
	db, err = datastore.NewSQLite(":memory:")
	if err != nil {
		log.Fatal("Failed to create datastore:", err)
	}

	if err := datastore.Initialize(db); err != nil {
		log.Fatal("Failed to initialize datastore:", err)
	}
	defer datastore.Cleanup()

	// Create schema
	ctx := context.Background()
	models := []interface{}{
		(*User)(nil),
		(*Task)(nil),
		(*Project)(nil),
		(*Item)(nil),
		(*AuditLog)(nil),
	}

	for _, model := range models {
		if _, err := db.GetDB().NewCreateTable().Model(model).IfNotExists().Exec(ctx); err != nil {
			log.Fatal("Failed to create table:", err)
		}
	}

	// Seed a test user
	testUser := &User{ExternalID: "alice", Name: "Alice Smith", Email: "alice@example.com"}
	_, _ = db.GetDB().NewInsert().Model(testUser).Exec(ctx)

	// Create router
	r := chi.NewRouter()
	r.Use(authMiddleware)

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	b := router.NewBuilder(r)

	// /me endpoint - single route with custom Get and Update using auth token
	// AsSingleRouteWithUpdate("") creates GET, PUT, and PATCH /me (no {id} parameter)
	router.RegisterRoutes[User](b, "/me",
		router.AsSingleRouteWithUpdate(""), // Empty string = no parent FK, ID from custom logic
		router.IsAuthenticated(),
		router.WithCustomGet(customGetMe),
		router.WithCustomUpdate(customUpdateMe),
	)

	// /users - standard CRUD for admin
	router.RegisterRoutes[User](b, "/users",
		router.AllScoped("admin"),
	)

	// /my-tasks - custom GetAll and Create
	router.RegisterRoutes[Task](b, "/my-tasks",
		router.IsAuthenticated(),
		router.WithCustomGetAll(customGetMyTasks),
		router.WithCustomCreate(customCreateTask),
	)

	// /projects with nested /items - custom Create for project, custom Update for items
	router.RegisterRoutes[Project](b, "/projects",
		router.IsAuthenticated(),
		router.WithCustomCreate(customCreateProject),
		func(b *router.Builder) {
			router.RegisterRoutes[Item](b, "/items",
				router.IsAuthenticated(),
				router.WithCustomUpdate(customUpdateItem),
			)
		},
	)

	// /audit-logs - custom Delete that prevents deletion
	router.RegisterRoutes[AuditLog](b, "/audit-logs",
		router.IsAuthenticated(),
		router.WithCustomDelete(customDeleteAuditLog),
	)

	// Print usage
	fmt.Println("Custom Handlers Example Server starting on :8080")
	fmt.Println("Using SQLite in-memory database")
	fmt.Println("\n=== Authentication ===")
	fmt.Println("Use fake bearer tokens: user:<userID>:<scope1>,<scope2>,...")
	fmt.Println("Example: Bearer user:alice:user")
	fmt.Println("\n=== Custom Handler Examples ===")
	fmt.Println("\n1. /me - Single route with custom Get/Update using auth token")
	fmt.Println("   GET  /me  -> Returns current user from auth token")
	fmt.Println("   PUT  /me  -> Updates current user from auth token")
	fmt.Println("\n2. /my-tasks - Custom GetAll/Create with auto-owner")
	fmt.Println("   GET  /my-tasks     -> Returns only current user's tasks")
	fmt.Println("   POST /my-tasks     -> Creates task with owner auto-set")
	fmt.Println("\n3. /projects/{id}/items - Custom Update with validation")
	fmt.Println("   PUT  /projects/{id}/items/{id} -> Validates quantity (0-1000)")
	fmt.Println("\n4. /audit-logs - Custom Delete that prevents deletion")
	fmt.Println("   DELETE /audit-logs/{id} -> Always returns error")
	fmt.Println("\nTest user seeded: alice (external_id: alice)")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, r))
}
