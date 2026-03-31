//nolint:gosec,gocritic,unparam // Example code - simplified for demonstration
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
	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/router"
)

type User struct {
	bun.BaseModel `bun:"table:users"`
	ID            int        `bun:"id,pk,autoincrement" json:"id"`
	Name          string     `bun:"name,notnull" json:"name"`
	Email         string     `bun:"email,unique,notnull" json:"email"`
	Accounts      []*Account `bun:"rel:has-many,join:id=user_id" json:"-"`
}

type Account struct {
	bun.BaseModel `bun:"table:accounts"`
	ID            int     `bun:"id,pk,autoincrement" json:"id"`
	UserID        int     `bun:"user_id,notnull" json:"user_id"`
	User          *User   `bun:"rel:belongs-to,join:user_id=id" json:"-"`
	Name          string  `bun:"name,notnull" json:"name"`
	Sites         []*Site `bun:"rel:has-many,join:id=account_id" json:"-"`
}

type Site struct {
	bun.BaseModel `bun:"table:sites"`
	ID            int          `bun:"id,pk,autoincrement" json:"id"`
	AccountID     int          `bun:"account_id,notnull" json:"account_id"`
	Account       *Account     `bun:"rel:belongs-to,join:account_id=id" json:"-"`
	NMI           string       `bun:"nmi,notnull" json:"nmi"`
	Address       string       `bun:"address,notnull" json:"address"`
	UsageData     []*UsageData `bun:"rel:has-many,join:nmi=nmi" json:"usage_data,omitempty"`
}

// UsageData is independently ingested data keyed by NMI + date.
// There is no FK back to Site — the relationship is through the shared NMI field.
type UsageData struct {
	bun.BaseModel `bun:"table:usage_data"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	NMI           string    `bun:"nmi,notnull" json:"nmi"`
	Date          time.Time `bun:"date,notnull" json:"date"`
	KWh           float64   `bun:"kwh,notnull" json:"kwh"`
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
	for _, model := range []any{(*User)(nil), (*Account)(nil), (*Site)(nil), (*UsageData)(nil)} {
		if _, err := db.GetDB().NewCreateTable().Model(model).IfNotExists().Exec(ctx); err != nil {
			log.Fatal("Failed to create table:", err)
		}
	}

	// Seed usage data (ingested independently, keyed by NMI)
	seedUsageData(db, ctx)

	r := chi.NewRouter()

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	b := router.NewBuilder(r)
	router.RegisterRoutes[User](b, "/users", router.AllPublic(), func(b *router.Builder) {
		router.RegisterRoutes[Account](b, "/accounts", router.AllPublic(), func(b *router.Builder) {
			router.RegisterRoutes[Site](b, "/sites", router.AllPublic(), func(b *router.Builder) {
				router.RegisterRoutes[UsageData](b, "/usage-data",
					router.WithRelationName("UsageData"),
					router.WithJoinOn("NMI", "NMI"),
					router.AllPublic(),
					router.QueryConfig{
						FilterableFields: []string{"Date", "KWh"},
						SortableFields:   []string{"Date", "KWh"},
						DefaultSort:      "Date",
					},
				)
			})
		})
	})

	// Routes created:
	//   /users                                                     CRUD
	//   /users/{userId}/accounts                                   CRUD
	//   /users/{userId}/accounts/{accountId}/sites                 CRUD
	//   /users/{userId}/accounts/{accountId}/sites/{siteId}/usage-data   list + get (joined on NMI)

	fmt.Println("Server starting on :" + getPort())
	fmt.Println("Using SQLite in-memory database")
	fmt.Println("\nThis example demonstrates WithJoinOn for non-FK relationships.")
	fmt.Println("Site -> UsageData is joined on NMI (shared attribute), not a foreign key.")
	fmt.Println("\nPre-seeded usage data for NMIs: NMI001, NMI002")
	fmt.Println("\nExample flow:")
	fmt.Println("  POST /users                                                      {\"name\":\"Alice\",\"email\":\"alice@example.com\"}")
	fmt.Println("  POST /users/1/accounts                                           {\"name\":\"Home Account\"}")
	fmt.Println("  POST /users/1/accounts/1/sites                                   {\"nmi\":\"NMI001\",\"address\":\"123 Main St\"}")
	fmt.Println("  GET  /users/1/accounts/1/sites/1/usage-data                      (returns usage for NMI001)")
	log.Fatal(http.ListenAndServe(":"+getPort(), r))
}

func getPort() string {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	return port
}

func seedUsageData(db *datastore.SQLite, ctx context.Context) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 5 {
		_, _ = db.GetDB().NewInsert().Model(&UsageData{
			NMI:  "NMI001",
			Date: base.AddDate(0, 0, i),
			KWh:  10.5 + float64(i)*2.0,
		}).Exec(ctx)
	}
	for i := range 3 {
		_, _ = db.GetDB().NewInsert().Model(&UsageData{
			NMI:  "NMI002",
			Date: base.AddDate(0, 0, i),
			KWh:  5.0 + float64(i)*1.5,
		}).Exec(ctx)
	}
}
