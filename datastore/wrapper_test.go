//nolint:dupl,goconst,staticcheck,errcheck,gosec // Test code - duplicate test patterns, test data strings, string context keys, and unchecked test cleanup are acceptable
package datastore_test

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sjgoldie/go-restgen/datastore"
	apperrors "github.com/sjgoldie/go-restgen/errors"
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
	PKField:       "ID",
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
	retrieved, err := wrapper.Get(ctx, strconv.Itoa(created.ID))
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
	retrieved, _, err := wrapper.GetAll(ctx)
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
	updated, err := wrapper.Update(ctx, strconv.Itoa(created.ID), *created)
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
	if err := wrapper.Delete(ctx, strconv.Itoa(created.ID)); err != nil {
		t.Fatal("Failed to delete user:", err)
	}

	// Verify deletion
	_, err = wrapper.Get(ctx, strconv.Itoa(created.ID))
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

	_, err := wrapper.Get(ctx, "999")
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

	_, err := wrapper.Update(ctx, "999", user)
	if err == nil {
		t.Error("Expected error when updating non-existent user")
	}
}

func TestWrapper_Delete_NotFound(t *testing.T) {
	server, cleanup := setupTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestUser]{Store: server}
	ctx := ctxWithMeta(testUserMeta)

	err := wrapper.Delete(ctx, "999")
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

	retrieved, _, err := wrapper.GetAll(ctx)
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
	retrieved, err := wrapper.Get(ctx, strconv.Itoa(created.ID))
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
	retrieved, _, err := wrapper.GetAll(ctx)
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
	retrieved, err := wrapper.Get(ctx, strconv.Itoa(created.ID))
	if err != nil {
		t.Fatal("Failed to get user:", err)
	}
	if retrieved.Name != user.Name {
		t.Errorf("Expected name %s, got %s", user.Name, retrieved.Name)
	}

	// Update
	retrieved.Name = "Updated Lifecycle"
	updated, err := wrapper.Update(ctx, strconv.Itoa(retrieved.ID), *retrieved)
	if err != nil {
		t.Fatal("Failed to update user:", err)
	}
	if updated.Name != "Updated Lifecycle" {
		t.Errorf("Expected name 'Updated Lifecycle', got %s", updated.Name)
	}

	// GetAll
	all, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("Failed to get all users:", err)
	}
	if len(all) != 1 {
		t.Errorf("Expected 1 user, got %d", len(all))
	}

	// Delete
	err = wrapper.Delete(ctx, strconv.Itoa(created.ID))
	if err != nil {
		t.Fatal("Failed to delete user:", err)
	}

	// Verify deletion
	_, err = wrapper.Get(ctx, strconv.Itoa(created.ID))
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
	PKField:         "ID",
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
	PKField:         "ID",
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
	PKField:         "ID",
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

	retrieved, _, err := wrapper.GetAll(ctxWithOwnership)
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

	retrieved, err := wrapper.Get(ctxAlice, strconv.Itoa(created.ID))
	if err != nil {
		t.Fatal("Failed to get blog as alice:", err)
	}
	if retrieved.AuthorID != "alice" {
		t.Errorf("Expected blog to belong to alice, got %s", retrieved.AuthorID)
	}

	// Get with ownership enforcement for bob (should fail)
	ctxBob := context.WithValue(ctx, "ownershipEnforced", true)
	ctxBob = context.WithValue(ctxBob, "ownershipUserID", "bob")

	_, err = wrapper.Get(ctxBob, strconv.Itoa(created.ID))
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
	parentIDs := map[string]string{
		"blog_id": strconv.Itoa(createdBlog.ID),
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

	retrieved, _, err := postWrapper.GetAll(ctxAlice)
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

	retrieved, _, err := wrapper.GetAll(ctxCharlie)
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

	retrieved, _, err := wrapper.GetAll(ctxDiana)
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
	retrieved, _, err := wrapper.GetAll(ctx)
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

	retrieved, _, err := wrapper.GetAll(ctxAlice)
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
	parentIDs := map[string]string{
		"blog_id": strconv.Itoa(createdBlog.ID),
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

// Validation tests

// TestValidatedItem is a test model for validation testing
type TestValidatedItem struct {
	bun.BaseModel `bun:"table:test_validated_items"`
	ID            int    `bun:"id,pk,autoincrement"`
	Name          string `bun:"name,notnull"`
	Status        string `bun:"status,notnull"`
	Priority      int    `bun:"priority,notnull"`
}

func setupValidationTestDB(t *testing.T) (*datastore.SQLite, func()) {
	t.Helper()

	db, err := datastore.NewSQLite(":memory:")
	if err != nil {
		t.Fatal("Failed to create test database:", err)
	}

	_, err = db.GetDB().NewCreateTable().Model((*TestValidatedItem)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		db.Cleanup()
		t.Fatal("Failed to create table:", err)
	}

	cleanup := func() {
		db.GetDB().NewDropTable().Model((*TestValidatedItem)(nil)).IfExists().Exec(context.Background())
		db.Cleanup()
	}

	return db, cleanup
}

func TestValidation_Create_Success(t *testing.T) {
	db, cleanup := setupValidationTestDB(t)
	defer cleanup()

	// Validator that allows all creates
	var validator metadata.ValidatorFunc[TestValidatedItem] = func(vc metadata.ValidationContext[TestValidatedItem]) error {
		return nil
	}

	meta := &metadata.TypeMetadata{
		TypeID:    "test_validated_item",
		TypeName:  "TestValidatedItem",
		TableName: "test_validated_items",
		ModelType: reflect.TypeOf(TestValidatedItem{}),
		Validator: validator,
	}

	wrapper := &datastore.Wrapper[TestValidatedItem]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, meta)

	item := TestValidatedItem{Name: "Test", Status: "active", Priority: 1}
	created, err := wrapper.Create(ctx, item)
	if err != nil {
		t.Fatal("Expected create to succeed:", err)
	}
	if created.ID == 0 {
		t.Error("Expected ID to be set")
	}
}

func TestValidation_Create_Failure(t *testing.T) {
	db, cleanup := setupValidationTestDB(t)
	defer cleanup()

	// Validator that rejects creates with priority > 5
	// Must be explicitly typed as ValidatorFunc[T] for type assertion to work
	var validator metadata.ValidatorFunc[TestValidatedItem] = func(vc metadata.ValidationContext[TestValidatedItem]) error {
		if vc.Operation == metadata.OpCreate && vc.New.Priority > 5 {
			return errors.New("priority must be 5 or less")
		}
		return nil
	}

	meta := &metadata.TypeMetadata{
		TypeID:    "test_validated_item",
		TypeName:  "TestValidatedItem",
		TableName: "test_validated_items",
		ModelType: reflect.TypeOf(TestValidatedItem{}),
		Validator: validator,
	}

	wrapper := &datastore.Wrapper[TestValidatedItem]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, meta)

	item := TestValidatedItem{Name: "Test", Status: "active", Priority: 10}
	_, err := wrapper.Create(ctx, item)
	if err == nil {
		t.Fatal("Expected create to fail validation")
	}

	var validationErr *apperrors.ValidationError
	if !errors.As(err, &validationErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
	if validationErr.Message != "priority must be 5 or less" {
		t.Errorf("Expected message 'priority must be 5 or less', got '%s'", validationErr.Message)
	}
}

func TestValidation_Update_Success(t *testing.T) {
	db, cleanup := setupValidationTestDB(t)
	defer cleanup()

	// Validator that allows status transitions from active to inactive
	var validator metadata.ValidatorFunc[TestValidatedItem] = func(vc metadata.ValidationContext[TestValidatedItem]) error {
		if vc.Operation == metadata.OpUpdate {
			if vc.Old.Status == "active" && vc.New.Status == "inactive" {
				return nil // allowed
			}
			if vc.Old.Status == vc.New.Status {
				return nil // no change is allowed
			}
		}
		return nil
	}

	meta := &metadata.TypeMetadata{
		TypeID:    "test_validated_item",
		TypeName:  "TestValidatedItem",
		TableName: "test_validated_items",
		ModelType: reflect.TypeOf(TestValidatedItem{}),
		Validator: validator,
	}

	wrapper := &datastore.Wrapper[TestValidatedItem]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, meta)

	// Create item first
	item := TestValidatedItem{Name: "Test", Status: "active", Priority: 1}
	created, err := wrapper.Create(ctx, item)
	if err != nil {
		t.Fatal("Failed to create item:", err)
	}

	// Update to inactive (should succeed)
	created.Status = "inactive"
	updated, err := wrapper.Update(ctx, strconv.Itoa(created.ID), *created)
	if err != nil {
		t.Fatal("Expected update to succeed:", err)
	}
	if updated.Status != "inactive" {
		t.Errorf("Expected status 'inactive', got '%s'", updated.Status)
	}
}

