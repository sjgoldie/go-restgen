//nolint:dupl,goconst,staticcheck,errcheck,gosec // Benchmark code - acceptable for test files
package datastore_test

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/metadata"
)

type BenchAuthor struct {
	bun.BaseModel `bun:"table:bench_batch_authors"`
	ID            int       `bun:"id,pk,autoincrement"`
	Name          string    `bun:"name,notnull"`
	Email         string    `bun:"email,notnull"`
	CreatedAt     time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

type BenchArticle struct {
	bun.BaseModel `bun:"table:bench_batch_articles"`
	ID            int       `bun:"id,pk,autoincrement"`
	AuthorID      int       `bun:"author_id,notnull"`
	Title         string    `bun:"title,notnull"`
	Content       string    `bun:"content"`
	CreatedAt     time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

var benchAuthorMeta = &metadata.TypeMetadata{
	TypeID:       "bench_batch_author_id",
	TypeName:     "BenchAuthor",
	TableName:    "bench_batch_authors",
	URLParamUUID: "authorId",
	PKField:      "ID",
	ModelType:    reflect.TypeOf(BenchAuthor{}),
}

var benchArticleMeta = &metadata.TypeMetadata{
	TypeID:        "bench_batch_article_id",
	TypeName:      "BenchArticle",
	TableName:     "bench_batch_articles",
	URLParamUUID:  "articleId",
	PKField:       "ID",
	ModelType:     reflect.TypeOf(BenchArticle{}),
	ParentType:    reflect.TypeOf(BenchAuthor{}),
	ParentMeta:    benchAuthorMeta,
	ForeignKeyCol: "author_id",
}

func setupBenchBatchDB(b *testing.B) (*datastore.SQLite, func()) {
	b.Helper()

	db, err := datastore.NewSQLite("file::memory:?cache=shared")
	if err != nil {
		b.Fatal("Failed to create test database:", err)
	}

	ctx := context.Background()
	_, err = db.GetDB().NewCreateTable().Model((*BenchAuthor)(nil)).IfNotExists().Exec(ctx)
	if err != nil {
		db.Cleanup()
		b.Fatal("Failed to create bench_batch_authors table:", err)
	}

	_, err = db.GetDB().NewCreateTable().Model((*BenchArticle)(nil)).IfNotExists().Exec(ctx)
	if err != nil {
		db.Cleanup()
		b.Fatal("Failed to create bench_batch_articles table:", err)
	}

	cleanup := func() {
		db.GetDB().NewDropTable().Model((*BenchArticle)(nil)).IfExists().Exec(ctx)
		db.GetDB().NewDropTable().Model((*BenchAuthor)(nil)).IfExists().Exec(ctx)
		db.Cleanup()
	}

	return db, cleanup
}

func BenchmarkBatchCreate_NestedResource(b *testing.B) {
	db, cleanup := setupBenchBatchDB(b)
	defer cleanup()

	authorWrapper := &datastore.Wrapper[BenchAuthor]{Store: db}
	ctxAuthor := context.WithValue(context.Background(), metadata.MetadataKey, benchAuthorMeta)

	author := BenchAuthor{Name: "Bench Author", Email: "bench@example.com"}
	createdAuthor, err := authorWrapper.Create(ctxAuthor, author)
	if err != nil {
		b.Fatal("Failed to create author:", err)
	}

	sizes := []int{100, 500, 1000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("BatchSize_%d", size), func(b *testing.B) {
			articleWrapper := &datastore.Wrapper[BenchArticle]{Store: db}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				b.StopTimer()

				// Clean articles table between iterations
				db.GetDB().NewDelete().Model((*BenchArticle)(nil)).Where("1=1").Exec(context.Background())

				articles := make([]BenchArticle, size)
				for j := range articles {
					articles[j] = BenchArticle{
						Title:   fmt.Sprintf("Article %d", j),
						Content: fmt.Sprintf("Content for article %d", j),
					}
				}

				ctxArticle := context.WithValue(context.Background(), metadata.MetadataKey, benchArticleMeta)
				ctxArticle = context.WithValue(ctxArticle, metadata.ParentIDsKey, map[string]string{
					"authorId": strconv.Itoa(createdAuthor.ID),
				})

				b.StartTimer()

				results, err := articleWrapper.BatchCreate(ctxArticle, articles)
				if err != nil {
					b.Fatal("BatchCreate failed:", err)
				}
				if len(results) != size {
					b.Fatalf("Expected %d results, got %d", size, len(results))
				}
			}
		})
	}
}
