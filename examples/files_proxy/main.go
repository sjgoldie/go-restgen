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
	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/filestore"
	"github.com/sjgoldie/go-restgen/router"
	"github.com/uptrace/bun"
)

// Post model - parent for images
type Post struct {
	bun.BaseModel `bun:"table:posts"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	Title         string    `bun:"title,notnull" json:"title"`
	Content       string    `bun:"content" json:"content"`
	Images        []*Image  `bun:"rel:has-many,join:id=post_id" json:"-"`
	CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
}

func (p *Post) BeforeAppendModel(ctx context.Context, query bun.Query) error {
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

// Image model - file resource attached to posts
type Image struct {
	bun.BaseModel        `bun:"table:images"`
	ID                   int       `bun:"id,pk,autoincrement" json:"id"`
	PostID               int       `bun:"post_id,notnull" json:"post_id"`
	Post                 *Post     `bun:"rel:belongs-to,join:post_id=id" json:"-"`
	filestore.FileFields           // Embeds StorageKey, Filename, ContentType, Size, DownloadURL
	AltText              string    `bun:"alt_text" json:"alt_text,omitempty"`
	CreatedAt            time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
}

func (i *Image) BeforeAppendModel(ctx context.Context, query bun.Query) error {
	switch query.(type) {
	case *bun.InsertQuery:
		i.CreatedAt = time.Now()
	}
	return nil
}

func main() {
	// Configure logging
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))
	slog.SetDefault(logger)

	// Create local file storage
	uploadDir := "./uploads"
	storage, err := NewLocalStorage(uploadDir, "/files")
	if err != nil {
		log.Fatal("Failed to create storage:", err)
	}

	// Create SQLite in-memory database
	db, err := datastore.NewSQLite(":memory:")
	if err != nil {
		log.Fatal("Failed to create datastore:", err)
	}

	// Initialize the global datastore
	if err := datastore.Initialize(db); err != nil {
		log.Fatal("Failed to initialize datastore:", err)
	}
	defer datastore.Cleanup()

	// Create schema
	ctx := context.Background()
	bunDB := db.GetDB()
	if _, err := bunDB.NewCreateTable().Model((*Post)(nil)).IfNotExists().Exec(ctx); err != nil {
		log.Fatal("Failed to create posts table:", err)
	}
	if _, err := bunDB.NewCreateTable().Model((*Image)(nil)).IfNotExists().Exec(ctx); err != nil {
		log.Fatal("Failed to create images table:", err)
	}

	// Initialize file storage (global singleton, like datastore)
	if err := filestore.Initialize(storage, filestore.StorageProxy); err != nil {
		log.Fatal("Failed to initialize file storage:", err)
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

	// Register routes
	b := router.NewBuilder(r)

	// Posts with nested images
	router.RegisterRoutes[Post](b, "/posts",
		router.AllPublic(),
		router.WithFilters("Title"),
		router.WithSorts("Title", "CreatedAt"),
		router.WithDefaultSort("-CreatedAt"),
		func(b *router.Builder) {
			// Images as file resources under posts
			router.RegisterRoutes[Image](b, "/images",
				router.AsFileResource(),
				router.AllPublic(),
				router.WithFilters("Filename", "ContentType"),
				router.WithSorts("Filename", "CreatedAt"),
			)
		},
	)

	// Start server
	fmt.Println("Server starting on :8080")
	fmt.Println("Using SQLite in-memory database")
	fmt.Println("Using local file storage:", uploadDir)
	fmt.Println("\nFile download mode: PROXY (files stream through server)")
	fmt.Println("\nAvailable endpoints:")
	fmt.Println("  POST   http://localhost:8080/posts")
	fmt.Println("  GET    http://localhost:8080/posts")
	fmt.Println("  GET    http://localhost:8080/posts/{id}")
	fmt.Println("  PUT    http://localhost:8080/posts/{id}")
	fmt.Println("  DELETE http://localhost:8080/posts/{id}")
	fmt.Println("")
	fmt.Println("  POST   http://localhost:8080/posts/{id}/images  (multipart upload)")
	fmt.Println("  GET    http://localhost:8080/posts/{id}/images")
	fmt.Println("  GET    http://localhost:8080/posts/{id}/images/{id}")
	fmt.Println("  GET    http://localhost:8080/posts/{id}/images/{id}/download")
	fmt.Println("  DELETE http://localhost:8080/posts/{id}/images/{id}")
	fmt.Println("\nUpload example:")
	fmt.Println(`  curl -X POST http://localhost:8080/posts/1/images \`)
	fmt.Println(`    -F "file=@image.png" \`)
	fmt.Println(`    -F 'metadata={"alt_text":"My image"}'`)
	log.Fatal(http.ListenAndServe(":8080", r))
}