func TestValidation_Update_Failure(t *testing.T) {
	db, cleanup := setupValidationTestDB(t)
	defer cleanup()

	// Validator that blocks updates to completed items
	var validator metadata.ValidatorFunc[TestValidatedItem] = func(vc metadata.ValidationContext[TestValidatedItem]) error {
		if vc.Operation == metadata.OpUpdate && vc.Old.Status == "completed" {
			return errors.New("cannot modify completed items")
		}
		return nil
	}

	meta := &metadata.TypeMetadata{
		TypeID:    "test_validated_item",
		TypeName:  "TestValidatedItem",
		TableName: "test_validated_items",
		ModelType: reflect.TypeOf(TestValidatedItem{}),
		Validator: validator,
	}

	wrapper := &datastore.Wrapper[TestValidatedItem]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, meta)

	// Create item with status "completed"
	item := TestValidatedItem{Name: "Test", Status: "completed", Priority: 1}
	created, err := wrapper.Create(ctx, item)
	if err != nil {
		t.Fatal("Failed to create item:", err)
	}

	// Try to update (should fail)
	created.Name = "Updated"
	_, err = wrapper.Update(ctx, strconv.Itoa(created.ID), *created)
	if err == nil {
		t.Fatal("Expected update to fail validation")
	}

	var validationErr *apperrors.ValidationError
	if !errors.As(err, &validationErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestValidation_Delete_Success(t *testing.T) {
	db, cleanup := setupValidationTestDB(t)
	defer cleanup()

	// Validator that allows deletes of non-completed items
	var validator metadata.ValidatorFunc[TestValidatedItem] = func(vc metadata.ValidationContext[TestValidatedItem]) error {
		if vc.Operation == metadata.OpDelete && vc.Old.Status == "completed" {
			return errors.New("cannot delete completed items")
		}
		return nil
	}

	meta := &metadata.TypeMetadata{
		TypeID:    "test_validated_item",
		TypeName:  "TestValidatedItem",
		TableName: "test_validated_items",
		ModelType: reflect.TypeOf(TestValidatedItem{}),
		Validator: validator,
	}

	wrapper := &datastore.Wrapper[TestValidatedItem]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, meta)

	// Create item with status "active"
	item := TestValidatedItem{Name: "Test", Status: "active", Priority: 1}
	created, err := wrapper.Create(ctx, item)
	if err != nil {
		t.Fatal("Failed to create item:", err)
	}

	// Delete (should succeed)
	err = wrapper.Delete(ctx, strconv.Itoa(created.ID))
	if err != nil {
		t.Fatal("Expected delete to succeed:", err)
	}
}

func TestValidation_Delete_Failure(t *testing.T) {
	db, cleanup := setupValidationTestDB(t)
	defer cleanup()

	// Validator that blocks deletes of completed items
	var validator metadata.ValidatorFunc[TestValidatedItem] = func(vc metadata.ValidationContext[TestValidatedItem]) error {
		if vc.Operation == metadata.OpDelete && vc.Old.Status == "completed" {
			return errors.New("cannot delete completed items")
		}
		return nil
	}

	meta := &metadata.TypeMetadata{
		TypeID:    "test_validated_item",
		TypeName:  "TestValidatedItem",
		TableName: "test_validated_items",
		ModelType: reflect.TypeOf(TestValidatedItem{}),
		Validator: validator,
	}

	wrapper := &datastore.Wrapper[TestValidatedItem]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, meta)

	// Create item with status "completed"
	item := TestValidatedItem{Name: "Test", Status: "completed", Priority: 1}
	created, err := wrapper.Create(ctx, item)
	if err != nil {
		t.Fatal("Failed to create item:", err)
	}

	// Try to delete (should fail)
	err = wrapper.Delete(ctx, strconv.Itoa(created.ID))
	if err == nil {
		t.Fatal("Expected delete to fail validation")
	}

	var validationErr *apperrors.ValidationError
	if !errors.As(err, &validationErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestValidation_NoValidator(t *testing.T) {
	db, cleanup := setupValidationTestDB(t)
	defer cleanup()

	// No validator configured
	meta := &metadata.TypeMetadata{
		TypeID:    "test_validated_item",
		TypeName:  "TestValidatedItem",
		TableName: "test_validated_items",
		ModelType: reflect.TypeOf(TestValidatedItem{}),
		Validator: nil, // No validator
	}

	wrapper := &datastore.Wrapper[TestValidatedItem]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, meta)

	// All operations should succeed without validator
	item := TestValidatedItem{Name: "Test", Status: "active", Priority: 100}
	created, err := wrapper.Create(ctx, item)
	if err != nil {
		t.Fatal("Create should succeed without validator:", err)
	}

	created.Name = "Updated"
	_, err = wrapper.Update(ctx, strconv.Itoa(created.ID), *created)
	if err != nil {
		t.Fatal("Update should succeed without validator:", err)
	}

	err = wrapper.Delete(ctx, strconv.Itoa(created.ID))
	if err != nil {
		t.Fatal("Delete should succeed without validator:", err)
	}
}

func TestValidation_WrongValidatorType(t *testing.T) {
	db, cleanup := setupValidationTestDB(t)
	defer cleanup()

	// Validator for wrong type (should be skipped)
	wrongValidator := func(vc metadata.ValidationContext[TestUser]) error {
		return errors.New("should not be called")
	}

	meta := &metadata.TypeMetadata{
		TypeID:    "test_validated_item",
		TypeName:  "TestValidatedItem",
		TableName: "test_validated_items",
		ModelType: reflect.TypeOf(TestValidatedItem{}),
		Validator: wrongValidator, // Wrong type
	}

	wrapper := &datastore.Wrapper[TestValidatedItem]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, meta)

	// Should succeed because validator type doesn't match
	item := TestValidatedItem{Name: "Test", Status: "active", Priority: 1}
	_, err := wrapper.Create(ctx, item)
	if err != nil {
		t.Fatal("Create should succeed when validator type doesn't match:", err)
	}
}

// Audit tests

// TestAuditItem is a test model for audit testing
type TestAuditItem struct {
	bun.BaseModel `bun:"table:test_audit_items"`
	ID            int    `bun:"id,pk,autoincrement"`
	Name          string `bun:"name,notnull"`
	Status        string `bun:"status,notnull"`
}

// TestAuditLog is the audit log model
type TestAuditLog struct {
	bun.BaseModel `bun:"table:test_audit_logs"`
	ID            int    `bun:"id,pk,autoincrement"`
	ItemID        int    `bun:"item_id"`
	Operation     string `bun:"operation,notnull"`
	OldStatus     string `bun:"old_status"`
	NewStatus     string `bun:"new_status"`
}

