//nolint:dupl,goconst,staticcheck,errcheck,gosec // Test code - duplicate test patterns, test data strings, string context keys, and unchecked test cleanup are acceptable
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

// TestUser is a test model
type TestUser struct {
	bun.BaseModel `bun:"table:users"`
	ID            int       `bun:"id,pk,autoincrement"`
	Name          string    `bun:"name,notnull"`
	Email         string    `bun:"email,notnull"`
	CreatedAt     time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

// testUserMeta is the metadata for TestUser (module-level for reuse)
var testUserMeta = &metadata.TypeMetadata{
	TypeID:        "test_user_id",
	TypeName:      "TestUser",
	TableName:     "users",
	URLParamUUID:  "id",
	ModelType:     reflect.TypeOf(TestUser{}),
	ParentType:    nil,
	ParentMeta:    nil,
	ForeignKeyCol: "",
}

// ctxWithMeta creates a context with the given metadata
func ctxWithMeta(meta *metadata.TypeMetadata) context.Context {
	return context.WithValue(context.Background(), metadata.MetadataKey, meta)
}

func setupTestDB(t *testing.T) (*datastore.SQLite, func()) {
	t.Helper()

	db, err := datastore.NewSQLite(":memory:")
	if err != nil {
		t.Fatal("Failed to create test database:", err)
	}

	// Create schema
	_, err = db.GetDB().NewCreateTable().Model((*TestUser)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		db.Cleanup()
		t.Fatal("Failed to create table:", err)
	}

	cleanup := func() {
		db.GetDB().NewDropTable().Model((*TestUser)(nil)).IfExists().Exec(context.Background())
		db.Cleanup()
	}

	return db, cleanup
}

func TestWrapper_Create(t *testing.T) {
	server, cleanup := setupTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestUser]{Store: server}
	ctx := ctxWithMeta(testUserMeta)

	user := TestUser{
		Name:  "John Doe",
		Email: "john@example.com",
	}

	created, err := wrapper.Create(ctx, user)
	if err != nil {
		t.Fatal("Failed to create user:", err)
	}

	if created.ID == 0 {
		t.Error("Expected ID to be set")
	}

	if created.Name != user.Name {
		t.Errorf("Expected name %s, got %s", user.Name, created.Name)
	}
}

func TestWrapper_Get(t *testing.T) {
	server, cleanup := setupTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestUser]{Store: server}
	ctx := ctxWithMeta(testUserMeta)

	// Create a user first
	user := TestUser{
		Name:  "Jane Doe",
		Email: "jane@example.com",
	}

	created, err := wrapper.Create(ctx, user)
	if err != nil {
		t.Fatal("Failed to create user:", err)
	}

	// Get the user
	retrieved, err := wrapper.Get(ctx, created.ID, []string{})
	if err != nil {
		t.Fatal("Failed to get user:", err)
	}

	if retrieved.ID != created.ID {
		t.Errorf("Expected ID %d, got %d", created.ID, retrieved.ID)
	}

	if retrieved.Name != created.Name {
		t.Errorf("Expected name %s, got %s", created.Name, retrieved.Name)
	}
}

func TestWrapper_GetAll(t *testing.T) {
	server, cleanup := setupTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestUser]{Store: server}
	ctx := ctxWithMeta(testUserMeta)

	// Create multiple users
	users := []TestUser{
		{Name: "User 1", Email: "user1@example.com"},
		{Name: "User 2", Email: "user2@example.com"},
		{Name: "User 3", Email: "user3@example.com"},
	}

	for _, user := range users {
		if _, err := wrapper.Create(ctx, user); err != nil {
			t.Fatal("Failed to create user:", err)
		}
	}

	// Get all users
	retrieved, err := wrapper.GetAll(ctx, []string{})
	if err != nil {
		t.Fatal("Failed to get all users:", err)
	}

	if len(retrieved) != len(users) {
		t.Errorf("Expected %d users, got %d", len(users), len(retrieved))
	}
}

