package datastore_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/uptrace/bun"
)

// Nested test models
type TestAuthor struct {
	bun.BaseModel `bun:"table:authors"`
	ID            int       `bun:"id,pk,autoincrement"`
	Name          string    `bun:"name,notnull"`
	Email         string    `bun:"email,notnull"`
	CreatedAt     time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

type TestArticle struct {
	bun.BaseModel `bun:"table:articles"`
	ID            int       `bun:"id,pk,autoincrement"`
	AuthorID      int       `bun:"author_id,notnull"`
	Title         string    `bun:"title,notnull"`
	Content       string    `bun:"content"`
	CreatedAt     time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

type TestComment struct {
	bun.BaseModel `bun:"table:comments"`
	ID            int       `bun:"id,pk,autoincrement"`
	ArticleID     int       `bun:"article_id,notnull"`
	Text          string    `bun:"text,notnull"`
	Author        string    `bun:"author,notnull"`
	CreatedAt     time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

func setupNestedTestDB(t *testing.T) (*datastore.SQLite, func()) {
	t.Helper()

	db, err := datastore.NewSQLite(":memory:")
	if err != nil {
		t.Fatal("Failed to create test database:", err)
	}

	// Register metadata for TestAuthor (root)
	authorMeta := &metadata.TypeMetadata{
		TypeID:        metadata.GenerateTypeID(),
		TypeName:      "TestAuthor",
		TableName:     "authors",
		URLParamUUID:  "authorId",
		ParentType:    nil,
		ForeignKeyCol: "",
	}
	metadata.Register(authorMeta, reflect.TypeOf(TestAuthor{}))

	// Register metadata for TestArticle (child of Author)
	articleMeta := &metadata.TypeMetadata{
		TypeID:        metadata.GenerateTypeID(),
		TypeName:      "TestArticle",
		TableName:     "articles",
		URLParamUUID:  "articleId",
		ParentType:    reflect.TypeOf(TestAuthor{}),
		ForeignKeyCol: "author_id",
	}
	metadata.Register(articleMeta, reflect.TypeOf(TestArticle{}))

	// Register metadata for TestComment (grandchild - child of Article)
	commentMeta := &metadata.TypeMetadata{
		TypeID:        metadata.GenerateTypeID(),
		TypeName:      "TestComment",
		TableName:     "comments",
		URLParamUUID:  "commentId",
		ParentType:    reflect.TypeOf(TestArticle{}),
		ForeignKeyCol: "article_id",
	}
	metadata.Register(commentMeta, reflect.TypeOf(TestComment{}))

	// Initialize datastore
	if err := datastore.Initialize(db); err != nil {
		db.Cleanup()
		t.Fatal("Failed to initialize datastore:", err)
	}

	// Create schemas
	ctx := context.Background()
	_, err = db.GetDB().NewCreateTable().Model((*TestAuthor)(nil)).IfNotExists().Exec(ctx)
	if err != nil {
		db.Cleanup()
		t.Fatal("Failed to create authors table:", err)
	}

	_, err = db.GetDB().NewCreateTable().Model((*TestArticle)(nil)).IfNotExists().Exec(ctx)
	if err != nil {
		db.Cleanup()
		t.Fatal("Failed to create articles table:", err)
	}

	_, err = db.GetDB().NewCreateTable().Model((*TestComment)(nil)).IfNotExists().Exec(ctx)
	if err != nil {
		db.Cleanup()
		t.Fatal("Failed to create comments table:", err)
	}

	cleanup := func() {
		_, _ = db.GetDB().NewDropTable().Model((*TestComment)(nil)).IfExists().Exec(ctx)
		_, _ = db.GetDB().NewDropTable().Model((*TestArticle)(nil)).IfExists().Exec(ctx)
		_, _ = db.GetDB().NewDropTable().Model((*TestAuthor)(nil)).IfExists().Exec(ctx)
		datastore.Cleanup()
		db.Cleanup()
	}

	return db, cleanup
}

func TestWrapper_Nested_Get(t *testing.T) {
	db, cleanup := setupNestedTestDB(t)
	defer cleanup()

	// Create author
	authorWrapper := &datastore.Wrapper[TestAuthor]{Store: db}
	author := TestAuthor{
		Name:  "John Doe",
		Email: "john@example.com",
	}
	createdAuthor, err := authorWrapper.Create(context.Background(), author)
	if err != nil {
		t.Fatal("Failed to create author:", err)
	}

	// Create article with parent context
	ctx := context.WithValue(context.Background(), "parentIDs", map[string]int{
		"authorId": createdAuthor.ID,
	})
	articleWrapper := &datastore.Wrapper[TestArticle]{Store: db}
	article := TestArticle{
		Title:   "Test Article",
		Content: "Content here",
	}
	createdArticle, err := articleWrapper.Create(ctx, article)
	if err != nil {
		t.Fatal("Failed to create article:", err)
	}

	// Test: Get article WITH parent context - should succeed
	ctxGet := context.WithValue(context.Background(), "parentIDs", map[string]int{
		"authorId": createdAuthor.ID,
	})
	retrieved, err := articleWrapper.Get(ctxGet, createdArticle.ID, []string{})
	if err != nil {
		t.Fatal("Failed to get article with correct parent:", err)
	}
	if retrieved.Title != "Test Article" {
		t.Errorf("Expected title 'Test Article', got '%s'", retrieved.Title)
	}

	// Test: Get article with WRONG parent context - should fail (404)
	ctxWrong := context.WithValue(context.Background(), "parentIDs", map[string]int{
		"authorId": 999, // non-existent author
	})
	_, err = articleWrapper.Get(ctxWrong, createdArticle.ID, []string{})
	if err == nil {
		t.Error("Expected error when getting article with wrong parent")
	}
}

func TestWrapper_Nested_GetAll(t *testing.T) {
	db, cleanup := setupNestedTestDB(t)
	defer cleanup()

	// Create two authors
	authorWrapper := &datastore.Wrapper[TestAuthor]{Store: db}
	author1, _ := authorWrapper.Create(context.Background(), TestAuthor{Name: "Author 1", Email: "a1@example.com"})
	author2, _ := authorWrapper.Create(context.Background(), TestAuthor{Name: "Author 2", Email: "a2@example.com"})

	// Create articles for both authors
	articleWrapper := &datastore.Wrapper[TestArticle]{Store: db}
	ctx1Create := context.WithValue(context.Background(), "parentIDs", map[string]int{"authorId": author1.ID})
	ctx2Create := context.WithValue(context.Background(), "parentIDs", map[string]int{"authorId": author2.ID})
	_, _ = articleWrapper.Create(ctx1Create, TestArticle{Title: "Article 1-1", Content: "Content"})
	_, _ = articleWrapper.Create(ctx1Create, TestArticle{Title: "Article 1-2", Content: "Content"})
	_, _ = articleWrapper.Create(ctx2Create, TestArticle{Title: "Article 2-1", Content: "Content"})

	// Test: GetAll articles for author1 - should return 2
	ctx1 := context.WithValue(context.Background(), "parentIDs", map[string]int{
		"authorId": author1.ID,
	})
	articles1, err := articleWrapper.GetAll(ctx1, []string{})
	if err != nil {
		t.Fatal("Failed to get articles for author1:", err)
	}
	if len(articles1) != 2 {
		t.Errorf("Expected 2 articles for author1, got %d", len(articles1))
	}

	// Test: GetAll articles for author2 - should return 1
	ctx2 := context.WithValue(context.Background(), "parentIDs", map[string]int{
		"authorId": author2.ID,
	})
	articles2, err := articleWrapper.GetAll(ctx2, []string{})
	if err != nil {
		t.Fatal("Failed to get articles for author2:", err)
	}
	if len(articles2) != 1 {
		t.Errorf("Expected 1 article for author2, got %d", len(articles2))
	}
}

func TestWrapper_Nested_ThreeLevels_Get(t *testing.T) {
	db, cleanup := setupNestedTestDB(t)
	defer cleanup()

	// Create author -> article -> comment chain
	authorWrapper := &datastore.Wrapper[TestAuthor]{Store: db}
	author, _ := authorWrapper.Create(context.Background(), TestAuthor{Name: "Author", Email: "a@example.com"})

	// Create article with author context
	ctxAuthor := context.WithValue(context.Background(), "parentIDs", map[string]int{
		"authorId": author.ID,
	})
	articleWrapper := &datastore.Wrapper[TestArticle]{Store: db}
	article, err := articleWrapper.Create(ctxAuthor, TestArticle{Title: "Article", Content: "Content"})
	if err != nil {
		t.Fatal("Failed to create article:", err)
	}

	// Create comment with full parent chain context
	ctxArticle := context.WithValue(context.Background(), "parentIDs", map[string]int{
		"authorId":  author.ID,
		"articleId": article.ID,
	})
	commentWrapper := &datastore.Wrapper[TestComment]{Store: db}
	comment := TestComment{
		Text:   "Great article!",
		Author: "Reader",
	}
	createdComment, err := commentWrapper.Create(ctxArticle, comment)
	if err != nil {
		t.Fatal("Failed to create comment:", err)
	}

	// Test: Get comment with full parent chain - should succeed
	ctx := context.WithValue(context.Background(), "parentIDs", map[string]int{
		"authorId":  author.ID,
		"articleId": article.ID,
	})
	retrieved, err := commentWrapper.Get(ctx, createdComment.ID, []string{})
	if err != nil {
		t.Fatal("Failed to get comment with correct parent chain:", err)
	}
	if retrieved.Text != "Great article!" {
		t.Errorf("Expected text 'Great article!', got '%s'", retrieved.Text)
	}

	// Test: Get comment with wrong article (but correct author) - should fail
	ctxWrongArticle := context.WithValue(context.Background(), "parentIDs", map[string]int{
		"authorId":  author.ID,
		"articleId": 999, // non-existent article
	})
	_, err = commentWrapper.Get(ctxWrongArticle, createdComment.ID, []string{})
	if err == nil {
		t.Error("Expected error when getting comment with wrong article")
	}

	// Test: Get comment with wrong author (but correct article) - should fail
	ctxWrongAuthor := context.WithValue(context.Background(), "parentIDs", map[string]int{
		"authorId":  999, // non-existent author
		"articleId": article.ID,
	})
	_, err = commentWrapper.Get(ctxWrongAuthor, createdComment.ID, []string{})
	if err == nil {
		t.Error("Expected error when getting comment with wrong author")
	}
}

func TestWrapper_Nested_Update(t *testing.T) {
	db, cleanup := setupNestedTestDB(t)
	defer cleanup()

	// Create author and article
	authorWrapper := &datastore.Wrapper[TestAuthor]{Store: db}
	author, _ := authorWrapper.Create(context.Background(), TestAuthor{Name: "Author", Email: "a@example.com"})

	ctxCreate := context.WithValue(context.Background(), "parentIDs", map[string]int{"authorId": author.ID})
	articleWrapper := &datastore.Wrapper[TestArticle]{Store: db}
	article, err := articleWrapper.Create(ctxCreate, TestArticle{Title: "Original", Content: "Content"})
	if err != nil {
		t.Fatal("Failed to create article:", err)
	}

	// Test: Update article with correct parent context - should succeed
	ctx := context.WithValue(context.Background(), "parentIDs", map[string]int{
		"authorId": author.ID,
	})
	article.Title = "Updated"
	updated, err := articleWrapper.Update(ctx, article.ID, *article)
	if err != nil {
		t.Fatal("Failed to update article with correct parent:", err)
	}
	if updated.Title != "Updated" {
		t.Errorf("Expected title 'Updated', got '%s'", updated.Title)
	}

	// Test: Update article with wrong parent context - should fail
	ctxWrong := context.WithValue(context.Background(), "parentIDs", map[string]int{
		"authorId": 999,
	})
	article.Title = "Should Fail"
	_, err = articleWrapper.Update(ctxWrong, article.ID, *article)
	if err == nil {
		t.Error("Expected error when updating article with wrong parent")
	}
}

func TestWrapper_Nested_Delete(t *testing.T) {
	db, cleanup := setupNestedTestDB(t)
	defer cleanup()

	// Create author and article
	authorWrapper := &datastore.Wrapper[TestAuthor]{Store: db}
	author, _ := authorWrapper.Create(context.Background(), TestAuthor{Name: "Author", Email: "a@example.com"})

	ctxCreate := context.WithValue(context.Background(), "parentIDs", map[string]int{"authorId": author.ID})
	articleWrapper := &datastore.Wrapper[TestArticle]{Store: db}
	article, err := articleWrapper.Create(ctxCreate, TestArticle{Title: "To Delete", Content: "Content"})
	if err != nil {
		t.Fatal("Failed to create article:", err)
	}

	// Test: Delete article with wrong parent context - should fail
	ctxWrong := context.WithValue(context.Background(), "parentIDs", map[string]int{
		"authorId": 999,
	})
	err = articleWrapper.Delete(ctxWrong, article.ID)
	if err == nil {
		t.Error("Expected error when deleting article with wrong parent")
	}

	// Test: Delete article with correct parent context - should succeed
	ctx := context.WithValue(context.Background(), "parentIDs", map[string]int{
		"authorId": author.ID,
	})
	err = articleWrapper.Delete(ctx, article.ID)
	if err != nil {
		t.Fatal("Failed to delete article with correct parent:", err)
	}

	// Verify deletion
	_, err = articleWrapper.Get(ctx, article.ID, []string{})
	if err == nil {
		t.Error("Expected error when getting deleted article")
	}
}