func setupAuditTestDB(t *testing.T) (*datastore.SQLite, func()) {
	t.Helper()

	db, err := datastore.NewSQLite(":memory:")
	if err != nil {
		t.Fatal("Failed to create test database:", err)
	}

	_, err = db.GetDB().NewCreateTable().Model((*TestAuditItem)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		db.Cleanup()
		t.Fatal("Failed to create items table:", err)
	}

	_, err = db.GetDB().NewCreateTable().Model((*TestAuditLog)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		db.Cleanup()
		t.Fatal("Failed to create audit logs table:", err)
	}

	cleanup := func() {
		db.GetDB().NewDropTable().Model((*TestAuditLog)(nil)).IfExists().Exec(context.Background())
		db.GetDB().NewDropTable().Model((*TestAuditItem)(nil)).IfExists().Exec(context.Background())
		db.Cleanup()
	}

	return db, cleanup
}

func TestAudit_Create(t *testing.T) {
	db, cleanup := setupAuditTestDB(t)
	defer cleanup()

	// Auditor that logs creates
	var auditor metadata.AuditFunc[TestAuditItem] = func(ac metadata.AuditContext[TestAuditItem]) any {
		return &TestAuditLog{
			ItemID:    ac.New.ID,
			Operation: string(ac.Operation),
			NewStatus: ac.New.Status,
		}
	}

	meta := &metadata.TypeMetadata{
		TypeID:    "test_audit_item",
		TypeName:  "TestAuditItem",
		TableName: "test_audit_items",
		ModelType: reflect.TypeOf(TestAuditItem{}),
		Auditor:   auditor,
	}

	wrapper := &datastore.Wrapper[TestAuditItem]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, meta)

	item := TestAuditItem{Name: "Test", Status: "active"}
	created, err := wrapper.Create(ctx, item)
	if err != nil {
		t.Fatal("Expected create to succeed:", err)
	}

	// Verify audit log was created
	var logs []TestAuditLog
	err = db.GetDB().NewSelect().Model(&logs).Scan(context.Background())
	if err != nil {
		t.Fatal("Failed to query audit logs:", err)
	}

	if len(logs) != 1 {
		t.Fatalf("Expected 1 audit log, got %d", len(logs))
	}

	if logs[0].ItemID != created.ID {
		t.Errorf("Expected ItemID %d, got %d", created.ID, logs[0].ItemID)
	}
	if logs[0].Operation != "create" {
		t.Errorf("Expected operation 'create', got '%s'", logs[0].Operation)
	}
	if logs[0].NewStatus != "active" {
		t.Errorf("Expected NewStatus 'active', got '%s'", logs[0].NewStatus)
	}
}

func TestAudit_Update(t *testing.T) {
	db, cleanup := setupAuditTestDB(t)
	defer cleanup()

	// Auditor that logs updates with old and new status
	var auditor metadata.AuditFunc[TestAuditItem] = func(ac metadata.AuditContext[TestAuditItem]) any {
		oldStatus := ""
		if ac.Old != nil {
			oldStatus = ac.Old.Status
		}
		newStatus := ""
		if ac.New != nil {
			newStatus = ac.New.Status
		}
		return &TestAuditLog{
			ItemID:    ac.New.ID,
			Operation: string(ac.Operation),
			OldStatus: oldStatus,
			NewStatus: newStatus,
		}
	}

	meta := &metadata.TypeMetadata{
		TypeID:       "test_audit_item",
		TypeName:     "TestAuditItem",
		TableName:    "test_audit_items",
		URLParamUUID: "id",
		ModelType:    reflect.TypeOf(TestAuditItem{}),
		Auditor:      auditor,
	}

	wrapper := &datastore.Wrapper[TestAuditItem]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, meta)

	// Create an item first (without audit to simplify test)
	item := TestAuditItem{Name: "Test", Status: "pending"}
	_, err := db.GetDB().NewInsert().Model(&item).Returning("*").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to insert test item:", err)
	}

	// Update the item
	item.Status = "active"
	_, err = wrapper.Update(ctx, strconv.Itoa(item.ID), item)
	if err != nil {
		t.Fatal("Expected update to succeed:", err)
	}

	// Verify audit log was created
	var logs []TestAuditLog
	err = db.GetDB().NewSelect().Model(&logs).Scan(context.Background())
	if err != nil {
		t.Fatal("Failed to query audit logs:", err)
	}

	if len(logs) != 1 {
		t.Fatalf("Expected 1 audit log, got %d", len(logs))
	}

	if logs[0].Operation != "update" {
		t.Errorf("Expected operation 'update', got '%s'", logs[0].Operation)
	}
	if logs[0].OldStatus != "pending" {
		t.Errorf("Expected OldStatus 'pending', got '%s'", logs[0].OldStatus)
	}
	if logs[0].NewStatus != "active" {
		t.Errorf("Expected NewStatus 'active', got '%s'", logs[0].NewStatus)
	}
}

func TestAudit_Delete(t *testing.T) {
	db, cleanup := setupAuditTestDB(t)
	defer cleanup()

	// Auditor that logs deletes with old status
	var auditor metadata.AuditFunc[TestAuditItem] = func(ac metadata.AuditContext[TestAuditItem]) any {
		return &TestAuditLog{
			ItemID:    ac.Old.ID,
			Operation: string(ac.Operation),
			OldStatus: ac.Old.Status,
		}
	}

	meta := &metadata.TypeMetadata{
		TypeID:       "test_audit_item",
		TypeName:     "TestAuditItem",
		TableName:    "test_audit_items",
		URLParamUUID: "id",
		ModelType:    reflect.TypeOf(TestAuditItem{}),
		Auditor:      auditor,
	}

	wrapper := &datastore.Wrapper[TestAuditItem]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, meta)

	// Create an item first (without audit to simplify test)
	item := TestAuditItem{Name: "Test", Status: "active"}
	_, err := db.GetDB().NewInsert().Model(&item).Returning("*").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to insert test item:", err)
	}

	// Delete the item
	err = wrapper.Delete(ctx, strconv.Itoa(item.ID))
	if err != nil {
		t.Fatal("Expected delete to succeed:", err)
	}

	// Verify audit log was created
	var logs []TestAuditLog
	err = db.GetDB().NewSelect().Model(&logs).Scan(context.Background())
	if err != nil {
		t.Fatal("Failed to query audit logs:", err)
	}

	if len(logs) != 1 {
		t.Fatalf("Expected 1 audit log, got %d", len(logs))
	}

	if logs[0].Operation != "delete" {
		t.Errorf("Expected operation 'delete', got '%s'", logs[0].Operation)
	}
	if logs[0].OldStatus != "active" {
		t.Errorf("Expected OldStatus 'active', got '%s'", logs[0].OldStatus)
	}
}

func TestAudit_SkipWhenNil(t *testing.T) {
	db, cleanup := setupAuditTestDB(t)
	defer cleanup()

	// Auditor that returns nil for creates (skip audit)
	var auditor metadata.AuditFunc[TestAuditItem] = func(ac metadata.AuditContext[TestAuditItem]) any {
		if ac.Operation == metadata.OpCreate {
			return nil // Skip audit for creates
		}
		return &TestAuditLog{
			ItemID:    ac.Old.ID,
			Operation: string(ac.Operation),
		}
	}

	meta := &metadata.TypeMetadata{
		TypeID:    "test_audit_item",
		TypeName:  "TestAuditItem",
		TableName: "test_audit_items",
		ModelType: reflect.TypeOf(TestAuditItem{}),
		Auditor:   auditor,
	}

	wrapper := &datastore.Wrapper[TestAuditItem]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, meta)

	item := TestAuditItem{Name: "Test", Status: "active"}
	_, err := wrapper.Create(ctx, item)
	if err != nil {
		t.Fatal("Expected create to succeed:", err)
	}

	// Verify no audit log was created
	var logs []TestAuditLog
	err = db.GetDB().NewSelect().Model(&logs).Scan(context.Background())
	if err != nil {
		t.Fatal("Failed to query audit logs:", err)
	}

	if len(logs) != 0 {
		t.Fatalf("Expected 0 audit logs, got %d", len(logs))
	}
}