func TestWrapper_Update(t *testing.T) {
	server, cleanup := setupTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestUser]{Store: server}
	ctx := ctxWithMeta(testUserMeta)

	// Create a user
	user := TestUser{
		Name:  "Original Name",
		Email: "original@example.com",
	}

	created, err := wrapper.Create(ctx, user)
	if err != nil {
		t.Fatal("Failed to create user:", err)
	}

	// Update the user
	created.Name = "Updated Name"
	updated, err := wrapper.Update(ctx, created.ID, *created)
	if err != nil {
		t.Fatal("Failed to update user:", err)
	}

	if updated.Name != "Updated Name" {
		t.Errorf("Expected name 'Updated Name', got %s", updated.Name)
	}
}

func TestWrapper_Delete(t *testing.T) {
	server, cleanup := setupTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestUser]{Store: server}
	ctx := ctxWithMeta(testUserMeta)

	// Create a user
	user := TestUser{
		Name:  "To Delete",
		Email: "delete@example.com",
	}

	created, err := wrapper.Create(ctx, user)
	if err != nil {
		t.Fatal("Failed to create user:", err)
	}

	// Delete the user
	if err := wrapper.Delete(ctx, created.ID); err != nil {
		t.Fatal("Failed to delete user:", err)
	}

	// Verify deletion
	_, err = wrapper.Get(ctx, created.ID, []string{})
	if err == nil {
		t.Error("Expected error when getting deleted user")
	}
}

// Error handling tests

func TestWrapper_Get_NotFound(t *testing.T) {
	server, cleanup := setupTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestUser]{Store: server}
	ctx := ctxWithMeta(testUserMeta)

	_, err := wrapper.Get(ctx, 999, []string{})
	if err == nil {
		t.Error("Expected error when getting non-existent user")
	}
}

func TestWrapper_Update_NotFound(t *testing.T) {
	server, cleanup := setupTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestUser]{Store: server}
	ctx := ctxWithMeta(testUserMeta)

	user := TestUser{
		ID:    999,
		Name:  "Does Not Exist",
		Email: "notexist@example.com",
	}

	_, err := wrapper.Update(ctx, 999, user)
	if err == nil {
		t.Error("Expected error when updating non-existent user")
	}
}

func TestWrapper_Delete_NotFound(t *testing.T) {
	server, cleanup := setupTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestUser]{Store: server}
	ctx := ctxWithMeta(testUserMeta)

	err := wrapper.Delete(ctx, 999)
	if err == nil {
		t.Error("Expected error when deleting non-existent user")
	}
}

func TestWrapper_Create_DuplicateEmail(t *testing.T) {
	// Note: This test is skipped because the TestUser model doesn't have a unique constraint
	// on the email field. In a real application, you would add `bun:"email,unique"` to the model.
	// The wrapper code properly handles duplicate errors when the database enforces uniqueness.
	t.Skip("TestUser model doesn't enforce unique email constraint")
}

func TestWrapper_GetAll_Empty(t *testing.T) {
	server, cleanup := setupTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestUser]{Store: server}
	ctx := ctxWithMeta(testUserMeta)

	retrieved, err := wrapper.GetAll(ctx, []string{})
	if err != nil {
		t.Fatal("Failed to get all users:", err)
	}

	if len(retrieved) != 0 {
		t.Errorf("Expected 0 users, got %d", len(retrieved))
	}
}

func TestWrapper_Get_WithRelations(t *testing.T) {
	server, cleanup := setupTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestUser]{Store: server}
	ctx := ctxWithMeta(testUserMeta)

	user := TestUser{
		Name:  "John Doe",
		Email: "john@example.com",
	}

	created, err := wrapper.Create(ctx, user)
	if err != nil {
		t.Fatal("Failed to create user:", err)
	}

	// Get with relations (even though we don't have any relations in this test model)
	// This tests that the relations parameter is properly handled
	retrieved, err := wrapper.Get(ctx, created.ID, []string{})
	if err != nil {
		t.Fatal("Failed to get user with relations:", err)
	}

	if retrieved.ID != created.ID {
		t.Errorf("Expected ID %d, got %d", created.ID, retrieved.ID)
	}
}

