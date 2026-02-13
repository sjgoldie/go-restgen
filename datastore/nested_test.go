//nolint:staticcheck,goconst // Test code - string context keys and test data strings are acceptable
package datastore_test

import (
	"context"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/metadata"
)

// itoa converts int to string for ID parameters
func itoa(i int) string {
	return strconv.Itoa(i)
}

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

// Nested test metadata with parent chain
var testAuthorMeta = &metadata.TypeMetadata{
	TypeID:          "test_author_id",
	TypeName:        "TestAuthor",
	TableName:       "authors",
	URLParamUUID:    "authorId",
	PKField:         "ID",
	ModelType:       reflect.TypeOf(TestAuthor{}),
	ParentType:      nil,
	ParentMeta:      nil,
	ForeignKeyCol:   "",
	ParentJoinCol:   "id",
	ParentJoinField: "ID",
}

var testArticleMeta = &metadata.TypeMetadata{
	TypeID:          "test_article_id",
	TypeName:        "TestArticle",
	TableName:       "articles",
	URLParamUUID:    "articleId",
	PKField:         "ID",
	ModelType:       reflect.TypeOf(TestArticle{}),
	ParentType:      reflect.TypeOf(TestAuthor{}),
	ParentMeta:      testAuthorMeta,
	ForeignKeyCol:   "author_id",
	ParentJoinCol:   "id",
	ParentJoinField: "ID",
}

var testCommentMeta = &metadata.TypeMetadata{
	TypeID:          "test_comment_id",
	TypeName:        "TestComment",
	TableName:       "comments",
	URLParamUUID:    "commentId",
	PKField:         "ID",
	ModelType:       reflect.TypeOf(TestComment{}),
	ParentType:      reflect.TypeOf(TestArticle{}),
	ParentMeta:      testArticleMeta,
	ForeignKeyCol:   "article_id",
	ParentJoinCol:   "id",
	ParentJoinField: "ID",
}

// ctxWithNestedMeta creates a context with the given metadata
func ctxWithNestedMeta(meta *metadata.TypeMetadata) context.Context {
	return context.WithValue(context.Background(), metadata.MetadataKey, meta)
}

func setupNestedTestDB(t *testing.T) (*datastore.SQLite, func()) {
	t.Helper()

	db, err := datastore.NewSQLite(":memory:")
	if err != nil {
		t.Fatal("Failed to create test database:", err)
	}

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
	ctxAuthor := ctxWithNestedMeta(testAuthorMeta)
	author := TestAuthor{
		Name:  "John Doe",
		Email: "john@example.com",
	}
	createdAuthor, err := authorWrapper.Create(ctxAuthor, author)
	if err != nil {
		t.Fatal("Failed to create author:", err)
	}

	// Create article with parent context
	ctxArticle := ctxWithNestedMeta(testArticleMeta)
	ctxArticle = context.WithValue(ctxArticle, metadata.ParentIDsKey, map[string]string{
		"authorId": itoa(createdAuthor.ID),
	})
	articleWrapper := &datastore.Wrapper[TestArticle]{Store: db}
	article := TestArticle{
		Title:   "Test Article",
		Content: "Content here",
	}
	createdArticle, err := articleWrapper.Create(ctxArticle, article)
	if err != nil {
		t.Fatal("Failed to create article:", err)
	}

	// Test: Get article WITH parent context - should succeed
	ctxGet := ctxWithNestedMeta(testArticleMeta)
	ctxGet = context.WithValue(ctxGet, metadata.ParentIDsKey, map[string]string{
		"authorId": itoa(createdAuthor.ID),
	})
	retrieved, err := articleWrapper.Get(ctxGet, itoa(createdArticle.ID))
	if err != nil {
		t.Fatal("Failed to get article with correct parent:", err)
	}
	if retrieved.Title != "Test Article" {
		t.Errorf("Expected title 'Test Article', got '%s'", retrieved.Title)
	}

	// Test: Get article with WRONG parent context - should fail (404)
	ctxWrong := ctxWithNestedMeta(testArticleMeta)
	ctxWrong = context.WithValue(ctxWrong, metadata.ParentIDsKey, map[string]string{
		"authorId": "999", // non-existent author
	})
	_, err = articleWrapper.Get(ctxWrong, itoa(createdArticle.ID))
	if err == nil {
		t.Error("Expected error when getting article with wrong parent")
	}
}