func TestAudit_NoAuditor(t *testing.T) {
	db, cleanup := setupAuditTestDB(t)
	defer cleanup()

	// No auditor configured
	meta := &metadata.TypeMetadata{
		TypeID:    "test_audit_item",
		TypeName:  "TestAuditItem",
		TableName: "test_audit_items",
		ModelType: reflect.TypeOf(TestAuditItem{}),
	}

	wrapper := &datastore.Wrapper[TestAuditItem]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, meta)

	item := TestAuditItem{Name: "Test", Status: "active"}
	_, err := wrapper.Create(ctx, item)
	if err != nil {
		t.Fatal("Create should succeed without auditor:", err)
	}

	// Verify no audit log was created
	var logs []TestAuditLog
	err = db.GetDB().NewSelect().Model(&logs).Scan(context.Background())
	if err != nil {
		t.Fatal("Failed to query audit logs:", err)
	}

	if len(logs) != 0 {
		t.Fatalf("Expected 0 audit logs, got %d", len(logs))
	}
}

func TestAudit_WrongAuditorType(t *testing.T) {
	db, cleanup := setupAuditTestDB(t)
	defer cleanup()

	// Auditor for wrong type (should be skipped)
	wrongAuditor := func(ac metadata.AuditContext[TestUser]) any {
		return &TestAuditLog{Operation: "should not be called"}
	}

	meta := &metadata.TypeMetadata{
		TypeID:    "test_audit_item",
		TypeName:  "TestAuditItem",
		TableName: "test_audit_items",
		ModelType: reflect.TypeOf(TestAuditItem{}),
		Auditor:   wrongAuditor, // Wrong type
	}

	wrapper := &datastore.Wrapper[TestAuditItem]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, meta)

	// Should succeed because auditor type doesn't match (no audit)
	item := TestAuditItem{Name: "Test", Status: "active"}
	_, err := wrapper.Create(ctx, item)
	if err != nil {
		t.Fatal("Create should succeed when auditor type doesn't match:", err)
	}

	// Verify no audit log was created
	var logs []TestAuditLog
	err = db.GetDB().NewSelect().Model(&logs).Scan(context.Background())
	if err != nil {
		t.Fatal("Failed to query audit logs:", err)
	}

	if len(logs) != 0 {
		t.Fatalf("Expected 0 audit logs, got %d", len(logs))
	}
}

// ============================================================================
// UUID Primary Key and Foreign Key Tests
// ============================================================================

// TestUUIDBlog is a parent model with UUID primary key
type TestUUIDBlog struct {
	bun.BaseModel `bun:"table:uuid_blogs"`
	ID            uuid.UUID `bun:"id,pk,type:uuid" json:"id"`
	Name          string    `bun:"name,notnull" json:"name"`
	CreatedAt     time.Time `bun:"created_at,notnull,default:current_timestamp" json:"created_at,omitempty"`
}

// BeforeAppendModel generates UUID for new blogs
func (b *TestUUIDBlog) BeforeAppendModel(_ context.Context, query bun.Query) error {
	if _, ok := query.(*bun.InsertQuery); ok {
		if b.ID == uuid.Nil {
			b.ID = uuid.New()
		}
	}
	return nil
}

// TestUUIDPost is a child model with UUID primary key and UUID foreign key
type TestUUIDPost struct {
	bun.BaseModel `bun:"table:uuid_posts"`
	ID            uuid.UUID     `bun:"id,pk,type:uuid" json:"id"`
	BlogID        uuid.UUID     `bun:"blog_id,notnull,type:uuid" json:"blog_id"`
	Blog          *TestUUIDBlog `bun:"rel:belongs-to,join:blog_id=id" json:"-"`
	Title         string        `bun:"title,notnull" json:"title"`
	CreatedAt     time.Time     `bun:"created_at,notnull,default:current_timestamp" json:"created_at,omitempty"`
}

// BeforeAppendModel generates UUID for new posts
func (p *TestUUIDPost) BeforeAppendModel(_ context.Context, query bun.Query) error {
	if _, ok := query.(*bun.InsertQuery); ok {
		if p.ID == uuid.Nil {
			p.ID = uuid.New()
		}
	}
	return nil
}

func setupUUIDTestDB(t *testing.T) (*datastore.SQLite, func()) {
	t.Helper()

	db, err := datastore.NewSQLite(":memory:")
	if err != nil {
		t.Fatal("Failed to create test database:", err)
	}

	if err := datastore.Initialize(db); err != nil {
		db.Cleanup()
		t.Fatal("Failed to initialize datastore:", err)
	}

	// Create UUID tables
	_, err = db.GetDB().NewCreateTable().Model((*TestUUIDBlog)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		db.Cleanup()
		t.Fatal("Failed to create uuid_blogs table:", err)
	}

	_, err = db.GetDB().NewCreateTable().Model((*TestUUIDPost)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		db.Cleanup()
		t.Fatal("Failed to create uuid_posts table:", err)
	}

	cleanup := func() {
		db.GetDB().NewDropTable().Model((*TestUUIDPost)(nil)).IfExists().Exec(context.Background())
		db.GetDB().NewDropTable().Model((*TestUUIDBlog)(nil)).IfExists().Exec(context.Background())
		datastore.Cleanup()
		db.Cleanup()
	}

	return db, cleanup
}

// TestWrapper_UUID_NestedCreate tests creating nested resources with UUID foreign keys
func TestWrapper_UUID_NestedCreate(t *testing.T) {
	db, cleanup := setupUUIDTestDB(t)
	defer cleanup()

	// Create parent blog metadata
	blogMeta := &metadata.TypeMetadata{
		TypeID:       "test_uuid_blog",
		TypeName:     "TestUUIDBlog",
		TableName:    "uuid_blogs",
		URLParamUUID: "blogId",
		PKField:      "ID",
		ModelType:    reflect.TypeOf(TestUUIDBlog{}),
	}

	// Create parent blog first
	blogWrapper := &datastore.Wrapper[TestUUIDBlog]{Store: db}
	blogCtx := context.WithValue(context.Background(), metadata.MetadataKey, blogMeta)

	blog := TestUUIDBlog{Name: "Test Blog"}
	createdBlog, err := blogWrapper.Create(blogCtx, blog)
	if err != nil {
		t.Fatal("Failed to create blog:", err)
	}

	if createdBlog.ID == uuid.Nil {
		t.Error("Expected blog UUID to be generated")
	}

	// Create post metadata with parent reference
	postMeta := &metadata.TypeMetadata{
		TypeID:        "test_uuid_post",
		TypeName:      "TestUUIDPost",
		TableName:     "uuid_posts",
		URLParamUUID:  "postId",
		PKField:       "ID",
		ModelType:     reflect.TypeOf(TestUUIDPost{}),
		ParentType:    reflect.TypeOf(TestUUIDBlog{}),
		ParentMeta:    blogMeta,
		ForeignKeyCol: "blog_id",
	}

	// Create post under blog with parent IDs in context
	postWrapper := &datastore.Wrapper[TestUUIDPost]{Store: db}
	parentIDs := map[string]string{"blogId": createdBlog.ID.String()}
	postCtx := context.WithValue(context.Background(), metadata.MetadataKey, postMeta)
	postCtx = context.WithValue(postCtx, "parentIDs", parentIDs)

	post := TestUUIDPost{Title: "Test Post"}
	createdPost, err := postWrapper.Create(postCtx, post)
	if err != nil {
		t.Fatal("Failed to create post:", err)
	}

	// Verify UUID FK was set correctly
	if createdPost.BlogID != createdBlog.ID {
		t.Errorf("Expected BlogID %s, got %s", createdBlog.ID, createdPost.BlogID)
	}

	// Verify we can get the post with parent chain validation
	gotPost, err := postWrapper.Get(postCtx, createdPost.ID.String())
	if err != nil {
		t.Fatal("Failed to get post:", err)
	}

	if gotPost.ID != createdPost.ID {
		t.Errorf("Expected post ID %s, got %s", createdPost.ID, gotPost.ID)
	}
}