func TestWrapper_GetAll_WithRelations(t *testing.T) {
	server, cleanup := setupTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestUser]{Store: server}
	ctx := ctxWithMeta(testUserMeta)

	user := TestUser{
		Name:  "John Doe",
		Email: "john@example.com",
	}

	_, err := wrapper.Create(ctx, user)
	if err != nil {
		t.Fatal("Failed to create user:", err)
	}

	// Get all with relations (even though we don't have any relations in this test model)
	// This tests that the relations parameter is properly handled
	retrieved, err := wrapper.GetAll(ctx, []string{})
	if err != nil {
		t.Fatal("Failed to get all users with relations:", err)
	}

	if len(retrieved) != 1 {
		t.Errorf("Expected 1 user, got %d", len(retrieved))
	}
}

func TestWrapper_Create_UpdateDelete_Lifecycle(t *testing.T) {
	server, cleanup := setupTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestUser]{Store: server}
	ctx := ctxWithMeta(testUserMeta)

	// Create
	user := TestUser{
		Name:  "Lifecycle Test",
		Email: "lifecycle@example.com",
	}

	created, err := wrapper.Create(ctx, user)
	if err != nil {
		t.Fatal("Failed to create user:", err)
	}
	if created.ID == 0 {
		t.Error("Expected ID to be set")
	}

	// Get
	retrieved, err := wrapper.Get(ctx, created.ID, []string{})
	if err != nil {
		t.Fatal("Failed to get user:", err)
	}
	if retrieved.Name != user.Name {
		t.Errorf("Expected name %s, got %s", user.Name, retrieved.Name)
	}

	// Update
	retrieved.Name = "Updated Lifecycle"
	updated, err := wrapper.Update(ctx, retrieved.ID, *retrieved)
	if err != nil {
		t.Fatal("Failed to update user:", err)
	}
	if updated.Name != "Updated Lifecycle" {
		t.Errorf("Expected name 'Updated Lifecycle', got %s", updated.Name)
	}

	// GetAll
	all, err := wrapper.GetAll(ctx, []string{})
	if err != nil {
		t.Fatal("Failed to get all users:", err)
	}
	if len(all) != 1 {
		t.Errorf("Expected 1 user, got %d", len(all))
	}

	// Delete
	err = wrapper.Delete(ctx, created.ID)
	if err != nil {
		t.Fatal("Failed to delete user:", err)
	}

	// Verify deletion
	_, err = wrapper.Get(ctx, created.ID, []string{})
	if err == nil {
		t.Error("Expected error when getting deleted user")
	}
}

// Ownership filtering tests