func TestWrapper_Nested_GetAll(t *testing.T) {
	db, cleanup := setupNestedTestDB(t)
	defer cleanup()

	// Create two authors
	authorWrapper := &datastore.Wrapper[TestAuthor]{Store: db}
	ctxAuthor := ctxWithNestedMeta(testAuthorMeta)
	author1, _ := authorWrapper.Create(ctxAuthor, TestAuthor{Name: "Author 1", Email: "a1@example.com"})
	author2, _ := authorWrapper.Create(ctxAuthor, TestAuthor{Name: "Author 2", Email: "a2@example.com"})

	// Create articles for both authors
	articleWrapper := &datastore.Wrapper[TestArticle]{Store: db}
	ctx1Create := ctxWithNestedMeta(testArticleMeta)
	ctx1Create = context.WithValue(ctx1Create, metadata.ParentIDsKey, map[string]string{"authorId": itoa(author1.ID)})
	ctx2Create := ctxWithNestedMeta(testArticleMeta)
	ctx2Create = context.WithValue(ctx2Create, metadata.ParentIDsKey, map[string]string{"authorId": itoa(author2.ID)})
	_, _ = articleWrapper.Create(ctx1Create, TestArticle{Title: "Article 1-1", Content: "Content"})
	_, _ = articleWrapper.Create(ctx1Create, TestArticle{Title: "Article 1-2", Content: "Content"})
	_, _ = articleWrapper.Create(ctx2Create, TestArticle{Title: "Article 2-1", Content: "Content"})

	// Test: GetAll articles for author1 - should return 2
	ctx1 := ctxWithNestedMeta(testArticleMeta)
	ctx1 = context.WithValue(ctx1, metadata.ParentIDsKey, map[string]string{
		"authorId": itoa(author1.ID),
	})
	articles1, _, _, err := articleWrapper.GetAll(ctx1)
	if err != nil {
		t.Fatal("Failed to get articles for author1:", err)
	}
	if len(articles1) != 2 {
		t.Errorf("Expected 2 articles for author1, got %d", len(articles1))
	}

	// Test: GetAll articles for author2 - should return 1
	ctx2 := ctxWithNestedMeta(testArticleMeta)
	ctx2 = context.WithValue(ctx2, metadata.ParentIDsKey, map[string]string{
		"authorId": itoa(author2.ID),
	})
	articles2, _, _, err := articleWrapper.GetAll(ctx2)
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
	ctxAuthorMeta := ctxWithNestedMeta(testAuthorMeta)
	author, _ := authorWrapper.Create(ctxAuthorMeta, TestAuthor{Name: "Author", Email: "a@example.com"})

	// Create article with author context
	ctxArticleMeta := ctxWithNestedMeta(testArticleMeta)
	ctxArticleMeta = context.WithValue(ctxArticleMeta, metadata.ParentIDsKey, map[string]string{
		"authorId": itoa(author.ID),
	})
	articleWrapper := &datastore.Wrapper[TestArticle]{Store: db}
	article, err := articleWrapper.Create(ctxArticleMeta, TestArticle{Title: "Article", Content: "Content"})
	if err != nil {
		t.Fatal("Failed to create article:", err)
	}

	// Create comment with full parent chain context
	ctxCommentMeta := ctxWithNestedMeta(testCommentMeta)
	ctxCommentMeta = context.WithValue(ctxCommentMeta, metadata.ParentIDsKey, map[string]string{
		"authorId":  itoa(author.ID),
		"articleId": itoa(article.ID),
	})
	commentWrapper := &datastore.Wrapper[TestComment]{Store: db}
	comment := TestComment{
		Text:   "Great article!",
		Author: "Reader",
	}
	createdComment, err := commentWrapper.Create(ctxCommentMeta, comment)
	if err != nil {
		t.Fatal("Failed to create comment:", err)
	}

	// Test: Get comment with full parent chain - should succeed
	ctx := ctxWithNestedMeta(testCommentMeta)
	ctx = context.WithValue(ctx, metadata.ParentIDsKey, map[string]string{
		"authorId":  itoa(author.ID),
		"articleId": itoa(article.ID),
	})
	retrieved, err := commentWrapper.Get(ctx, itoa(createdComment.ID))
	if err != nil {
		t.Fatal("Failed to get comment with correct parent chain:", err)
	}
	if retrieved.Text != "Great article!" {
		t.Errorf("Expected text 'Great article!', got '%s'", retrieved.Text)
	}

	// Test: Get comment with wrong article (but correct author) - should fail
	ctxWrongArticle := ctxWithNestedMeta(testCommentMeta)
	ctxWrongArticle = context.WithValue(ctxWrongArticle, metadata.ParentIDsKey, map[string]string{
		"authorId":  itoa(author.ID),
		"articleId": "999", // non-existent article
	})
	_, err = commentWrapper.Get(ctxWrongArticle, itoa(createdComment.ID))
	if err == nil {
		t.Error("Expected error when getting comment with wrong article")
	}

	// Test: Get comment with wrong author (but correct article) - should fail
	ctxWrongAuthor := ctxWithNestedMeta(testCommentMeta)
	ctxWrongAuthor = context.WithValue(ctxWrongAuthor, metadata.ParentIDsKey, map[string]string{
		"authorId":  "999", // non-existent author
		"articleId": itoa(article.ID),
	})
	_, err = commentWrapper.Get(ctxWrongAuthor, itoa(createdComment.ID))
	if err == nil {
		t.Error("Expected error when getting comment with wrong author")
	}
}

