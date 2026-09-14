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

// Project model - partitioned by region
type Project struct {
	bun.BaseModel `bun:"table:projects"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	Region        string    `bun:"region,notnull" json:"region"`
	Name          string    `bun:"name,notnull" json:"name"`
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

// Task model - child of Project, inherits the region through its project
type Task struct {
	bun.BaseModel `bun:"table:tasks"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	ProjectID     int       `bun:"project_id,notnull,skipupdate" json:"project_id"`
	Project       *Project  `bun:"rel:belongs-to,join:project_id=id" json:"project,omitempty"`
	Title         string    `bun:"title,notnull" json:"title"`
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

// ProjectShare model - shares a project with a user at a level ("viewer" or "editor")
type ProjectShare struct {
	bun.BaseModel `bun:"table:project_shares"`
	ID            int    `bun:"id,pk,autoincrement" json:"id"`
	ProjectID     int    `bun:"project_id,notnull" json:"project_id"`
	UserID        string `bun:"user_id,notnull" json:"user_id"`
	Level         string `bun:"level,notnull" json:"level"`
}

// Order model - partitioned by region, owned by the customer who placed it
type Order struct {
	bun.BaseModel `bun:"table:orders"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	Region        string    `bun:"region,notnull" json:"region"`
	CustomerID    string    `bun:"customer_id,notnull" json:"customer_id"`
	Title         string    `bun:"title,notnull" json:"title"`
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
}

func (o *Order) BeforeAppendModel(_ context.Context, query bun.Query) error {
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

// Auth middleware that reads the caller from headers (a stand-in for a validated JWT).
// In a real application the middleware resolves roles into scopes and scoped grants.
//
//	X-User:   user ID
//	X-Scopes: comma-separated global scopes, e.g. "project:read,order:read"
//	X-Grants: semicolon-separated scoped grants, each "scope@partition=value|value",
//	          e.g. "project:read@region=emea|apac;project:write@region=emea".
//	          "project:read@region=" grants the scope with no regions (shares only).
func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := r.Header.Get("X-User")
		if userID == "" {
			next.ServeHTTP(w, r)
			return
		}

		authInfo := &router.AuthInfo{UserID: userID}
		if scopes := r.Header.Get("X-Scopes"); scopes != "" {
			authInfo.Scopes = strings.Split(scopes, ",")
		}

		if grants := r.Header.Get("X-Grants"); grants != "" {
			for _, entry := range strings.Split(grants, ";") {
				scope, rest, ok := strings.Cut(entry, "@")
				if !ok {
					http.Error(w, "invalid grant format", http.StatusUnauthorized)
					return
				}
				partition, values, ok := strings.Cut(rest, "=")
				if !ok {
					http.Error(w, "invalid grant format", http.StatusUnauthorized)
					return
				}
				grant := router.ScopedGrant{Scope: scope, Partition: partition}
				if values != "" {
					grant.Values = strings.Split(values, "|")
				}
				authInfo.Grants = append(authInfo.Grants, grant)
			}
		}

		ctx := context.WithValue(r.Context(), router.AuthInfoKey, authInfo)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// projectShares accepts project shares at the given levels (any share when none).
// target is nil on the project route and (*Project)(nil) on its child routes.
func projectShares(target any, levels ...string) *router.ShareConfig {
	return &router.ShareConfig{
		Model:       (*ProjectShare)(nil),
		Target:      target,
		TargetField: "ProjectID",
		UserField:   "UserID",
		LevelField:  "Level",
		Levels:      levels,
	}
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
		(*Project)(nil),
		(*Task)(nil),
		(*ProjectShare)(nil),
		(*Order)(nil),
	}

	for _, model := range models {
		if _, err := db.GetDB().NewCreateTable().Model(model).IfNotExists().Exec(ctx); err != nil {
			log.Fatal("Failed to create table:", err)
		}
	}

	r := chi.NewRouter()
	r.Use(authMiddleware)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	b := router.NewBuilder(r)

	// Project - partitioned by region, and shareable
	// A global scope sees every region; a scoped grant sees only its regions.
	// Any share lets a user read the project; an editor share also lets them update it.
	// Creating and deleting projects never goes through a share.
	router.RegisterRoutes[Project](b, "/projects",
		router.WithPartition("region", "Region"),
		router.AuthConfig{
			Methods: []string{router.MethodGet, router.MethodList},
			Scopes:  []string{"project:read"},
			Share:   projectShares(nil),
		},
		router.AuthConfig{
			Methods: []string{router.MethodPut, router.MethodPatch},
			Scopes:  []string{"project:write"},
			Share:   projectShares(nil, "editor"),
		},
		router.AuthConfig{
			Methods: []string{router.MethodPost, router.MethodDelete},
			Scopes:  []string{"project:write"},
		},
		router.WithFilters("Name"),
		func(b *router.Builder) {
			// Task - inherits the region through its project
			// A share on the project reaches its tasks: any share reads them, an editor share
			// creates and updates them. Deleting tasks never goes through a share.
			router.RegisterRoutes[Task](b, "/tasks",
				router.AuthConfig{
					Methods: []string{router.MethodGet, router.MethodList},
					Scopes:  []string{"task:read"},
					Share:   projectShares((*Project)(nil)),
				},
				router.AuthConfig{
					Methods: []string{router.MethodPost, router.MethodPut, router.MethodPatch},
					Scopes:  []string{"task:write"},
					Share:   projectShares((*Project)(nil), "editor"),
				},
				router.AuthConfig{
					Methods: []string{router.MethodDelete},
					Scopes:  []string{"task:write"},
				},
				router.WithRelationName("Tasks"),
			)
		},
	)

	// ProjectShare - who a project is shared with
	router.RegisterRoutes[ProjectShare](b, "/project-shares",
		router.AllScoped("project:share"),
	)

	// Order - partitioned by region, with ownership
	// Customers see the orders they placed. A support grant lifts ownership within its
	// regions only; a global support scope lifts it everywhere.
	router.RegisterRoutes[Order](b, "/orders",
		router.WithPartition("region", "Region"),
		router.AuthConfig{
			Methods: []string{router.MethodAll},
			Scopes:  []string{"order:read"},
			Ownership: &router.OwnershipConfig{
				Fields:       []string{"CustomerID"},
				BypassScopes: []string{"support"},
			},
		},
	)

	fmt.Println("Scoped Roles Example Server starting")
	fmt.Println("Using SQLite in-memory database")
	fmt.Println("\n=== Authentication (headers) ===")
	fmt.Println("  X-User:   user ID")
	fmt.Println("  X-Scopes: global scopes, e.g. project:read,project:write")
	fmt.Println("  X-Grants: scoped grants, e.g. project:read@region=emea|apac;project:write@region=emea")
	fmt.Println("\n=== Routes ===")
	fmt.Println("\n1. Projects - partitioned by region, shareable")
	fmt.Println("   GET    /projects                  (project:read, narrowed by grants, plus shared projects)")
	fmt.Println("   POST   /projects                  (project:write, region must be within access)")
	fmt.Println("   PATCH  /projects/{id}             (project:write, or an editor share)")
	fmt.Println("   GET    /projects?include=Tasks    (tasks narrowed by task:read access)")
	fmt.Println("\n2. Tasks - child of Project, inherits the region")
	fmt.Println("   GET    /projects/{id}/tasks       (task:read, or any share on the project)")
	fmt.Println("   POST   /projects/{id}/tasks       (task:write, or an editor share on the project)")
	fmt.Println("\n3. Project shares")
	fmt.Println("   POST   /project-shares            (project:share)")
	fmt.Println("\n4. Orders - partitioned by region, owned by the customer")
	fmt.Println("   GET    /orders                    (own orders; a support grant adds its regions)")
	fmt.Println("   POST   /orders                    (customer_id auto-set)")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, r))
}