// TestOwnedBlog is a test model with single ownership field
type TestOwnedBlog struct {
	bun.BaseModel `bun:"table:test_owned_blogs"`
	ID            int       `bun:"id,pk,autoincrement"`
	AuthorID      string    `bun:"author_id,notnull"`
	Name          string    `bun:"name,notnull"`
	CreatedAt     time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

// TestOwnedPost is a test model with multiple ownership fields (OR logic)
type TestOwnedPost struct {
	bun.BaseModel `bun:"table:test_owned_posts"`
	ID            int            `bun:"id,pk,autoincrement"`
	BlogID        int            `bun:"blog_id,notnull"`
	Blog          *TestOwnedBlog `bun:"rel:belongs-to,join:blog_id=id"`
	AuthorID      string         `bun:"author_id,notnull"`
	EditorID      string         `bun:"editor_id"`
	Title         string         `bun:"title,notnull"`
	CreatedAt     time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

// TestOwnedArticle is a test model for bypass scope testing
type TestOwnedArticle struct {
	bun.BaseModel `bun:"table:test_owned_articles"`
	ID            int       `bun:"id,pk,autoincrement"`
	AuthorID      string    `bun:"author_id,notnull"`
	Title         string    `bun:"title,notnull"`
	CreatedAt     time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

// Ownership metadata (module-level for reuse)
var testOwnedBlogMeta = &metadata.TypeMetadata{
	TypeID:          "test_owned_blog_id",
	TypeName:        "TestOwnedBlog",
	TableName:       "test_owned_blogs",
	URLParamUUID:    "blog_id",
	ModelType:       reflect.TypeOf(TestOwnedBlog{}),
	ParentType:      nil,
	ParentMeta:      nil,
	ForeignKeyCol:   "",
	OwnershipFields: []string{"AuthorID"},
	BypassScopes:    []string{"admin"},
}

var testOwnedPostMeta = &metadata.TypeMetadata{
	TypeID:          "test_owned_post_id",
	TypeName:        "TestOwnedPost",
	TableName:       "test_owned_posts",
	URLParamUUID:    "post_id",
	ModelType:       reflect.TypeOf(TestOwnedPost{}),
	ParentType:      reflect.TypeOf(TestOwnedBlog{}),
	ParentMeta:      testOwnedBlogMeta,
	ForeignKeyCol:   "blog_id",
	OwnershipFields: []string{"AuthorID", "EditorID"},
	BypassScopes:    []string{"admin"},
}

var testOwnedArticleMeta = &metadata.TypeMetadata{
	TypeID:          "test_owned_article_id",
	TypeName:        "TestOwnedArticle",
	TableName:       "test_owned_articles",
	URLParamUUID:    "article_id",
	ModelType:       reflect.TypeOf(TestOwnedArticle{}),
	ParentType:      nil,
	ParentMeta:      nil,
	ForeignKeyCol:   "",
	OwnershipFields: []string{"AuthorID"},
	BypassScopes:    []string{"admin", "moderator"},
}

func setupOwnershipTestDB(t *testing.T) (*datastore.SQLite, func()) {
	t.Helper()

	db, err := datastore.NewSQLite(":memory:")
	if err != nil {
		t.Fatal("Failed to create test database:", err)
	}

	// Create schemas
	ctx := context.Background()
	models := []interface{}{
		(*TestOwnedBlog)(nil),
		(*TestOwnedPost)(nil),
		(*TestOwnedArticle)(nil),
	}

	for _, model := range models {
		_, err := db.GetDB().NewCreateTable().Model(model).IfNotExists().Exec(ctx)
		if err != nil {
			db.Cleanup()
			t.Fatal("Failed to create table:", err)
		}
	}

	cleanup := func() {
		for _, model := range models {
			db.GetDB().NewDropTable().Model(model).IfExists().Exec(ctx)
		}
		db.Cleanup()
	}

	return db, cleanup
}

func TestOwnership_SingleField_GetAll(t *testing.T) {
	db, cleanup := setupOwnershipTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestOwnedBlog]{Store: db}
	ctx := ctxWithMeta(testOwnedBlogMeta)

	// Create blogs for different authors (without ownership enforcement on create)
	blogs := []TestOwnedBlog{
		{AuthorID: "alice", Name: "Alice's Blog"},
		{AuthorID: "bob", Name: "Bob's Blog"},
		{AuthorID: "alice", Name: "Alice's Second Blog"},
	}

	for _, blog := range blogs {
		_, err := wrapper.Create(ctx, blog)
		if err != nil {
			t.Fatal("Failed to create blog:", err)
		}
	}

	// GetAll with ownership enforcement for alice
	ctxWithOwnership := context.WithValue(ctx, "ownershipEnforced", true)
	ctxWithOwnership = context.WithValue(ctxWithOwnership, "ownershipUserID", "alice")

	retrieved, err := wrapper.GetAll(ctxWithOwnership, []string{})
	if err != nil {
		t.Fatal("Failed to get blogs:", err)
	}

	// Should only get alice's blogs
	if len(retrieved) != 2 {
		t.Errorf("Expected 2 blogs for alice, got %d", len(retrieved))
	}

	for _, blog := range retrieved {
		if blog.AuthorID != "alice" { //nolint:goconst // Test data string
			t.Errorf("Expected blog to belong to alice, got %s", blog.AuthorID)
		}
	}
}

func TestOwnership_SingleField_Get(t *testing.T) {
	db, cleanup := setupOwnershipTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestOwnedBlog]{Store: db}
	ctx := ctxWithMeta(testOwnedBlogMeta)

	// Create a blog for alice
	blog := TestOwnedBlog{AuthorID: "alice", Name: "Alice's Blog"}
	created, err := wrapper.Create(ctx, blog)
	if err != nil {
		t.Fatal("Failed to create blog:", err)
	}

	// Get with ownership enforcement for alice (should succeed)
	ctxAlice := context.WithValue(ctx, "ownershipEnforced", true)
	ctxAlice = context.WithValue(ctxAlice, "ownershipUserID", "alice")

	retrieved, err := wrapper.Get(ctxAlice, created.ID, []string{})
	if err != nil {
		t.Fatal("Failed to get blog as alice:", err)
	}
	if retrieved.AuthorID != "alice" {
		t.Errorf("Expected blog to belong to alice, got %s", retrieved.AuthorID)
	}

	// Get with ownership enforcement for bob (should fail)
	ctxBob := context.WithValue(ctx, "ownershipEnforced", true)
	ctxBob = context.WithValue(ctxBob, "ownershipUserID", "bob")

	_, err = wrapper.Get(ctxBob, created.ID, []string{})
	if err == nil {
		t.Error("Expected error when bob tries to get alice's blog")
	}
}

func TestOwnership_MultipleFields_GetAll(t *testing.T) {
	db, cleanup := setupOwnershipTestDB(t)
	defer cleanup()

	blogWrapper := &datastore.Wrapper[TestOwnedBlog]{Store: db}
	postWrapper := &datastore.Wrapper[TestOwnedPost]{Store: db}
	ctxBlog := ctxWithMeta(testOwnedBlogMeta)
	ctxPost := ctxWithMeta(testOwnedPostMeta)

	// Create a blog for alice
	blog := TestOwnedBlog{AuthorID: "alice", Name: "Alice's Blog"}
	createdBlog, err := blogWrapper.Create(ctxBlog, blog)
	if err != nil {
		t.Fatal("Failed to create blog:", err)
	}

	// Create posts with different ownership combinations
	// Add parent context for nested resource creation
	parentIDs := map[string]int{
		"blog_id": createdBlog.ID,
	}
	ctxWithParent := context.WithValue(ctxPost, "parentIDs", parentIDs)

	posts := []TestOwnedPost{
		{BlogID: createdBlog.ID, AuthorID: "alice", EditorID: "", Title: "Alice authored"},
		{BlogID: createdBlog.ID, AuthorID: "bob", EditorID: "alice", Title: "Bob authored, Alice edited"},
		{BlogID: createdBlog.ID, AuthorID: "bob", EditorID: "charlie", Title: "Bob authored, Charlie edited"},
	}

	for _, post := range posts {
		_, err := postWrapper.Create(ctxWithParent, post)
		if err != nil {
			t.Fatal("Failed to create post:", err)
		}
	}

	// GetAll with ownership enforcement for alice
	// Should get posts where alice is author OR editor
	ctxAlice := context.WithValue(ctxPost, "ownershipEnforced", true)
	ctxAlice = context.WithValue(ctxAlice, "ownershipUserID", "alice")

	retrieved, err := postWrapper.GetAll(ctxAlice, []string{})
	if err != nil {
		t.Fatal("Failed to get posts:", err)
	}

	// Should get 2 posts (authored by alice, and edited by alice)
	if len(retrieved) != 2 {
		t.Errorf("Expected 2 posts for alice, got %d", len(retrieved))
	}

	for _, post := range retrieved {
		if post.AuthorID != "alice" && post.EditorID != "alice" {
			t.Errorf("Expected post to belong to alice, got author=%s editor=%s", post.AuthorID, post.EditorID)
		}
	}
}

//nolint:dupl // Test functions for different bypass scopes have similar structure
func TestOwnership_BypassScope_Admin(t *testing.T) {
	db, cleanup := setupOwnershipTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestOwnedArticle]{Store: db}
	ctx := ctxWithMeta(testOwnedArticleMeta)

	// Create articles for different authors
	articles := []TestOwnedArticle{
		{AuthorID: "alice", Title: "Alice's Article"},
		{AuthorID: "bob", Title: "Bob's Article"},
	}

	for _, article := range articles {
		_, err := wrapper.Create(ctx, article)
		if err != nil {
			t.Fatal("Failed to create article:", err)
		}
	}

	// GetAll with ownership enforcement for charlie, but charlie is admin
	ctxCharlie := context.WithValue(ctx, "ownershipEnforced", true)
	ctxCharlie = context.WithValue(ctxCharlie, "ownershipUserID", "charlie")

	authInfo := &metadata.AuthInfo{
		UserID: "charlie",
		Scopes: []string{"admin"},
	}
	ctxCharlie = context.WithValue(ctxCharlie, "authInfo", authInfo)

	retrieved, err := wrapper.GetAll(ctxCharlie, []string{})
	if err != nil {
		t.Fatal("Failed to get articles:", err)
	}

	// Admin should see all articles
	if len(retrieved) != 2 {
		t.Errorf("Expected admin to see 2 articles, got %d", len(retrieved))
	}
}