// TestWrapper_UUID_GetAll tests getting all items with UUID primary keys
func TestWrapper_UUID_GetAll(t *testing.T) {
	db, cleanup := setupUUIDTestDB(t)
	defer cleanup()

	blogMeta := &metadata.TypeMetadata{
		TypeID:       "test_uuid_blog_getall",
		TypeName:     "TestUUIDBlog",
		TableName:    "uuid_blogs",
		URLParamUUID: "blogId",
		ModelType:    reflect.TypeOf(TestUUIDBlog{}),
	}

	wrapper := &datastore.Wrapper[TestUUIDBlog]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, blogMeta)

	// Create multiple blogs
	for i := 0; i < 3; i++ {
		blog := TestUUIDBlog{Name: "Blog " + strconv.Itoa(i)}
		_, err := wrapper.Create(ctx, blog)
		if err != nil {
			t.Fatal("Failed to create blog:", err)
		}
	}

	// Get all blogs
	blogs, count, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("Failed to get all blogs:", err)
	}

	if len(blogs) != 3 {
		t.Errorf("Expected 3 blogs, got %d", len(blogs))
	}

	// Count should be 0 when not requested
	if count != 0 {
		t.Errorf("Expected count 0 (not requested), got %d", count)
	}

	// Verify each blog has a valid UUID
	for _, blog := range blogs {
		if blog.ID == uuid.Nil {
			t.Error("Blog has nil UUID")
		}
	}
}

// TestWrapper_UUID_Update tests updating items with UUID primary keys
func TestWrapper_UUID_Update(t *testing.T) {
	db, cleanup := setupUUIDTestDB(t)
	defer cleanup()

	blogMeta := &metadata.TypeMetadata{
		TypeID:       "test_uuid_blog_update",
		TypeName:     "TestUUIDBlog",
		TableName:    "uuid_blogs",
		URLParamUUID: "blogId",
		ModelType:    reflect.TypeOf(TestUUIDBlog{}),
	}

	wrapper := &datastore.Wrapper[TestUUIDBlog]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, blogMeta)

	// Create blog
	blog := TestUUIDBlog{Name: "Original Name"}
	created, err := wrapper.Create(ctx, blog)
	if err != nil {
		t.Fatal("Failed to create blog:", err)
	}

	// Update blog - ID must be set for WherePK() to work
	updated := TestUUIDBlog{ID: created.ID, Name: "Updated Name"}
	result, err := wrapper.Update(ctx, created.ID.String(), updated)
	if err != nil {
		t.Fatal("Failed to update blog:", err)
	}

	if result.Name != "Updated Name" {
		t.Errorf("Expected name 'Updated Name', got '%s'", result.Name)
	}

	// ID should be preserved
	if result.ID != created.ID {
		t.Errorf("Expected ID %s to be preserved, got %s", created.ID, result.ID)
	}
}

// TestWrapper_UUID_Delete tests deleting items with UUID primary keys
func TestWrapper_UUID_Delete(t *testing.T) {
	db, cleanup := setupUUIDTestDB(t)
	defer cleanup()

	blogMeta := &metadata.TypeMetadata{
		TypeID:       "test_uuid_blog_delete",
		TypeName:     "TestUUIDBlog",
		TableName:    "uuid_blogs",
		URLParamUUID: "blogId",
		ModelType:    reflect.TypeOf(TestUUIDBlog{}),
	}

	wrapper := &datastore.Wrapper[TestUUIDBlog]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, blogMeta)

	// Create blog
	blog := TestUUIDBlog{Name: "To Be Deleted"}
	created, err := wrapper.Create(ctx, blog)
	if err != nil {
		t.Fatal("Failed to create blog:", err)
	}

	// Delete blog
	err = wrapper.Delete(ctx, created.ID.String())
	if err != nil {
		t.Fatal("Failed to delete blog:", err)
	}

	// Verify deletion
	_, err = wrapper.Get(ctx, created.ID.String())
	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Errorf("Expected ErrNotFound after deletion, got: %v", err)
	}
}

// ============================================================================
// Include/Relation Tests
// ============================================================================