func TestWrapper_Nested_Update(t *testing.T) {
	db, cleanup := setupNestedTestDB(t)
	defer cleanup()

	// Create author and article
	authorWrapper := &datastore.Wrapper[TestAuthor]{Store: db}
	ctxAuthorMeta := ctxWithNestedMeta(testAuthorMeta)
	author, _ := authorWrapper.Create(ctxAuthorMeta, TestAuthor{Name: "Author", Email: "a@example.com"})

	ctxCreate := ctxWithNestedMeta(testArticleMeta)
	ctxCreate = context.WithValue(ctxCreate, metadata.ParentIDsKey, map[string]string{"authorId": itoa(author.ID)})
	articleWrapper := &datastore.Wrapper[TestArticle]{Store: db}
	article, err := articleWrapper.Create(ctxCreate, TestArticle{Title: "Original", Content: "Content"})
	if err != nil {
		t.Fatal("Failed to create article:", err)
	}

	// Test: Update article with correct parent context - should succeed
	ctx := ctxWithNestedMeta(testArticleMeta)
	ctx = context.WithValue(ctx, metadata.ParentIDsKey, map[string]string{
		"authorId": itoa(author.ID),
	})
	article.Title = "Updated"
	updated, err := articleWrapper.Update(ctx, itoa(article.ID), *article)
	if err != nil {
		t.Fatal("Failed to update article with correct parent:", err)
	}
	if updated.Title != "Updated" {
		t.Errorf("Expected title 'Updated', got '%s'", updated.Title)
	}

	// Test: Update article with wrong parent context - should fail
	ctxWrong := ctxWithNestedMeta(testArticleMeta)
	ctxWrong = context.WithValue(ctxWrong, metadata.ParentIDsKey, map[string]string{
		"authorId": "999",
	})
	article.Title = "Should Fail"
	_, err = articleWrapper.Update(ctxWrong, itoa(article.ID), *article)
	if err == nil {
		t.Error("Expected error when updating article with wrong parent")
	}
}