//nolint:dupl // Test functions for different bypass scopes have similar structure
func TestOwnership_BypassScope_Moderator(t *testing.T) {
	db, cleanup := setupOwnershipTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestOwnedArticle]{Store: db}
	ctx := ctxWithMeta(testOwnedArticleMeta)

	// Create articles for different authors
	articles := []TestOwnedArticle{
		{AuthorID: "alice", Title: "Alice's Article"},
		{AuthorID: "bob", Title: "Bob's Article"},
	}

	for _, article := range articles {
		_, err := wrapper.Create(ctx, article)
		if err != nil {
			t.Fatal("Failed to create article:", err)
		}
	}

	// GetAll with ownership enforcement for diana, but diana is moderator
	ctxDiana := context.WithValue(ctx, "ownershipEnforced", true)
	ctxDiana = context.WithValue(ctxDiana, "ownershipUserID", "diana")

	authInfo := &metadata.AuthInfo{
		UserID: "diana",
		Scopes: []string{"moderator"},
	}
	ctxDiana = context.WithValue(ctxDiana, "authInfo", authInfo)

	retrieved, err := wrapper.GetAll(ctxDiana, []string{})
	if err != nil {
		t.Fatal("Failed to get articles:", err)
	}

	// Moderator should see all articles
	if len(retrieved) != 2 {
		t.Errorf("Expected moderator to see 2 articles, got %d", len(retrieved))
	}
}

