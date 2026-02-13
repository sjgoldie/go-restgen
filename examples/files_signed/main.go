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
	"github.com/sjgoldie/go-restgen/filestore"
	"github.com/sjgoldie/go-restgen/router"
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

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Create local file storage
	uploadDir := "./uploads"
	storage, err := NewLocalStorage(uploadDir, "http://localhost:"+port+"/files")
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
	if err := filestore.Initialize(storage); err != nil {
		log.Fatal("Failed to initialize file storage:", err)
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

	// Serve static files directly (simulates "signed URL" direct access)
	// In production with S3/Minio, clients would access storage directly
	fileServer := http.FileServer(http.Dir(uploadDir))
	r.Handle("/files/*", http.StripPrefix("/files/", fileServer))

	// Register routes
	b := router.NewBuilder(r, db.GetDB())

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
	base := "http://localhost:" + port
	fmt.Println("Server starting on :" + port)
	fmt.Println("Using SQLite in-memory database")
	fmt.Println("Using local file storage:", uploadDir)
	fmt.Println("\nFile download mode: SIGNED URL (direct access via /files)")
	fmt.Println("\nAvailable endpoints:")
	fmt.Println("  POST   " + base + "/posts")
	fmt.Println("  GET    " + base + "/posts")
	fmt.Println("  GET    " + base + "/posts/{id}")
	fmt.Println("  PUT    " + base + "/posts/{id}")
	fmt.Println("  DELETE " + base + "/posts/{id}")
	fmt.Println("")
	fmt.Println("  POST   " + base + "/posts/{id}/images  (multipart upload)")
	fmt.Println("  GET    " + base + "/posts/{id}/images")
	fmt.Println("  GET    " + base + "/posts/{id}/images/{id}")
	fmt.Println("  DELETE " + base + "/posts/{id}/images/{id}")
	fmt.Println("\n  NOTE: No /download endpoint - download_url points to /files/{key}")
	fmt.Println("\nUpload example:")
	fmt.Printf("  curl -X POST %s/posts/1/images \\\n", base)
	fmt.Println(`    -F "file=@image.png" \`)
	fmt.Println(`    -F 'metadata={"alt_text":"My image"}'`)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