func TestWrapper_Nested_Delete(t *testing.T) {
	db, cleanup := setupNestedTestDB(t)
	defer cleanup()

	// Create author and article
	authorWrapper := &datastore.Wrapper[TestAuthor]{Store: db}
	ctxAuthorMeta := ctxWithNestedMeta(testAuthorMeta)
	author, _ := authorWrapper.Create(ctxAuthorMeta, TestAuthor{Name: "Author", Email: "a@example.com"})

	ctxCreate := ctxWithNestedMeta(testArticleMeta)
	ctxCreate = context.WithValue(ctxCreate, metadata.ParentIDsKey, map[string]string{"authorId": itoa(author.ID)})
	articleWrapper := &datastore.Wrapper[TestArticle]{Store: db}
	article, err := articleWrapper.Create(ctxCreate, TestArticle{Title: "To Delete", Content: "Content"})
	if err != nil {
		t.Fatal("Failed to create article:", err)
	}

	// Test: Delete article with wrong parent context - should fail
	ctxWrong := ctxWithNestedMeta(testArticleMeta)
	ctxWrong = context.WithValue(ctxWrong, metadata.ParentIDsKey, map[string]string{
		"authorId": "999",
	})
	err = articleWrapper.Delete(ctxWrong, itoa(article.ID))
	if err == nil {
		t.Error("Expected error when deleting article with wrong parent")
	}

	// Test: Delete article with correct parent context - should succeed
	ctx := ctxWithNestedMeta(testArticleMeta)
	ctx = context.WithValue(ctx, metadata.ParentIDsKey, map[string]string{
		"authorId": itoa(author.ID),
	})
	err = articleWrapper.Delete(ctx, itoa(article.ID))
	if err != nil {
		t.Fatal("Failed to delete article with correct parent:", err)
	}

	// Verify deletion
	_, err = articleWrapper.Get(ctx, itoa(article.ID))
	if err == nil {
		t.Error("Expected error when getting deleted article")
	}
}

// Custom join test models — Site -> UsageData joined on NMI (not FK)
type TestSite struct {
	bun.BaseModel `bun:"table:sites"`
	ID            int    `bun:"id,pk,autoincrement"`
	AccountID     int    `bun:"account_id,notnull"`
	NMI           string `bun:"nmi,notnull"`
}

type TestUsageData struct {
	bun.BaseModel `bun:"table:usage_data"`
	ID            int    `bun:"id,pk,autoincrement"`
	NMI           string `bun:"nmi,notnull"`
	Reading       int    `bun:"reading"`
}

var testSiteMeta = &metadata.TypeMetadata{
	TypeID:          "test_site_id",
	TypeName:        "TestSite",
	TableName:       "sites",
	URLParamUUID:    "siteId",
	PKField:         "ID",
	ModelType:       reflect.TypeOf(TestSite{}),
	ParentType:      nil,
	ParentMeta:      nil,
	ForeignKeyCol:   "",
	ParentJoinCol:   "id",
	ParentJoinField: "ID",
}

var testUsageDataMeta = &metadata.TypeMetadata{
	TypeID:          "test_usage_data_id",
	TypeName:        "TestUsageData",
	TableName:       "usage_data",
	URLParamUUID:    "usageDataId",
	PKField:         "ID",
	ModelType:       reflect.TypeOf(TestUsageData{}),
	ParentType:      reflect.TypeOf(TestSite{}),
	ParentMeta:      testSiteMeta,
	ForeignKeyCol:   "nmi",
	ParentJoinCol:   "nmi",
	ParentJoinField: "NMI",
}

func setupCustomJoinTestDB(t *testing.T) (*datastore.SQLite, func()) {
	t.Helper()

	db, err := datastore.NewSQLite(":memory:")
	if err != nil {
		t.Fatal("Failed to create test database:", err)
	}

	if err := datastore.Initialize(db); err != nil {
		db.Cleanup()
		t.Fatal("Failed to initialize datastore:", err)
	}

	ctx := context.Background()
	_, err = db.GetDB().NewCreateTable().Model((*TestSite)(nil)).IfNotExists().Exec(ctx)
	if err != nil {
		db.Cleanup()
		t.Fatal("Failed to create sites table:", err)
	}

	_, err = db.GetDB().NewCreateTable().Model((*TestUsageData)(nil)).IfNotExists().Exec(ctx)
	if err != nil {
		db.Cleanup()
		t.Fatal("Failed to create usage_data table:", err)
	}

	cleanup := func() {
		_, _ = db.GetDB().NewDropTable().Model((*TestUsageData)(nil)).IfExists().Exec(ctx)
		_, _ = db.GetDB().NewDropTable().Model((*TestSite)(nil)).IfExists().Exec(ctx)
		datastore.Cleanup()
		db.Cleanup()
	}

	return db, cleanup
}

