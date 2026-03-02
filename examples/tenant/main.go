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
	"github.com/sjgoldie/go-restgen/router"
)

// Organization model - the tenant entity itself (IsTenantTable)
// The primary key IS the tenant ID, so queries filter by WHERE id = tenantID
type Organization struct {
	bun.BaseModel `bun:"table:organizations"`
	ID            string    `bun:"id,pk" json:"id"`
	Name          string    `bun:"name,notnull" json:"name"`
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
}

func (o *Organization) BeforeAppendModel(_ context.Context, query bun.Query) error {
	now := time.Now()
	switch query.(type) {
	case *bun.InsertQuery:
		o.CreatedAt = now
		o.UpdatedAt = now
	case *bun.UpdateQuery:
		o.UpdatedAt = now
	}
	return nil
}

// Project model - tenant-scoped with ownership and admin bypass
type Project struct {
	bun.BaseModel `bun:"table:projects"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	OrgID         string    `bun:"org_id,notnull" json:"org_id"`
	OwnerID       string    `bun:"owner_id,notnull" json:"owner_id"`
	Name          string    `bun:"name,notnull" json:"name"`
	Description   string    `bun:"description" json:"description"`
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
	Tasks         []*Task   `bun:"rel:has-many,join:id=project_id" json:"tasks,omitempty"`
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

// Task model - child of Project, inherits tenant scope
type Task struct {
	bun.BaseModel `bun:"table:tasks"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	ProjectID     int       `bun:"project_id,notnull,skipupdate" json:"project_id"`
	OrgID         string    `bun:"org_id,notnull" json:"org_id"`
	Title         string    `bun:"title,notnull" json:"title"`
	Status        string    `bun:"status,notnull,default:'open'" json:"status"`
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
	Project       *Project  `bun:"rel:belongs-to,join:project_id=id" json:"project,omitempty"`
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

// IconColor model - global data, no tenant scoping (available to everyone)
type IconColor struct {
	bun.BaseModel `bun:"table:icon_colors"`
	ID            int    `bun:"id,pk,autoincrement" json:"id"`
	Name          string `bun:"name,notnull" json:"name"`
	Hex           string `bun:"hex,notnull" json:"hex"`
}

// Auth middleware that parses fake bearer tokens with tenant ID
// Token format: user:<userID>:<tenantID>:<scope1>,<scope2>,...
// Examples:
//
//	"user:alice:org-a:user"          - Alice in org-a with user scope
//	"user:bob:org-a:user,admin"      - Bob admin in org-a
//	"user:charlie:org-b:user"        - Charlie in org-b (different tenant)
//	"user:diana::user"               - Diana with no tenant (missing TenantID)
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

		tokenParts := strings.SplitN(token, ":", 4)
		if len(tokenParts) != 4 || tokenParts[0] != "user" {
			http.Error(w, "invalid token format", http.StatusUnauthorized)
			return
		}

		userID := tokenParts[1]
		tenantID := tokenParts[2]
		scopesPart := tokenParts[3]

		var scopes []string
		if scopesPart != "" {
			scopes = strings.Split(scopesPart, ",")
		}

		authInfo := &router.AuthInfo{
			UserID:   userID,
			TenantID: tenantID,
			Scopes:   scopes,
		}

		ctx := context.WithValue(r.Context(), router.AuthInfoKey, authInfo)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
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
		(*Organization)(nil),
		(*Project)(nil),
		(*Task)(nil),
		(*IconColor)(nil),
	}

	for _, model := range models {
		if _, err := db.GetDB().NewCreateTable().Model(model).IfNotExists().Exec(ctx); err != nil {
			log.Fatal("Failed to create table:", err)
		}
	}

	// Seed global data (icon colors available to all tenants)
	colors := []IconColor{
		{Name: "Red", Hex: "#FF0000"},
		{Name: "Blue", Hex: "#0000FF"},
		{Name: "Green", Hex: "#00FF00"},
	}
	for i := range colors {
		if _, err := db.GetDB().NewInsert().Model(&colors[i]).Exec(ctx); err != nil {
			log.Fatal("Failed to seed icon color:", err)
		}
	}

	r := chi.NewRouter()
	r.Use(authMiddleware)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	b := router.NewBuilder(r, db.GetDB())

	// Organization - the tenant entity itself
	// IsTenantTable: PK = TenantID, so queries filter by WHERE id = tenantID
	// IsAuthenticated: all methods require auth (any authenticated user with TenantID)
	router.RegisterRoutes[Organization](b, "/organizations",
		router.IsTenantTable(),
		router.IsAuthenticated(),
	)

	// Project - tenant-scoped root with ownership
	// WithTenantScope: all queries filtered by org_id = TenantID, auto-set on create
	// AllWithOwnershipUnless: users see only their projects, admins see all (within tenant)
	// Children inherit tenant scope automatically
	router.RegisterRoutes[Project](b, "/projects",
		router.WithTenantScope("OrgID"),
		router.AllWithOwnershipUnless([]string{"OwnerID"}, "admin"),
		func(b *router.Builder) {
			// Task - child of Project, inherits tenant scope
			// IsAuthenticated: requires any authenticated user
			router.RegisterRoutes[Task](b, "/tasks",
				router.IsAuthenticated(),
				router.WithRelationName("Tasks"),
			)
		},
	)

	// IconColor - global data, no tenant scoping
	// Public: accessible to everyone, no auth required
	router.RegisterRoutes[IconColor](b, "/iconcolors",
		router.AllPublic(),
	)

	fmt.Println("Tenant Example Server starting")
	fmt.Println("Using SQLite in-memory database")
	fmt.Println("\n=== Authentication ===")
	fmt.Println("Token format: user:<userID>:<tenantID>:<scope1>,<scope2>,...")
	fmt.Println("\nExample tokens:")
	fmt.Println("  - Alice (org-a user):    Bearer user:alice:org-a:user")
	fmt.Println("  - Bob (org-a admin):     Bearer user:bob:org-a:user,admin")
	fmt.Println("  - Charlie (org-b user):  Bearer user:charlie:org-b:user")
	fmt.Println("  - Diana (no tenant):     Bearer user:diana::user")
	fmt.Println("\n=== Routes ===")
	fmt.Println("\n1. Organizations - Tenant entity (IsTenantTable)")
	fmt.Println("   GET    /organizations        (shows only your org)")
	fmt.Println("   GET    /organizations/{id}    (your org only, cross-tenant = 404)")
	fmt.Println("   POST   /organizations         (create org)")
	fmt.Println("\n2. Projects - Tenant-scoped + ownership")
	fmt.Println("   GET    /projects              (your tenant + your projects, admin sees all in tenant)")
	fmt.Println("   POST   /projects              (org_id auto-set from TenantID)")
	fmt.Println("   PUT    /projects/{id}         (owner or admin, within tenant)")
	fmt.Println("   DELETE /projects/{id}         (owner or admin, within tenant)")
	fmt.Println("\n3. Tasks - Child of Project (inherits tenant scope)")
	fmt.Println("   GET    /projects/{id}/tasks          (tasks in your tenant)")
	fmt.Println("   POST   /projects/{id}/tasks          (org_id auto-set)")
	fmt.Println("   GET    /projects/{id}/tasks/{taskId} (within tenant)")
	fmt.Println("\n4. Icon Colors - Global (no tenant)")
	fmt.Println("   GET    /iconcolors            (public, all tenants)")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, r))
}