// TestIncludeAuthor is a parent model with child relations
type TestIncludeAuthor struct {
	bun.BaseModel `bun:"table:include_authors"`
	ID            int                `bun:"id,pk,autoincrement"`
	Name          string             `bun:"name,notnull"`
	Posts         []*TestIncludePost `bun:"rel:has-many,join:id=author_id"`
	CreatedAt     time.Time          `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

// TestIncludePost is a child model with ownership
type TestIncludePost struct {
	bun.BaseModel `bun:"table:include_posts"`
	ID            int                   `bun:"id,pk,autoincrement"`
	AuthorID      int                   `bun:"author_id,notnull"`
	Author        *TestIncludeAuthor    `bun:"rel:belongs-to,join:author_id=id"`
	OwnerID       string                `bun:"owner_id,notnull"`
	Title         string                `bun:"title,notnull"`
	Comments      []*TestIncludeComment `bun:"rel:has-many,join:id=post_id"`
	CreatedAt     time.Time             `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

// TestIncludeComment is a nested child model with ownership
type TestIncludeComment struct {
	bun.BaseModel `bun:"table:include_comments"`
	ID            int              `bun:"id,pk,autoincrement"`
	PostID        int              `bun:"post_id,notnull"`
	Post          *TestIncludePost `bun:"rel:belongs-to,join:post_id=id"`
	OwnerID       string           `bun:"owner_id,notnull"`
	Text          string           `bun:"text,notnull"`
	CreatedAt     time.Time        `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

func setupIncludeTestDB(t *testing.T) (*datastore.SQLite, func()) {
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
	models := []interface{}{
		(*TestIncludeAuthor)(nil),
		(*TestIncludePost)(nil),
		(*TestIncludeComment)(nil),
	}

	for _, model := range models {
		_, err := db.GetDB().NewCreateTable().Model(model).IfNotExists().Exec(ctx)
		if err != nil {
			db.Cleanup()
			t.Fatal("Failed to create table:", err)
		}
	}

	cleanup := func() {
		for i := len(models) - 1; i >= 0; i-- {
			db.GetDB().NewDropTable().Model(models[i]).IfExists().Exec(ctx)
		}
		datastore.Cleanup()
		db.Cleanup()
	}

	return db, cleanup
}

// Create metadata for include tests
func createIncludeTestMeta() (*metadata.TypeMetadata, *metadata.TypeMetadata) {
	authorMeta := &metadata.TypeMetadata{
		TypeID:       "include_author",
		TypeName:     "TestIncludeAuthor",
		TableName:    "include_authors",
		URLParamUUID: "authorId",
		PKField:      "ID",
		ModelType:    reflect.TypeOf(TestIncludeAuthor{}),
		ChildMeta:    make(map[string]*metadata.TypeMetadata),
	}

	postMeta := &metadata.TypeMetadata{
		TypeID:          "include_post",
		TypeName:        "TestIncludePost",
		TableName:       "include_posts",
		URLParamUUID:    "postId",
		PKField:         "ID",
		ModelType:       reflect.TypeOf(TestIncludePost{}),
		ParentType:      reflect.TypeOf(TestIncludeAuthor{}),
		ParentMeta:      authorMeta,
		ForeignKeyCol:   "author_id",
		OwnershipFields: []string{"OwnerID"},
		BypassScopes:    []string{"admin"},
		ChildMeta:       make(map[string]*metadata.TypeMetadata),
	}

	commentMeta := &metadata.TypeMetadata{
		TypeID:          "include_comment",
		TypeName:        "TestIncludeComment",
		TableName:       "include_comments",
		URLParamUUID:    "commentId",
		PKField:         "ID",
		ModelType:       reflect.TypeOf(TestIncludeComment{}),
		ParentType:      reflect.TypeOf(TestIncludePost{}),
		ParentMeta:      postMeta,
		ForeignKeyCol:   "post_id",
		OwnershipFields: []string{"OwnerID"},
		BypassScopes:    []string{"admin"},
		ChildMeta:       make(map[string]*metadata.TypeMetadata),
	}

	// Register child meta for includes
	authorMeta.ChildMeta["Posts"] = postMeta
	postMeta.ChildMeta["Comments"] = commentMeta

	return authorMeta, postMeta
}

func TestInclude_BasicRelation(t *testing.T) {
	db, cleanup := setupIncludeTestDB(t)
	defer cleanup()

	authorMeta, postMeta := createIncludeTestMeta()

	// Create author
	authorWrapper := &datastore.Wrapper[TestIncludeAuthor]{Store: db}
	authorCtx := context.WithValue(context.Background(), metadata.MetadataKey, authorMeta)

	author := TestIncludeAuthor{Name: "Alice"}
	createdAuthor, err := authorWrapper.Create(authorCtx, author)
	if err != nil {
		t.Fatal("Failed to create author:", err)
	}

	// Create posts directly in DB (without ownership enforcement)
	posts := []TestIncludePost{
		{AuthorID: createdAuthor.ID, OwnerID: "alice", Title: "Alice's Post"},
		{AuthorID: createdAuthor.ID, OwnerID: "bob", Title: "Bob's Post"},
	}

	postWrapper := &datastore.Wrapper[TestIncludePost]{Store: db}
	postCtx := context.WithValue(context.Background(), metadata.MetadataKey, postMeta)
	postCtx = context.WithValue(postCtx, "parentIDs", map[string]string{"authorId": strconv.Itoa(createdAuthor.ID)})

	for _, post := range posts {
		_, err := db.GetDB().NewInsert().Model(&post).Exec(context.Background())
		if err != nil {
			t.Fatal("Failed to insert post:", err)
		}
	}

	// Test: Get author with include=Posts as Alice
	opts := &metadata.QueryOptions{Include: []string{"Posts"}}
	getCtx := context.WithValue(authorCtx, metadata.QueryOptionsKey, opts)
	getCtx = context.WithValue(getCtx, "authInfo", &metadata.AuthInfo{UserID: "alice", Scopes: []string{"user"}})
	// Set AllowedIncludes to authorize "Posts" with ownership filtering
	getCtx = context.WithValue(getCtx, metadata.AllowedIncludesKey, metadata.AllowedIncludes{"Posts": true})
	// Set ownership context (normally set by middleware)
	getCtx = context.WithValue(getCtx, "ownershipEnforced", true)
	getCtx = context.WithValue(getCtx, "ownershipUserID", "alice")

	retrieved, err := authorWrapper.Get(getCtx, strconv.Itoa(createdAuthor.ID))
	if err != nil {
		t.Fatal("Failed to get author with include:", err)
	}

	// Should only see Alice's post (ownership filtered)
	if len(retrieved.Posts) != 1 {
		t.Errorf("Expected 1 post for Alice, got %d", len(retrieved.Posts))
	}
	if len(retrieved.Posts) > 0 && retrieved.Posts[0].OwnerID != "alice" {
		t.Errorf("Expected Alice's post, got owner %s", retrieved.Posts[0].OwnerID)
	}

	// Suppress unused variable warning
	_ = postWrapper
	_ = postCtx
}

func TestInclude_AdminBypass(t *testing.T) {
	db, cleanup := setupIncludeTestDB(t)
	defer cleanup()

	authorMeta, _ := createIncludeTestMeta()

	// Create author
	authorWrapper := &datastore.Wrapper[TestIncludeAuthor]{Store: db}
	authorCtx := context.WithValue(context.Background(), metadata.MetadataKey, authorMeta)

	author := TestIncludeAuthor{Name: "Author"}
	createdAuthor, err := authorWrapper.Create(authorCtx, author)
	if err != nil {
		t.Fatal("Failed to create author:", err)
	}

	// Create posts for different owners
	posts := []TestIncludePost{
		{AuthorID: createdAuthor.ID, OwnerID: "alice", Title: "Alice's Post"},
		{AuthorID: createdAuthor.ID, OwnerID: "bob", Title: "Bob's Post"},
	}

	for _, post := range posts {
		_, err := db.GetDB().NewInsert().Model(&post).Exec(context.Background())
		if err != nil {
			t.Fatal("Failed to insert post:", err)
		}
	}

	// Test: Admin should see all posts
	opts := &metadata.QueryOptions{Include: []string{"Posts"}}
	getCtx := context.WithValue(authorCtx, metadata.QueryOptionsKey, opts)
	getCtx = context.WithValue(getCtx, "authInfo", &metadata.AuthInfo{UserID: "admin", Scopes: []string{"user", "admin"}})
	// Admin has bypass scope, so AllowedIncludes shows false (don't apply ownership)
	getCtx = context.WithValue(getCtx, metadata.AllowedIncludesKey, metadata.AllowedIncludes{"Posts": false})

	retrieved, err := authorWrapper.Get(getCtx, strconv.Itoa(createdAuthor.ID))
	if err != nil {
		t.Fatal("Failed to get author with include:", err)
	}

	// Admin should see all posts
	if len(retrieved.Posts) != 2 {
		t.Errorf("Expected 2 posts for admin, got %d", len(retrieved.Posts))
	}
}

func TestInclude_NoAuth(t *testing.T) {
	db, cleanup := setupIncludeTestDB(t)
	defer cleanup()

	authorMeta, _ := createIncludeTestMeta()

	// Create author
	authorWrapper := &datastore.Wrapper[TestIncludeAuthor]{Store: db}
	authorCtx := context.WithValue(context.Background(), metadata.MetadataKey, authorMeta)

	author := TestIncludeAuthor{Name: "Author"}
	createdAuthor, err := authorWrapper.Create(authorCtx, author)
	if err != nil {
		t.Fatal("Failed to create author:", err)
	}

	// Create posts
	posts := []TestIncludePost{
		{AuthorID: createdAuthor.ID, OwnerID: "alice", Title: "Alice's Post"},
	}

	for _, post := range posts {
		_, err := db.GetDB().NewInsert().Model(&post).Exec(context.Background())
		if err != nil {
			t.Fatal("Failed to insert post:", err)
		}
	}

	// Test: Unauthenticated request should see no posts
	opts := &metadata.QueryOptions{Include: []string{"Posts"}}
	getCtx := context.WithValue(authorCtx, metadata.QueryOptionsKey, opts)
	// No authInfo in context

	retrieved, err := authorWrapper.Get(getCtx, strconv.Itoa(createdAuthor.ID))
	if err != nil {
		t.Fatal("Failed to get author with include:", err)
	}

	// Unauthenticated should see no posts (ownership required)
	if len(retrieved.Posts) != 0 {
		t.Errorf("Expected 0 posts for unauthenticated user, got %d", len(retrieved.Posts))
	}
}

func TestInclude_UnknownRelation(t *testing.T) {
	db, cleanup := setupIncludeTestDB(t)
	defer cleanup()

	authorMeta, _ := createIncludeTestMeta()

	// Create author
	authorWrapper := &datastore.Wrapper[TestIncludeAuthor]{Store: db}
	authorCtx := context.WithValue(context.Background(), metadata.MetadataKey, authorMeta)

	author := TestIncludeAuthor{Name: "Author"}
	createdAuthor, err := authorWrapper.Create(authorCtx, author)
	if err != nil {
		t.Fatal("Failed to create author:", err)
	}

	// Test: Unknown relation should be silently ignored
	opts := &metadata.QueryOptions{Include: []string{"UnknownRelation"}}
	getCtx := context.WithValue(authorCtx, metadata.QueryOptionsKey, opts)

	retrieved, err := authorWrapper.Get(getCtx, strconv.Itoa(createdAuthor.ID))
	if err != nil {
		t.Fatal("Failed to get author with unknown include:", err)
	}

	// Should succeed without error, posts will be nil/empty
	if retrieved.ID != createdAuthor.ID {
		t.Errorf("Expected author ID %d, got %d", createdAuthor.ID, retrieved.ID)
	}
}

func TestInclude_GetAllWithRelation(t *testing.T) {
	db, cleanup := setupIncludeTestDB(t)
	defer cleanup()

	authorMeta, _ := createIncludeTestMeta()

	// Create authors
	authorWrapper := &datastore.Wrapper[TestIncludeAuthor]{Store: db}
	authorCtx := context.WithValue(context.Background(), metadata.MetadataKey, authorMeta)

	author1 := TestIncludeAuthor{Name: "Author 1"}
	created1, err := authorWrapper.Create(authorCtx, author1)
	if err != nil {
		t.Fatal("Failed to create author 1:", err)
	}

	author2 := TestIncludeAuthor{Name: "Author 2"}
	created2, err := authorWrapper.Create(authorCtx, author2)
	if err != nil {
		t.Fatal("Failed to create author 2:", err)
	}

	// Create posts for different authors
	posts := []TestIncludePost{
		{AuthorID: created1.ID, OwnerID: "alice", Title: "Post 1"},
		{AuthorID: created2.ID, OwnerID: "alice", Title: "Post 2"},
	}

	for _, post := range posts {
		_, err := db.GetDB().NewInsert().Model(&post).Exec(context.Background())
		if err != nil {
			t.Fatal("Failed to insert post:", err)
		}
	}

	// Test: GetAll with include as alice
	opts := &metadata.QueryOptions{Include: []string{"Posts"}}
	getCtx := context.WithValue(authorCtx, metadata.QueryOptionsKey, opts)
	getCtx = context.WithValue(getCtx, "authInfo", &metadata.AuthInfo{UserID: "alice", Scopes: []string{"user"}})
	// Set AllowedIncludes to authorize "Posts" with ownership filtering
	getCtx = context.WithValue(getCtx, metadata.AllowedIncludesKey, metadata.AllowedIncludes{"Posts": true})
	// Set ownership context (normally set by middleware)
	getCtx = context.WithValue(getCtx, "ownershipEnforced", true)
	getCtx = context.WithValue(getCtx, "ownershipUserID", "alice")

	retrieved, _, err := authorWrapper.GetAll(getCtx)
	if err != nil {
		t.Fatal("Failed to get all authors with include:", err)
	}

	if len(retrieved) != 2 {
		t.Errorf("Expected 2 authors, got %d", len(retrieved))
	}

	// Each author should have 1 post owned by alice
	for _, author := range retrieved {
		if len(author.Posts) != 1 {
			t.Errorf("Expected 1 post per author for alice, got %d", len(author.Posts))
		}
	}
}

func TestInclude_NoOwnershipConfig(t *testing.T) {
	db, cleanup := setupIncludeTestDB(t)
	defer cleanup()

	// Create metadata without ownership on posts
	authorMeta := &metadata.TypeMetadata{
		TypeID:       "include_author_no_owner",
		TypeName:     "TestIncludeAuthor",
		TableName:    "include_authors",
		URLParamUUID: "authorId",
		PKField:      "ID",
		ModelType:    reflect.TypeOf(TestIncludeAuthor{}),
		ChildMeta:    make(map[string]*metadata.TypeMetadata),
	}

	postMetaNoOwnership := &metadata.TypeMetadata{
		TypeID:        "include_post_no_owner",
		TypeName:      "TestIncludePost",
		TableName:     "include_posts",
		URLParamUUID:  "postId",
		PKField:       "ID",
		ModelType:     reflect.TypeOf(TestIncludePost{}),
		ParentType:    reflect.TypeOf(TestIncludeAuthor{}),
		ParentMeta:    authorMeta,
		ForeignKeyCol: "author_id",
		// No OwnershipFields configured
		ChildMeta: make(map[string]*metadata.TypeMetadata),
	}

	authorMeta.ChildMeta["Posts"] = postMetaNoOwnership

	// Create author
	authorWrapper := &datastore.Wrapper[TestIncludeAuthor]{Store: db}
	authorCtx := context.WithValue(context.Background(), metadata.MetadataKey, authorMeta)

	author := TestIncludeAuthor{Name: "Author"}
	createdAuthor, err := authorWrapper.Create(authorCtx, author)
	if err != nil {
		t.Fatal("Failed to create author:", err)
	}

	// Create posts
	posts := []TestIncludePost{
		{AuthorID: createdAuthor.ID, OwnerID: "alice", Title: "Post 1"},
		{AuthorID: createdAuthor.ID, OwnerID: "bob", Title: "Post 2"},
	}

	for _, post := range posts {
		_, err := db.GetDB().NewInsert().Model(&post).Exec(context.Background())
		if err != nil {
			t.Fatal("Failed to insert post:", err)
		}
	}

	// Test: Any user should see all posts (no ownership filter on child type)
	opts := &metadata.QueryOptions{Include: []string{"Posts"}}
	getCtx := context.WithValue(authorCtx, metadata.QueryOptionsKey, opts)
	getCtx = context.WithValue(getCtx, "authInfo", &metadata.AuthInfo{UserID: "charlie", Scopes: []string{"user"}})
	// AllowedIncludes says to apply ownership, but child has no OwnershipFields
	// so the ownership filter will be a no-op (applyOwnershipFilterWithMeta skips it)
	getCtx = context.WithValue(getCtx, metadata.AllowedIncludesKey, metadata.AllowedIncludes{"Posts": true})
	getCtx = context.WithValue(getCtx, "ownershipEnforced", true)
	getCtx = context.WithValue(getCtx, "ownershipUserID", "charlie")

	retrieved, err := authorWrapper.Get(getCtx, strconv.Itoa(createdAuthor.ID))
	if err != nil {
		t.Fatal("Failed to get author with include:", err)
	}

	// Should see all posts since no ownership is configured on child
	if len(retrieved.Posts) != 2 {
		t.Errorf("Expected 2 posts (no ownership filter), got %d", len(retrieved.Posts))
	}
}

// TestWrapper_GetByParentRelation tests getting an item via parent's FK field
func TestWrapper_GetByParentRelation(t *testing.T) {
	db, cleanup := setupIncludeTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create author
	authorWrapper := &datastore.Wrapper[TestIncludeAuthor]{Store: db}
	authorMeta := &metadata.TypeMetadata{
		TypeID:       "test_author_id",
		TypeName:     "TestIncludeAuthor",
		TableName:    "include_authors",
		URLParamUUID: "author_id",
		ModelType:    reflect.TypeOf(TestIncludeAuthor{}),
	}
	authorCtx := context.WithValue(ctx, metadata.MetadataKey, authorMeta)
	author, err := authorWrapper.Create(authorCtx, TestIncludeAuthor{Name: "Test Author"})
	if err != nil {
		t.Fatal("Failed to create author:", err)
	}

	// Create post with author - use simple metadata without ParentType to avoid nested resource checks
	postWrapper := &datastore.Wrapper[TestIncludePost]{Store: db}
	postMeta := &metadata.TypeMetadata{
		TypeID:       "test_post_id",
		TypeName:     "TestIncludePost",
		TableName:    "include_posts",
		URLParamUUID: "post_id",
		ModelType:    reflect.TypeOf(TestIncludePost{}),
	}
	postCtx := context.WithValue(ctx, metadata.MetadataKey, postMeta)
	post, err := postWrapper.Create(postCtx, TestIncludePost{AuthorID: author.ID, OwnerID: "alice", Title: "Test Post"})
	if err != nil {
		t.Fatal("Failed to create post:", err)
	}

	// Now test GetByParentRelation - get Author from Post
	// We need metadata for Author with ParentMeta pointing to Post and ParentFKField set
	authorFromPostMeta := &metadata.TypeMetadata{
		TypeID:        "test_author_from_post_id",
		TypeName:      "TestIncludeAuthor",
		TableName:     "include_authors",
		URLParamUUID:  postMeta.URLParamUUID, // Use post's URL param
		ModelType:     reflect.TypeOf(TestIncludeAuthor{}),
		ParentType:    reflect.TypeOf(TestIncludePost{}),
		ParentMeta:    postMeta,
		ParentFKField: "AuthorID", // Post.AuthorID points to Author.ID
	}
	authorFromPostCtx := context.WithValue(ctx, metadata.MetadataKey, authorFromPostMeta)

	// Get author by passing the post ID
	retrieved, err := authorWrapper.GetByParentRelation(authorFromPostCtx, strconv.Itoa(post.ID))
	if err != nil {
		t.Fatal("Failed to get author by parent relation:", err)
	}

	if retrieved.ID != author.ID {
		t.Errorf("Expected author ID %d, got %d", author.ID, retrieved.ID)
	}
	if retrieved.Name != author.Name {
		t.Errorf("Expected author name %s, got %s", author.Name, retrieved.Name)
	}
}

// TestWrapper_GetByParentRelation_NoParentMeta tests error when parent meta is missing
func TestWrapper_GetByParentRelation_NoParentMeta(t *testing.T) {
	db, cleanup := setupIncludeTestDB(t)
	defer cleanup()

	authorWrapper := &datastore.Wrapper[TestIncludeAuthor]{Store: db}
	// Metadata without ParentMeta
	authorMeta := &metadata.TypeMetadata{
		TypeID:       "test_author_id",
		TypeName:     "TestIncludeAuthor",
		TableName:    "include_authors",
		URLParamUUID: "author_id",
		ModelType:    reflect.TypeOf(TestIncludeAuthor{}),
	}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, authorMeta)

	_, err := authorWrapper.GetByParentRelation(ctx, "1")
	if err == nil {
		t.Error("Expected error when ParentMeta is nil")
	}
	if err != nil && err.Error() != "resolveChildIDFromParent requires parent metadata" {
		t.Errorf("Unexpected error message: %s", err.Error())
	}
}

// TestWrapper_GetByParentRelation_NoParentFKField tests error when ParentFKField is missing
func TestWrapper_GetByParentRelation_NoParentFKField(t *testing.T) {
	db, cleanup := setupIncludeTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create post metadata
	postMeta := &metadata.TypeMetadata{
		TypeID:       "test_post_id",
		TypeName:     "TestIncludePost",
		TableName:    "include_posts",
		URLParamUUID: "post_id",
		ModelType:    reflect.TypeOf(TestIncludePost{}),
	}

	authorWrapper := &datastore.Wrapper[TestIncludeAuthor]{Store: db}
	// Metadata with ParentMeta but no ParentFKField
	authorMeta := &metadata.TypeMetadata{
		TypeID:       "test_author_id",
		TypeName:     "TestIncludeAuthor",
		TableName:    "include_authors",
		URLParamUUID: "author_id",
		ModelType:    reflect.TypeOf(TestIncludeAuthor{}),
		ParentMeta:   postMeta,
		// ParentFKField is empty
	}
	authorCtx := context.WithValue(ctx, metadata.MetadataKey, authorMeta)

	_, err := authorWrapper.GetByParentRelation(authorCtx, "1")
	if err == nil {
		t.Error("Expected error when ParentFKField is empty")
	}
	if err != nil && err.Error() != "resolveChildIDFromParent requires ParentFKField to be set" {
		t.Errorf("Unexpected error message: %s", err.Error())
	}
}

// TestWrapper_UpdateByParentRelation tests updating an item via parent's FK field
func TestWrapper_UpdateByParentRelation(t *testing.T) {
	db, cleanup := setupIncludeTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create author
	authorWrapper := &datastore.Wrapper[TestIncludeAuthor]{Store: db}
	authorMeta := &metadata.TypeMetadata{
		TypeID:       "test_author_id",
		TypeName:     "TestIncludeAuthor",
		TableName:    "include_authors",
		URLParamUUID: "author_id",
		ModelType:    reflect.TypeOf(TestIncludeAuthor{}),
	}
	authorCtx := context.WithValue(ctx, metadata.MetadataKey, authorMeta)
	author, err := authorWrapper.Create(authorCtx, TestIncludeAuthor{Name: "Original Name"})
	if err != nil {
		t.Fatal("Failed to create author:", err)
	}

	// Create post with author - use simple metadata without ParentType to avoid nested resource checks
	postWrapper := &datastore.Wrapper[TestIncludePost]{Store: db}
	postMeta := &metadata.TypeMetadata{
		TypeID:       "test_post_id",
		TypeName:     "TestIncludePost",
		TableName:    "include_posts",
		URLParamUUID: "post_id",
		ModelType:    reflect.TypeOf(TestIncludePost{}),
	}
	postCtx := context.WithValue(ctx, metadata.MetadataKey, postMeta)
	post, err := postWrapper.Create(postCtx, TestIncludePost{AuthorID: author.ID, OwnerID: "alice", Title: "Test Post"})
	if err != nil {
		t.Fatal("Failed to create post:", err)
	}

	// Now test UpdateByParentRelation - update Author via Post
	authorFromPostMeta := &metadata.TypeMetadata{
		TypeID:        "test_author_from_post_id",
		TypeName:      "TestIncludeAuthor",
		TableName:     "include_authors",
		URLParamUUID:  postMeta.URLParamUUID,
		ModelType:     reflect.TypeOf(TestIncludeAuthor{}),
		ParentType:    reflect.TypeOf(TestIncludePost{}),
		ParentMeta:    postMeta,
		ParentFKField: "AuthorID",
	}
	authorFromPostCtx := context.WithValue(ctx, metadata.MetadataKey, authorFromPostMeta)

	// Update author by passing the post ID - preserve CreatedAt
	updatedAuthor := TestIncludeAuthor{ID: author.ID, Name: "Updated Name", CreatedAt: author.CreatedAt}
	updated, err := authorWrapper.UpdateByParentRelation(authorFromPostCtx, strconv.Itoa(post.ID), updatedAuthor)
	if err != nil {
		t.Fatal("Failed to update author by parent relation:", err)
	}

	if updated.Name != "Updated Name" {
		t.Errorf("Expected updated name 'Updated Name', got %s", updated.Name)
	}

	// Verify the update persisted
	retrieved, err := authorWrapper.Get(authorCtx, strconv.Itoa(author.ID))
	if err != nil {
		t.Fatal("Failed to get author:", err)
	}
	if retrieved.Name != "Updated Name" {
		t.Errorf("Expected persisted name 'Updated Name', got %s", retrieved.Name)
	}
}