func TestCustomJoin_ListFiltersByParentJoinCol(t *testing.T) {
	db, cleanup := setupCustomJoinTestDB(t)
	defer cleanup()

	// Create a site with NMI "ABC123"
	siteWrapper := &datastore.Wrapper[TestSite]{Store: db}
	ctxSite := ctxWithNestedMeta(testSiteMeta)
	site, err := siteWrapper.Create(ctxSite, TestSite{AccountID: 1, NMI: "ABC123"})
	if err != nil {
		t.Fatal("Failed to create site:", err)
	}

	// Insert usage data directly — some matching NMI, some not
	ctx := context.Background()
	_, _ = db.GetDB().NewInsert().Model(&TestUsageData{NMI: "ABC123", Reading: 100}).Exec(ctx)
	_, _ = db.GetDB().NewInsert().Model(&TestUsageData{NMI: "ABC123", Reading: 200}).Exec(ctx)
	_, _ = db.GetDB().NewInsert().Model(&TestUsageData{NMI: "XYZ999", Reading: 300}).Exec(ctx)

	// List usage data scoped to site — should only return NMI="ABC123" rows
	usageWrapper := &datastore.Wrapper[TestUsageData]{Store: db}
	ctxList := ctxWithNestedMeta(testUsageDataMeta)
	ctxList = context.WithValue(ctxList, metadata.ParentIDsKey, map[string]string{
		"siteId": itoa(site.ID),
	})
	results, _, _, err := usageWrapper.GetAll(ctxList)
	if err != nil {
		t.Fatal("Failed to list usage data:", err)
	}
	if len(results) != 2 {
		t.Errorf("Expected 2 usage records for NMI ABC123, got %d", len(results))
	}
	for _, r := range results {
		if r.NMI != "ABC123" {
			t.Errorf("Expected NMI 'ABC123', got '%s'", r.NMI)
		}
	}
}

func TestCustomJoin_GetValidatesParentJoinCol(t *testing.T) {
	db, cleanup := setupCustomJoinTestDB(t)
	defer cleanup()

	// Create two sites with different NMIs
	siteWrapper := &datastore.Wrapper[TestSite]{Store: db}
	ctxSite := ctxWithNestedMeta(testSiteMeta)
	site1, _ := siteWrapper.Create(ctxSite, TestSite{AccountID: 1, NMI: "ABC123"})
	site2, _ := siteWrapper.Create(ctxSite, TestSite{AccountID: 1, NMI: "XYZ999"})

	// Insert usage data for site1's NMI
	ctx := context.Background()
	usage := &TestUsageData{NMI: "ABC123", Reading: 100}
	_, _ = db.GetDB().NewInsert().Model(usage).Exec(ctx)

	usageWrapper := &datastore.Wrapper[TestUsageData]{Store: db}

	// Get with correct parent (site1, NMI=ABC123) — should succeed
	ctxCorrect := ctxWithNestedMeta(testUsageDataMeta)
	ctxCorrect = context.WithValue(ctxCorrect, metadata.ParentIDsKey, map[string]string{
		"siteId": itoa(site1.ID),
	})
	retrieved, err := usageWrapper.Get(ctxCorrect, itoa(usage.ID))
	if err != nil {
		t.Fatal("Failed to get usage data with correct parent:", err)
	}
	if retrieved.Reading != 100 {
		t.Errorf("Expected reading 100, got %d", retrieved.Reading)
	}

	// Get with wrong parent (site2, NMI=XYZ999) — should fail (404)
	ctxWrong := ctxWithNestedMeta(testUsageDataMeta)
	ctxWrong = context.WithValue(ctxWrong, metadata.ParentIDsKey, map[string]string{
		"siteId": itoa(site2.ID),
	})
	_, err = usageWrapper.Get(ctxWrong, itoa(usage.ID))
	if err == nil {
		t.Error("Expected error when getting usage data with wrong parent NMI")
	}
}

func TestCustomJoin_CreateSetsJoinCol(t *testing.T) {
	db, cleanup := setupCustomJoinTestDB(t)
	defer cleanup()

	// Create site
	siteWrapper := &datastore.Wrapper[TestSite]{Store: db}
	ctxSite := ctxWithNestedMeta(testSiteMeta)
	site, err := siteWrapper.Create(ctxSite, TestSite{AccountID: 1, NMI: "ABC123"})
	if err != nil {
		t.Fatal("Failed to create site:", err)
	}

	// Create usage data under the site — NMI should be auto-set
	usageWrapper := &datastore.Wrapper[TestUsageData]{Store: db}
	ctxCreate := ctxWithNestedMeta(testUsageDataMeta)
	ctxCreate = context.WithValue(ctxCreate, metadata.ParentIDsKey, map[string]string{
		"siteId": itoa(site.ID),
	})
	created, err := usageWrapper.Create(ctxCreate, TestUsageData{Reading: 500})
	if err != nil {
		t.Fatal("Failed to create usage data:", err)
	}
	if created.NMI != "ABC123" {
		t.Errorf("Expected NMI 'ABC123' auto-set from parent, got '%s'", created.NMI)
	}
}