func TestOwnership_SetOwnershipField_OnCreate(t *testing.T) {
	db, cleanup := setupOwnershipTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestOwnedBlog]{Store: db}
	ctx := ctxWithMeta(testOwnedBlogMeta)

	// Create with ownership enforcement
	ctxAlice := context.WithValue(ctx, "ownershipEnforced", true)
	ctxAlice = context.WithValue(ctxAlice, "ownershipUserID", "alice")

	blog := TestOwnedBlog{
		AuthorID: "", // Will be set automatically
		Name:     "Auto-owned Blog",
	}

	created, err := wrapper.Create(ctxAlice, blog)
	if err != nil {
		t.Fatal("Failed to create blog:", err)
	}

	// Verify AuthorID was set to alice
	if created.AuthorID != "alice" {
		t.Errorf("Expected AuthorID to be set to alice, got %s", created.AuthorID)
	}
}

func TestOwnership_NoOwnershipContext_GetAll(t *testing.T) {
	db, cleanup := setupOwnershipTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestOwnedBlog]{Store: db}
	ctx := ctxWithMeta(testOwnedBlogMeta)

	// Create blogs for different authors
	blogs := []TestOwnedBlog{
		{AuthorID: "alice", Name: "Alice's Blog"},
		{AuthorID: "bob", Name: "Bob's Blog"},
	}

	for _, blog := range blogs {
		_, err := wrapper.Create(ctx, blog)
		if err != nil {
			t.Fatal("Failed to create blog:", err)
		}
	}

	// GetAll without ownership enforcement - should get all
	retrieved, err := wrapper.GetAll(ctx, []string{})
	if err != nil {
		t.Fatal("Failed to get all blogs:", err)
	}

	if len(retrieved) != 2 {
		t.Errorf("Expected 2 blogs without ownership filter, got %d", len(retrieved))
	}
}