func TestCustomJoin_MultipleParentsShareJoinCol(t *testing.T) {
	db, cleanup := setupCustomJoinTestDB(t)
	defer cleanup()

	// Two sites with the same NMI (different accounts, same physical meter)
	siteWrapper := &datastore.Wrapper[TestSite]{Store: db}
	ctxSite := ctxWithNestedMeta(testSiteMeta)
	site1, _ := siteWrapper.Create(ctxSite, TestSite{AccountID: 1, NMI: "SHARED_NMI"})
	site2, _ := siteWrapper.Create(ctxSite, TestSite{AccountID: 2, NMI: "SHARED_NMI"})

	// Insert usage data for the shared NMI
	ctx := context.Background()
	_, _ = db.GetDB().NewInsert().Model(&TestUsageData{NMI: "SHARED_NMI", Reading: 100}).Exec(ctx)
	_, _ = db.GetDB().NewInsert().Model(&TestUsageData{NMI: "SHARED_NMI", Reading: 200}).Exec(ctx)

	usageWrapper := &datastore.Wrapper[TestUsageData]{Store: db}

	// Both sites should see the same usage data
	ctxSite1 := ctxWithNestedMeta(testUsageDataMeta)
	ctxSite1 = context.WithValue(ctxSite1, metadata.ParentIDsKey, map[string]string{
		"siteId": itoa(site1.ID),
	})
	results1, _, _, err := usageWrapper.GetAll(ctxSite1)
	if err != nil {
		t.Fatal("Failed to list usage data for site1:", err)
	}

	ctxSite2 := ctxWithNestedMeta(testUsageDataMeta)
	ctxSite2 = context.WithValue(ctxSite2, metadata.ParentIDsKey, map[string]string{
		"siteId": itoa(site2.ID),
	})
	results2, _, _, err := usageWrapper.GetAll(ctxSite2)
	if err != nil {
		t.Fatal("Failed to list usage data for site2:", err)
	}

	if len(results1) != 2 {
		t.Errorf("Expected 2 usage records for site1, got %d", len(results1))
	}
	if len(results2) != 2 {
		t.Errorf("Expected 2 usage records for site2, got %d", len(results2))
	}
}

func TestCustomJoin_StandardFKUnchanged(t *testing.T) {
	db, cleanup := setupNestedTestDB(t)
	defer cleanup()

	// Standard FK flow: Author -> Article
	authorWrapper := &datastore.Wrapper[TestAuthor]{Store: db}
	ctxAuthor := ctxWithNestedMeta(testAuthorMeta)
	author, _ := authorWrapper.Create(ctxAuthor, TestAuthor{Name: "Author", Email: "a@example.com"})

	articleWrapper := &datastore.Wrapper[TestArticle]{Store: db}
	ctxCreate := ctxWithNestedMeta(testArticleMeta)
	ctxCreate = context.WithValue(ctxCreate, metadata.ParentIDsKey, map[string]string{
		"authorId": itoa(author.ID),
	})
	article, err := articleWrapper.Create(ctxCreate, TestArticle{Title: "Test", Content: "Content"})
	if err != nil {
		t.Fatal("Standard FK create failed:", err)
	}
	if article.AuthorID != author.ID {
		t.Errorf("Expected AuthorID %d, got %d", author.ID, article.AuthorID)
	}

	// Verify get still works
	ctxGet := ctxWithNestedMeta(testArticleMeta)
	ctxGet = context.WithValue(ctxGet, metadata.ParentIDsKey, map[string]string{
		"authorId": itoa(author.ID),
	})
	retrieved, err := articleWrapper.Get(ctxGet, itoa(article.ID))
	if err != nil {
		t.Fatal("Standard FK get failed:", err)
	}
	if retrieved.Title != "Test" {
		t.Errorf("Expected title 'Test', got '%s'", retrieved.Title)
	}
}