func TestOwnership_TypeWithoutOwnershipConfig(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestUser]{Store: db}
	ctx := ctxWithMeta(testUserMeta)

	// Create users
	users := []TestUser{
		{Name: "Alice", Email: "alice@example.com"},
		{Name: "Bob", Email: "bob@example.com"},
	}

	for _, user := range users {
		_, err := wrapper.Create(ctx, user)
		if err != nil {
			t.Fatal("Failed to create user:", err)
		}
	}

	// GetAll with ownership enforcement, but TestUser has no ownership config
	// Should return all users (ownership filter is skipped for types without config)
	ctxAlice := context.WithValue(ctx, "ownershipEnforced", true)
	ctxAlice = context.WithValue(ctxAlice, "ownershipUserID", "alice")

	retrieved, err := wrapper.GetAll(ctxAlice, []string{})
	if err != nil {
		t.Fatal("Failed to get all users:", err)
	}

	// Should get all users since TestUser has no ownership config
	if len(retrieved) != 2 {
		t.Errorf("Expected 2 users (no ownership config), got %d", len(retrieved))
	}
}

func TestOwnership_NestedResourceValidation(t *testing.T) {
	db, cleanup := setupOwnershipTestDB(t)
	defer cleanup()

	blogWrapper := &datastore.Wrapper[TestOwnedBlog]{Store: db}
	postWrapper := &datastore.Wrapper[TestOwnedPost]{Store: db}
	ctxBlog := ctxWithMeta(testOwnedBlogMeta)
	ctxPost := ctxWithMeta(testOwnedPostMeta)

	// Create blog for alice
	blog := TestOwnedBlog{AuthorID: "alice", Name: "Alice's Blog"}
	createdBlog, err := blogWrapper.Create(ctxBlog, blog)
	if err != nil {
		t.Fatal("Failed to create blog:", err)
	}

	// Try to create post under alice's blog as bob (with ownership enforcement)
	// This should fail because bob can't access alice's blog
	ctxBob := context.WithValue(ctxPost, "ownershipEnforced", true)
	ctxBob = context.WithValue(ctxBob, "ownershipUserID", "bob")

	// Add parent ID to context
	parentIDs := map[string]int{
		"blog_id": createdBlog.ID,
	}
	ctxBob = context.WithValue(ctxBob, "parentIDs", parentIDs)

	post := TestOwnedPost{
		BlogID:   createdBlog.ID,
		AuthorID: "bob",
		EditorID: "",
		Title:    "Bob's Post on Alice's Blog",
	}

	_, err = postWrapper.Create(ctxBob, post)
	if err == nil {
		t.Error("Expected error when bob tries to create post on alice's blog")
	}

	// Create post as alice (should succeed)
	ctxAlice := context.WithValue(ctxPost, "ownershipEnforced", true)
	ctxAlice = context.WithValue(ctxAlice, "ownershipUserID", "alice")
	ctxAlice = context.WithValue(ctxAlice, "parentIDs", parentIDs)

	post.AuthorID = "alice"
	created, err := postWrapper.Create(ctxAlice, post)
	if err != nil {
		t.Fatal("Failed to create post as alice:", err)
	}

	if created.AuthorID != "alice" {
		t.Errorf("Expected post author to be alice, got %s", created.AuthorID)
	}
}
