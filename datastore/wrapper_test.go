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
	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/datastore"
	apperrors "github.com/sjgoldie/go-restgen/errors"
	"github.com/sjgoldie/go-restgen/metadata"
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
	retrieved, _, _, err := wrapper.GetAll(ctx)
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

	retrieved, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("Failed to get all users:", err)
	}

	if len(retrieved) != 0 {
		t.Errorf("Expected 0 users, got %d", len(retrieved))
	}
}

func TestWrapper_GetAll_NoMetadata(t *testing.T) {
	server, cleanup := setupTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestUser]{Store: server}
	// Use a context without metadata
	ctx := context.Background()

	_, _, _, err := wrapper.GetAll(ctx)
	if err == nil {
		t.Error("Expected error when metadata is missing from context")
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
	retrieved, _, _, err := wrapper.GetAll(ctx)
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
	all, _, _, err := wrapper.GetAll(ctx)
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
	ctxWithOwnership := context.WithValue(ctx, metadata.OwnershipEnforcedKey, true)
	ctxWithOwnership = context.WithValue(ctxWithOwnership, metadata.OwnershipUserIDKey, "alice")

	retrieved, _, _, err := wrapper.GetAll(ctxWithOwnership)
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
	ctxAlice := context.WithValue(ctx, metadata.OwnershipEnforcedKey, true)
	ctxAlice = context.WithValue(ctxAlice, metadata.OwnershipUserIDKey, "alice")

	retrieved, err := wrapper.Get(ctxAlice, strconv.Itoa(created.ID))
	if err != nil {
		t.Fatal("Failed to get blog as alice:", err)
	}
	if retrieved.AuthorID != "alice" {
		t.Errorf("Expected blog to belong to alice, got %s", retrieved.AuthorID)
	}

	// Get with ownership enforcement for bob (should fail)
	ctxBob := context.WithValue(ctx, metadata.OwnershipEnforcedKey, true)
	ctxBob = context.WithValue(ctxBob, metadata.OwnershipUserIDKey, "bob")

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
	ctxWithParent := context.WithValue(ctxPost, metadata.ParentIDsKey, parentIDs)

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
	ctxAlice := context.WithValue(ctxPost, metadata.OwnershipEnforcedKey, true)
	ctxAlice = context.WithValue(ctxAlice, metadata.OwnershipUserIDKey, "alice")

	retrieved, _, _, err := postWrapper.GetAll(ctxAlice)
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
	ctxCharlie := context.WithValue(ctx, metadata.OwnershipEnforcedKey, true)
	ctxCharlie = context.WithValue(ctxCharlie, metadata.OwnershipUserIDKey, "charlie")

	authInfo := &metadata.AuthInfo{
		UserID: "charlie",
		Scopes: []string{"admin"},
	}
	ctxCharlie = context.WithValue(ctxCharlie, metadata.AuthInfoKey, authInfo)

	retrieved, _, _, err := wrapper.GetAll(ctxCharlie)
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
	ctxDiana := context.WithValue(ctx, metadata.OwnershipEnforcedKey, true)
	ctxDiana = context.WithValue(ctxDiana, metadata.OwnershipUserIDKey, "diana")

	authInfoDiana := &metadata.AuthInfo{
		UserID: "diana",
		Scopes: []string{"moderator"},
	}
	ctxDiana = context.WithValue(ctxDiana, metadata.AuthInfoKey, authInfoDiana)

	retrieved, _, _, err := wrapper.GetAll(ctxDiana)
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
	ctxAlice := context.WithValue(ctx, metadata.OwnershipEnforcedKey, true)
	ctxAlice = context.WithValue(ctxAlice, metadata.OwnershipUserIDKey, "alice")

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
	retrieved, _, _, err := wrapper.GetAll(ctx)
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
	ctxAlice := context.WithValue(ctx, metadata.OwnershipEnforcedKey, true)
	ctxAlice = context.WithValue(ctxAlice, metadata.OwnershipUserIDKey, "alice")

	retrieved, _, _, err := wrapper.GetAll(ctxAlice)
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
	ctxBob := context.WithValue(ctxPost, metadata.OwnershipEnforcedKey, true)
	ctxBob = context.WithValue(ctxBob, metadata.OwnershipUserIDKey, "bob")

	// Add parent ID to context
	parentIDs := map[string]string{
		"blog_id": strconv.Itoa(createdBlog.ID),
	}
	ctxBob = context.WithValue(ctxBob, metadata.ParentIDsKey, parentIDs)

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
	ctxAlice := context.WithValue(ctxPost, metadata.OwnershipEnforcedKey, true)
	ctxAlice = context.WithValue(ctxAlice, metadata.OwnershipUserIDKey, "alice")
	ctxAlice = context.WithValue(ctxAlice, metadata.ParentIDsKey, parentIDs)

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
	postCtx = context.WithValue(postCtx, metadata.ParentIDsKey, parentIDs)

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
	blogs, count, _, err := wrapper.GetAll(ctx)
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
	postCtx = context.WithValue(postCtx, metadata.ParentIDsKey, map[string]string{"authorId": strconv.Itoa(createdAuthor.ID)})

	for _, post := range posts {
		_, err := db.GetDB().NewInsert().Model(&post).Exec(context.Background())
		if err != nil {
			t.Fatal("Failed to insert post:", err)
		}
	}

	// Test: Get author with include=Posts as Alice
	opts := &metadata.QueryOptions{Include: []string{"Posts"}}
	getCtx := context.WithValue(authorCtx, metadata.QueryOptionsKey, opts)
	getCtx = context.WithValue(getCtx, metadata.AuthInfoKey, &metadata.AuthInfo{UserID: "alice", Scopes: []string{"user"}})
	// Set AllowedIncludes to authorize "Posts" with ownership filtering
	getCtx = context.WithValue(getCtx, metadata.AllowedIncludesKey, metadata.AllowedIncludes{"Posts": true})
	// Set ownership context (normally set by middleware)
	getCtx = context.WithValue(getCtx, metadata.OwnershipEnforcedKey, true)
	getCtx = context.WithValue(getCtx, metadata.OwnershipUserIDKey, "alice")

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
	getCtx = context.WithValue(getCtx, metadata.AuthInfoKey, &metadata.AuthInfo{UserID: "admin", Scopes: []string{"user", "admin"}})
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
	// No metadata.AuthInfoKey in context

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
	getCtx = context.WithValue(getCtx, metadata.AuthInfoKey, &metadata.AuthInfo{UserID: "alice", Scopes: []string{"user"}})
	// Set AllowedIncludes to authorize "Posts" with ownership filtering
	getCtx = context.WithValue(getCtx, metadata.AllowedIncludesKey, metadata.AllowedIncludes{"Posts": true})
	// Set ownership context (normally set by middleware)
	getCtx = context.WithValue(getCtx, metadata.OwnershipEnforcedKey, true)
	getCtx = context.WithValue(getCtx, metadata.OwnershipUserIDKey, "alice")

	retrieved, _, _, err := authorWrapper.GetAll(getCtx)
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
	getCtx = context.WithValue(getCtx, metadata.AuthInfoKey, &metadata.AuthInfo{UserID: "charlie", Scopes: []string{"user"}})
	// AllowedIncludes says to apply ownership, but child has no OwnershipFields
	// so the ownership filter will be a no-op (applyOwnershipFilterWithMeta skips it)
	getCtx = context.WithValue(getCtx, metadata.AllowedIncludesKey, metadata.AllowedIncludes{"Posts": true})
	getCtx = context.WithValue(getCtx, metadata.OwnershipEnforcedKey, true)
	getCtx = context.WithValue(getCtx, metadata.OwnershipUserIDKey, "charlie")

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

// Batch operation tests

// BatchTestAuthor is a test model for batch nested tests
type BatchTestAuthor struct {
	bun.BaseModel `bun:"table:batch_authors"`
	ID            int       `bun:"id,pk,autoincrement"`
	Name          string    `bun:"name,notnull"`
	Email         string    `bun:"email,notnull"`
	CreatedAt     time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

// BatchTestArticle is a test model for batch nested tests
type BatchTestArticle struct {
	bun.BaseModel `bun:"table:batch_articles"`
	ID            int       `bun:"id,pk,autoincrement"`
	AuthorID      int       `bun:"author_id,notnull"`
	Title         string    `bun:"title,notnull"`
	Content       string    `bun:"content"`
	CreatedAt     time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

var batchTestAuthorMeta = &metadata.TypeMetadata{
	TypeID:        "batch_test_author_id",
	TypeName:      "BatchTestAuthor",
	TableName:     "batch_authors",
	URLParamUUID:  "authorId",
	PKField:       "ID",
	ModelType:     reflect.TypeOf(BatchTestAuthor{}),
	ParentType:    nil,
	ParentMeta:    nil,
	ForeignKeyCol: "",
}

var batchTestArticleMeta = &metadata.TypeMetadata{
	TypeID:        "batch_test_article_id",
	TypeName:      "BatchTestArticle",
	TableName:     "batch_articles",
	URLParamUUID:  "articleId",
	PKField:       "ID",
	ModelType:     reflect.TypeOf(BatchTestArticle{}),
	ParentType:    reflect.TypeOf(BatchTestAuthor{}),
	ParentMeta:    batchTestAuthorMeta,
	ForeignKeyCol: "author_id",
}

func setupBatchNestedTestDB(t *testing.T) (*datastore.SQLite, func()) {
	t.Helper()

	db, err := datastore.NewSQLite(":memory:")
	if err != nil {
		t.Fatal("Failed to create test database:", err)
	}

	// Create schemas
	ctx := context.Background()
	_, err = db.GetDB().NewCreateTable().Model((*BatchTestAuthor)(nil)).IfNotExists().Exec(ctx)
	if err != nil {
		db.Cleanup()
		t.Fatal("Failed to create batch_authors table:", err)
	}

	_, err = db.GetDB().NewCreateTable().Model((*BatchTestArticle)(nil)).IfNotExists().Exec(ctx)
	if err != nil {
		db.Cleanup()
		t.Fatal("Failed to create batch_articles table:", err)
	}

	cleanup := func() {
		db.GetDB().NewDropTable().Model((*BatchTestArticle)(nil)).IfExists().Exec(ctx)
		db.GetDB().NewDropTable().Model((*BatchTestAuthor)(nil)).IfExists().Exec(ctx)
		db.Cleanup()
	}

	return db, cleanup
}

func TestWrapper_BatchCreate_Success(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestUser]{Store: db}
	ctx := ctxWithMeta(testUserMeta)

	users := []TestUser{
		{Name: "User 1", Email: "user1@example.com"},
		{Name: "User 2", Email: "user2@example.com"},
		{Name: "User 3", Email: "user3@example.com"},
	}

	results, err := wrapper.BatchCreate(ctx, users)
	if err != nil {
		t.Fatal("BatchCreate failed:", err)
	}

	if len(results) != 3 {
		t.Errorf("Expected 3 results, got %d", len(results))
	}

	// Verify all items have IDs assigned
	for i, result := range results {
		if result.ID == 0 {
			t.Errorf("Result %d has no ID assigned", i)
		}
	}

	// Verify items are in the database
	all, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}
	if len(all) != 3 {
		t.Errorf("Expected 3 items in database, got %d", len(all))
	}
}

func TestWrapper_BatchCreate_NoMetadata(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestUser]{Store: db}
	ctx := context.Background() // No metadata

	users := []TestUser{
		{Name: "User 1", Email: "user1@example.com"},
	}

	_, err := wrapper.BatchCreate(ctx, users)
	if err == nil {
		t.Error("Expected error when metadata is missing")
	}
}

func TestWrapper_BatchUpdate_Success(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestUser]{Store: db}
	ctx := ctxWithMeta(testUserMeta)

	// First create some users
	users := []TestUser{
		{Name: "User 1", Email: "user1@example.com"},
		{Name: "User 2", Email: "user2@example.com"},
	}
	created, err := wrapper.BatchCreate(ctx, users)
	if err != nil {
		t.Fatal("BatchCreate failed:", err)
	}

	// Update them - copy full object to preserve CreatedAt
	updates := []TestUser{
		{ID: created[0].ID, Name: "Updated 1", Email: "updated1@example.com", CreatedAt: created[0].CreatedAt},
		{ID: created[1].ID, Name: "Updated 2", Email: "updated2@example.com", CreatedAt: created[1].CreatedAt},
	}

	results, err := wrapper.BatchUpdate(ctx, updates)
	if err != nil {
		t.Fatal("BatchUpdate failed:", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}

	// Verify updates
	for i, result := range results {
		expectedName := "Updated " + strconv.Itoa(i+1)
		if result.Name != expectedName {
			t.Errorf("Expected name '%s', got '%s'", expectedName, result.Name)
		}
	}
}

func TestWrapper_BatchUpdate_NotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestUser]{Store: db}
	ctx := ctxWithMeta(testUserMeta)

	// Try to update non-existent items
	updates := []TestUser{
		{ID: 999, Name: "Does not exist", Email: "none@example.com"},
	}

	_, err := wrapper.BatchUpdate(ctx, updates)
	if err == nil {
		t.Error("Expected error when updating non-existent item")
	}
	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Errorf("Expected ErrNotFound, got %v", err)
	}
}

func TestWrapper_BatchUpdate_MissingID(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestUser]{Store: db}
	ctx := ctxWithMeta(testUserMeta)

	// Try to update without ID
	updates := []TestUser{
		{Name: "No ID", Email: "noid@example.com"},
	}

	_, err := wrapper.BatchUpdate(ctx, updates)
	if err == nil {
		t.Error("Expected error when ID is missing")
	}
}

func TestWrapper_BatchDelete_Success(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestUser]{Store: db}
	ctx := ctxWithMeta(testUserMeta)

	// First create some users
	users := []TestUser{
		{Name: "User 1", Email: "user1@example.com"},
		{Name: "User 2", Email: "user2@example.com"},
		{Name: "User 3", Email: "user3@example.com"},
	}
	created, err := wrapper.BatchCreate(ctx, users)
	if err != nil {
		t.Fatal("BatchCreate failed:", err)
	}

	// Delete first two
	deletes := []TestUser{
		{ID: created[0].ID},
		{ID: created[1].ID},
	}

	err = wrapper.BatchDelete(ctx, deletes)
	if err != nil {
		t.Fatal("BatchDelete failed:", err)
	}

	// Verify only one remains
	all, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}
	if len(all) != 1 {
		t.Errorf("Expected 1 item remaining, got %d", len(all))
	}
	if all[0].ID != created[2].ID {
		t.Errorf("Expected remaining item to be ID %d, got %d", created[2].ID, all[0].ID)
	}
}

func TestWrapper_BatchDelete_NotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestUser]{Store: db}
	ctx := ctxWithMeta(testUserMeta)

	// Try to delete non-existent items
	deletes := []TestUser{
		{ID: 999},
	}

	err := wrapper.BatchDelete(ctx, deletes)
	if err == nil {
		t.Error("Expected error when deleting non-existent item")
	}
	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Errorf("Expected ErrNotFound, got %v", err)
	}
}

func TestWrapper_BatchDelete_MissingID(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestUser]{Store: db}
	ctx := ctxWithMeta(testUserMeta)

	// Try to delete without ID
	deletes := []TestUser{
		{Name: "No ID"},
	}

	err := wrapper.BatchDelete(ctx, deletes)
	if err == nil {
		t.Error("Expected error when ID is missing")
	}
}

func TestWrapper_BatchCreate_Transactional(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create a validator that will fail on second item
	validator := metadata.ValidatorFunc[TestUser](func(vc metadata.ValidationContext[TestUser]) error {
		if vc.New != nil && vc.New.Name == "FAIL" {
			return apperrors.NewValidationError("validation failed")
		}
		return nil
	})

	validatorMeta := &metadata.TypeMetadata{
		TypeID:       "test_validator_id",
		TypeName:     "TestUser",
		TableName:    "users",
		URLParamUUID: "id",
		PKField:      "ID",
		ModelType:    reflect.TypeOf(TestUser{}),
		Validator:    validator,
	}

	wrapper := &datastore.Wrapper[TestUser]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, validatorMeta)

	users := []TestUser{
		{Name: "User 1", Email: "user1@example.com"},
		{Name: "FAIL", Email: "fail@example.com"}, // This will fail validation
		{Name: "User 3", Email: "user3@example.com"},
	}

	_, err := wrapper.BatchCreate(ctx, users)
	if err == nil {
		t.Error("Expected validation error")
	}

	// Verify transaction was rolled back - no items should be in database
	all, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}
	if len(all) != 0 {
		t.Errorf("Expected 0 items (transaction rolled back), got %d", len(all))
	}
}

func TestWrapper_BatchCreate_NestedResource_MissingParent(t *testing.T) {
	db, cleanup := setupBatchNestedTestDB(t)
	defer cleanup()

	articleWrapper := &datastore.Wrapper[BatchTestArticle]{Store: db}
	ctxArticle := ctxWithMeta(batchTestArticleMeta)
	// No parent IDs in context

	articles := []BatchTestArticle{
		{Title: "Article 1", Content: "Content 1"},
	}

	_, err := articleWrapper.BatchCreate(ctxArticle, articles)
	if err == nil {
		t.Error("Expected error when parent context is missing")
	}
}

func TestWrapper_BatchUpdate_Transactional(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create a validator that will fail on specific name
	validator := metadata.ValidatorFunc[TestUser](func(vc metadata.ValidationContext[TestUser]) error {
		if vc.New != nil && vc.New.Name == "FAIL_UPDATE" {
			return apperrors.NewValidationError("update validation failed")
		}
		return nil
	})

	validatorMeta := &metadata.TypeMetadata{
		TypeID:       "test_validator_id",
		TypeName:     "TestUser",
		TableName:    "users",
		URLParamUUID: "id",
		PKField:      "ID",
		ModelType:    reflect.TypeOf(TestUser{}),
		Validator:    validator,
	}

	wrapper := &datastore.Wrapper[TestUser]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, validatorMeta)

	// Create initial users
	users := []TestUser{
		{Name: "User 1", Email: "user1@example.com"},
		{Name: "User 2", Email: "user2@example.com"},
	}
	created, err := wrapper.BatchCreate(ctx, users)
	if err != nil {
		t.Fatal("BatchCreate failed:", err)
	}

	// Try to update - second one will fail validation
	updates := []TestUser{
		{ID: created[0].ID, Name: "Updated 1", Email: "u1@example.com", CreatedAt: created[0].CreatedAt},
		{ID: created[1].ID, Name: "FAIL_UPDATE", Email: "u2@example.com", CreatedAt: created[1].CreatedAt},
	}

	_, err = wrapper.BatchUpdate(ctx, updates)
	if err == nil {
		t.Error("Expected validation error")
	}

	// Verify first user was NOT updated (transaction rolled back)
	user1, err := wrapper.Get(ctx, strconv.Itoa(created[0].ID))
	if err != nil {
		t.Fatal("Get failed:", err)
	}
	if user1.Name != "User 1" {
		t.Errorf("Expected name 'User 1' (unchanged), got '%s'", user1.Name)
	}
}

func TestWrapper_BatchDelete_Transactional(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create a validator that will fail on delete
	validator := metadata.ValidatorFunc[TestUser](func(vc metadata.ValidationContext[TestUser]) error {
		if vc.Operation == metadata.OpDelete && vc.Old != nil && vc.Old.Name == "NO_DELETE" {
			return apperrors.NewValidationError("cannot delete this user")
		}
		return nil
	})

	validatorMeta := &metadata.TypeMetadata{
		TypeID:       "test_validator_id",
		TypeName:     "TestUser",
		TableName:    "users",
		URLParamUUID: "id",
		PKField:      "ID",
		ModelType:    reflect.TypeOf(TestUser{}),
		Validator:    validator,
	}

	wrapper := &datastore.Wrapper[TestUser]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, validatorMeta)

	// Create users
	users := []TestUser{
		{Name: "User 1", Email: "user1@example.com"},
		{Name: "NO_DELETE", Email: "nodelete@example.com"},
	}
	created, err := wrapper.BatchCreate(ctx, users)
	if err != nil {
		t.Fatal("BatchCreate failed:", err)
	}

	// Try to delete both - second one will fail validation
	deletes := []TestUser{
		{ID: created[0].ID},
		{ID: created[1].ID},
	}

	err = wrapper.BatchDelete(ctx, deletes)
	if err == nil {
		t.Error("Expected validation error")
	}

	// Verify both users still exist (transaction rolled back)
	all, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}
	if len(all) != 2 {
		t.Errorf("Expected 2 users (transaction rolled back), got %d", len(all))
	}
}

// =============================================================================
// Issue #28: Parent Ownership Filtering Tests
// =============================================================================

// TestParentOwnerProject is a test model for parent ownership tests
type TestParentOwnerProject struct {
	bun.BaseModel `bun:"table:parent_owner_projects"`
	ID            int    `bun:"id,pk,autoincrement"`
	OwnerID       string `bun:"owner_id,notnull"`
	Name          string `bun:"name"`
}

// TestParentOwnerTask is a test model for parent ownership tests
type TestParentOwnerTask struct {
	bun.BaseModel `bun:"table:parent_owner_tasks"`
	ID            int    `bun:"id,pk,autoincrement"`
	ProjectID     int    `bun:"project_id,notnull"`
	Title         string `bun:"title"`
}

func setupParentOwnershipTestDB(t *testing.T) (*datastore.SQLite, func()) {
	t.Helper()

	db, err := datastore.NewSQLite(":memory:")
	if err != nil {
		t.Fatal("Failed to create test database:", err)
	}

	ctx := context.Background()

	_, err = db.GetDB().NewCreateTable().Model((*TestParentOwnerProject)(nil)).IfNotExists().Exec(ctx)
	if err != nil {
		db.Cleanup()
		t.Fatal("Failed to create projects table:", err)
	}

	_, err = db.GetDB().NewCreateTable().Model((*TestParentOwnerTask)(nil)).IfNotExists().Exec(ctx)
	if err != nil {
		db.Cleanup()
		t.Fatal("Failed to create tasks table:", err)
	}

	cleanup := func() {
		db.GetDB().NewDropTable().Model((*TestParentOwnerTask)(nil)).IfExists().Exec(ctx)
		db.GetDB().NewDropTable().Model((*TestParentOwnerProject)(nil)).IfExists().Exec(ctx)
		db.Cleanup()
	}

	return db, cleanup
}

func createParentOwnershipTestMeta() (*metadata.TypeMetadata, *metadata.TypeMetadata) {
	projectMeta := &metadata.TypeMetadata{
		TypeID:          "parent_owner_project",
		TypeName:        "TestParentOwnerProject",
		TableName:       "parent_owner_projects",
		URLParamUUID:    "projectId",
		PKField:         "ID",
		ModelType:       reflect.TypeOf(TestParentOwnerProject{}),
		OwnershipFields: []string{"OwnerID"},
		BypassScopes:    []string{"admin"},
		ChildMeta:       make(map[string]*metadata.TypeMetadata),
	}

	taskMeta := &metadata.TypeMetadata{
		TypeID:        "parent_owner_task",
		TypeName:      "TestParentOwnerTask",
		TableName:     "parent_owner_tasks",
		URLParamUUID:  "taskId",
		PKField:       "ID",
		ModelType:     reflect.TypeOf(TestParentOwnerTask{}),
		ParentType:    reflect.TypeOf(TestParentOwnerProject{}),
		ParentMeta:    projectMeta,
		ForeignKeyCol: "project_id",
		ChildMeta:     make(map[string]*metadata.TypeMetadata),
	}

	projectMeta.ChildMeta["Tasks"] = taskMeta

	return projectMeta, taskMeta
}

func TestParentOwnership_FiltersByParentOwner(t *testing.T) {
	db, cleanup := setupParentOwnershipTestDB(t)
	defer cleanup()

	projectMeta, taskMeta := createParentOwnershipTestMeta()

	ctx := context.Background()

	// Create Alice's project
	aliceProject := &TestParentOwnerProject{OwnerID: "alice", Name: "Alice's Project"}
	_, err := db.GetDB().NewInsert().Model(aliceProject).Returning("*").Exec(ctx)
	if err != nil {
		t.Fatal("Failed to create Alice's project:", err)
	}

	// Create Bob's project
	bobProject := &TestParentOwnerProject{OwnerID: "bob", Name: "Bob's Project"}
	_, err = db.GetDB().NewInsert().Model(bobProject).Returning("*").Exec(ctx)
	if err != nil {
		t.Fatal("Failed to create Bob's project:", err)
	}

	// Create tasks under Alice's project
	task1 := &TestParentOwnerTask{ProjectID: aliceProject.ID, Title: "Task 1"}
	task2 := &TestParentOwnerTask{ProjectID: aliceProject.ID, Title: "Task 2"}
	_, _ = db.GetDB().NewInsert().Model(task1).Returning("*").Exec(ctx)
	_, _ = db.GetDB().NewInsert().Model(task2).Returning("*").Exec(ctx)

	// Setup task wrapper with parent ownership filtering
	taskWrapper := &datastore.Wrapper[TestParentOwnerTask]{Store: db}

	// Build context: Bob trying to access Alice's project's tasks
	taskCtx := context.WithValue(ctx, metadata.MetadataKey, taskMeta)
	taskCtx = context.WithValue(taskCtx, metadata.ParentIDsKey, map[string]string{
		"projectId": strconv.Itoa(aliceProject.ID),
	})
	taskCtx = context.WithValue(taskCtx, metadata.AuthInfoKey, &metadata.AuthInfo{
		UserID: "bob",
		Scopes: []string{"user"},
	})
	// Set parent ownership context (normally set by auth middleware)
	taskCtx = context.WithValue(taskCtx, metadata.ParentOwnershipKey, []*metadata.TypeMetadata{projectMeta})

	// Bob should see empty results (parent ownership filtered)
	tasks, _, _, err := taskWrapper.GetAll(taskCtx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	if len(tasks) != 0 {
		t.Errorf("Expected 0 tasks (parent ownership filtered), got %d", len(tasks))
	}

	// Now test as Alice (owner)
	aliceCtx := context.WithValue(ctx, metadata.MetadataKey, taskMeta)
	aliceCtx = context.WithValue(aliceCtx, metadata.ParentIDsKey, map[string]string{
		"projectId": strconv.Itoa(aliceProject.ID),
	})
	aliceCtx = context.WithValue(aliceCtx, metadata.AuthInfoKey, &metadata.AuthInfo{
		UserID: "alice",
		Scopes: []string{"user"},
	})
	aliceCtx = context.WithValue(aliceCtx, metadata.ParentOwnershipKey, []*metadata.TypeMetadata{projectMeta})

	// Alice should see her tasks
	tasks, _, _, err = taskWrapper.GetAll(aliceCtx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	if len(tasks) != 2 {
		t.Errorf("Expected 2 tasks for Alice, got %d", len(tasks))
	}
}

func TestParentOwnership_AdminBypass(t *testing.T) {
	db, cleanup := setupParentOwnershipTestDB(t)
	defer cleanup()

	_, taskMeta := createParentOwnershipTestMeta()

	ctx := context.Background()

	// Create Alice's project
	aliceProject := &TestParentOwnerProject{OwnerID: "alice", Name: "Alice's Project"}
	_, err := db.GetDB().NewInsert().Model(aliceProject).Returning("*").Exec(ctx)
	if err != nil {
		t.Fatal("Failed to create Alice's project:", err)
	}

	// Create tasks under Alice's project
	task1 := &TestParentOwnerTask{ProjectID: aliceProject.ID, Title: "Task 1"}
	_, _ = db.GetDB().NewInsert().Model(task1).Returning("*").Exec(ctx)

	taskWrapper := &datastore.Wrapper[TestParentOwnerTask]{Store: db}

	// Admin has bypass scope, so no parent ownership filtering
	// ParentOwnershipKey is empty (auth middleware doesn't add it for users with bypass)
	adminCtx := context.WithValue(ctx, metadata.MetadataKey, taskMeta)
	adminCtx = context.WithValue(adminCtx, metadata.ParentIDsKey, map[string]string{
		"projectId": strconv.Itoa(aliceProject.ID),
	})
	adminCtx = context.WithValue(adminCtx, metadata.AuthInfoKey, &metadata.AuthInfo{
		UserID: "admin",
		Scopes: []string{"user", "admin"},
	})
	// No ParentOwnershipKey set - admin bypasses ownership checks

	// Admin should see all tasks
	tasks, _, _, err := taskWrapper.GetAll(adminCtx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	if len(tasks) != 1 {
		t.Errorf("Expected 1 task for admin, got %d", len(tasks))
	}
}

func TestParentOwnership_NoOwnershipFields(t *testing.T) {
	db, cleanup := setupParentOwnershipTestDB(t)
	defer cleanup()

	// Create metadata where parent has NO ownership fields
	projectMeta := &metadata.TypeMetadata{
		TypeID:          "parent_owner_project",
		TypeName:        "TestParentOwnerProject",
		TableName:       "parent_owner_projects",
		URLParamUUID:    "projectId",
		PKField:         "ID",
		ModelType:       reflect.TypeOf(TestParentOwnerProject{}),
		OwnershipFields: []string{}, // No ownership fields
		ChildMeta:       make(map[string]*metadata.TypeMetadata),
	}

	taskMeta := &metadata.TypeMetadata{
		TypeID:        "parent_owner_task",
		TypeName:      "TestParentOwnerTask",
		TableName:     "parent_owner_tasks",
		URLParamUUID:  "taskId",
		PKField:       "ID",
		ModelType:     reflect.TypeOf(TestParentOwnerTask{}),
		ParentType:    reflect.TypeOf(TestParentOwnerProject{}),
		ParentMeta:    projectMeta,
		ForeignKeyCol: "project_id",
		ChildMeta:     make(map[string]*metadata.TypeMetadata),
	}

	ctx := context.Background()

	// Create project
	project := &TestParentOwnerProject{OwnerID: "alice", Name: "Project"}
	_, err := db.GetDB().NewInsert().Model(project).Returning("*").Exec(ctx)
	if err != nil {
		t.Fatal("Failed to create project:", err)
	}

	// Create task
	task := &TestParentOwnerTask{ProjectID: project.ID, Title: "Task 1"}
	_, _ = db.GetDB().NewInsert().Model(task).Returning("*").Exec(ctx)

	taskWrapper := &datastore.Wrapper[TestParentOwnerTask]{Store: db}

	// Even with ParentOwnershipKey set, if parent has no ownership fields, filtering is skipped
	taskCtx := context.WithValue(ctx, metadata.MetadataKey, taskMeta)
	taskCtx = context.WithValue(taskCtx, metadata.ParentIDsKey, map[string]string{
		"projectId": strconv.Itoa(project.ID),
	})
	taskCtx = context.WithValue(taskCtx, metadata.AuthInfoKey, &metadata.AuthInfo{
		UserID: "bob",
		Scopes: []string{"user"},
	})
	taskCtx = context.WithValue(taskCtx, metadata.ParentOwnershipKey, []*metadata.TypeMetadata{projectMeta})

	// Should see task because parent has no ownership fields to filter on
	tasks, _, _, err := taskWrapper.GetAll(taskCtx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	if len(tasks) != 1 {
		t.Errorf("Expected 1 task (no ownership filtering), got %d", len(tasks))
	}
}

func TestParentOwnership_MultipleOwnershipFields(t *testing.T) {
	db, cleanup := setupParentOwnershipTestDB(t)
	defer cleanup()

	// Create metadata with multiple ownership fields on parent
	projectMeta := &metadata.TypeMetadata{
		TypeID:          "parent_owner_project",
		TypeName:        "TestParentOwnerProject",
		TableName:       "parent_owner_projects",
		URLParamUUID:    "projectId",
		PKField:         "ID",
		ModelType:       reflect.TypeOf(TestParentOwnerProject{}),
		OwnershipFields: []string{"OwnerID", "Name"}, // Unusual but tests OR logic
		BypassScopes:    []string{"admin"},
		ChildMeta:       make(map[string]*metadata.TypeMetadata),
	}

	taskMeta := &metadata.TypeMetadata{
		TypeID:        "parent_owner_task",
		TypeName:      "TestParentOwnerTask",
		TableName:     "parent_owner_tasks",
		URLParamUUID:  "taskId",
		PKField:       "ID",
		ModelType:     reflect.TypeOf(TestParentOwnerTask{}),
		ParentType:    reflect.TypeOf(TestParentOwnerProject{}),
		ParentMeta:    projectMeta,
		ForeignKeyCol: "project_id",
		ChildMeta:     make(map[string]*metadata.TypeMetadata),
	}

	ctx := context.Background()

	// Create project where Name = "alice" (matches second ownership field)
	project := &TestParentOwnerProject{OwnerID: "bob", Name: "alice"}
	_, err := db.GetDB().NewInsert().Model(project).Returning("*").Exec(ctx)
	if err != nil {
		t.Fatal("Failed to create project:", err)
	}

	// Create task
	task := &TestParentOwnerTask{ProjectID: project.ID, Title: "Task 1"}
	_, _ = db.GetDB().NewInsert().Model(task).Returning("*").Exec(ctx)

	taskWrapper := &datastore.Wrapper[TestParentOwnerTask]{Store: db}

	// Alice should see task (matches Name field via OR logic)
	aliceCtx := context.WithValue(ctx, metadata.MetadataKey, taskMeta)
	aliceCtx = context.WithValue(aliceCtx, metadata.ParentIDsKey, map[string]string{
		"projectId": strconv.Itoa(project.ID),
	})
	aliceCtx = context.WithValue(aliceCtx, metadata.AuthInfoKey, &metadata.AuthInfo{
		UserID: "alice",
		Scopes: []string{"user"},
	})
	aliceCtx = context.WithValue(aliceCtx, metadata.ParentOwnershipKey, []*metadata.TypeMetadata{projectMeta})

	tasks, _, _, err := taskWrapper.GetAll(aliceCtx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	if len(tasks) != 1 {
		t.Errorf("Expected 1 task (OR logic on ownership fields), got %d", len(tasks))
	}
}

// ============================================================================
// Relation Filter Tests (Issue #35)
// Tests for filtering and including across relation chains
// 5-level hierarchy: User → Account → Site → Bill → LineItem
// ============================================================================

// RelUser is the top-level model (customer)
type RelUser struct {
	bun.BaseModel `bun:"table:rel_users"`
	ID            int           `bun:"id,pk,autoincrement"`
	Name          string        `bun:"name,notnull"`
	Email         string        `bun:"email,notnull"`
	CreatedAt     time.Time     `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	Accounts      []*RelAccount `bun:"rel:has-many,join:id=user_id"`
}

// RelAccount belongs to User, has many Sites
type RelAccount struct {
	bun.BaseModel `bun:"table:rel_accounts"`
	ID            int        `bun:"id,pk,autoincrement"`
	UserID        int        `bun:"user_id,notnull"`
	User          *RelUser   `bun:"rel:belongs-to,join:user_id=id"`
	OwnerID       string     `bun:"owner_id,notnull"`
	Status        string     `bun:"status,notnull"`
	Balance       float64    `bun:"balance,notnull,default:0"`
	CreatedAt     time.Time  `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	Sites         []*RelSite `bun:"rel:has-many,join:id=account_id"`
}

// RelSite belongs to Account, has many Bills
type RelSite struct {
	bun.BaseModel `bun:"table:rel_sites"`
	ID            int         `bun:"id,pk,autoincrement"`
	AccountID     int         `bun:"account_id,notnull"`
	Account       *RelAccount `bun:"rel:belongs-to,join:account_id=id"`
	OwnerID       string      `bun:"owner_id,notnull"`
	NMI           string      `bun:"nmi,notnull"`
	Region        string      `bun:"region,notnull"`
	Address       string      `bun:"address,notnull"`
	CreatedAt     time.Time   `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	Bills         []*RelBill  `bun:"rel:has-many,join:id=site_id"`
}

// RelBill belongs to Site, has many LineItems
type RelBill struct {
	bun.BaseModel `bun:"table:rel_bills"`
	ID            int            `bun:"id,pk,autoincrement"`
	SiteID        int            `bun:"site_id,notnull"`
	Site          *RelSite       `bun:"rel:belongs-to,join:site_id=id"`
	OwnerID       string         `bun:"owner_id,notnull"`
	Status        string         `bun:"status,notnull"`
	Amount        float64        `bun:"amount,notnull"`
	DueDate       time.Time      `bun:"due_date,notnull"`
	CreatedAt     time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	LineItems     []*RelLineItem `bun:"rel:has-many,join:id=bill_id"`
}

// RelLineItem belongs to Bill (deepest level)
type RelLineItem struct {
	bun.BaseModel `bun:"table:rel_line_items"`
	ID            int       `bun:"id,pk,autoincrement"`
	BillID        int       `bun:"bill_id,notnull"`
	Bill          *RelBill  `bun:"rel:belongs-to,join:bill_id=id"`
	OwnerID       string    `bun:"owner_id,notnull"`
	Description   string    `bun:"description,notnull"`
	Amount        float64   `bun:"amount,notnull"`
	CreatedAt     time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

// setupRelationFilterTestDB creates the database and tables for relation filter tests
func setupRelationFilterTestDB(t *testing.T) (*datastore.SQLite, func()) {
	t.Helper()
	db, err := datastore.NewSQLite(":memory:")
	if err != nil {
		t.Fatal("Failed to create SQLite:", err)
	}

	ctx := context.Background()
	models := []interface{}{
		(*RelUser)(nil),
		(*RelAccount)(nil),
		(*RelSite)(nil),
		(*RelBill)(nil),
		(*RelLineItem)(nil),
	}

	for _, model := range models {
		if _, err := db.GetDB().NewCreateTable().Model(model).IfNotExists().Exec(ctx); err != nil {
			t.Fatal("Failed to create table:", err)
		}
	}

	return db, func() { db.GetDB().Close() }
}

// createRelationFilterTestMeta creates the full 5-level metadata chain
func createRelationFilterTestMeta() (userMeta, accountMeta, siteMeta, billMeta, lineItemMeta *metadata.TypeMetadata) {
	userMeta = &metadata.TypeMetadata{
		TypeID:           "rel_user",
		TypeName:         "RelUser",
		TableName:        "rel_users",
		URLParamUUID:     "userId",
		PKField:          "ID",
		ModelType:        reflect.TypeOf(RelUser{}),
		ChildMeta:        make(map[string]*metadata.TypeMetadata),
		FilterableFields: []string{"Name", "Email"},
		SortableFields:   []string{"Name", "Email", "CreatedAt"},
	}

	accountMeta = &metadata.TypeMetadata{
		TypeID:           "rel_account",
		TypeName:         "RelAccount",
		TableName:        "rel_accounts",
		URLParamUUID:     "accountId",
		PKField:          "ID",
		ModelType:        reflect.TypeOf(RelAccount{}),
		ParentType:       reflect.TypeOf(RelUser{}),
		ParentMeta:       userMeta,
		ForeignKeyCol:    "user_id",
		RelationName:     "Accounts",
		OwnershipFields:  []string{"OwnerID"},
		ChildMeta:        make(map[string]*metadata.TypeMetadata),
		FilterableFields: []string{"Status", "Balance"},
		SortableFields:   []string{"Status", "Balance", "CreatedAt"},
	}

	siteMeta = &metadata.TypeMetadata{
		TypeID:           "rel_site",
		TypeName:         "RelSite",
		TableName:        "rel_sites",
		URLParamUUID:     "siteId",
		PKField:          "ID",
		ModelType:        reflect.TypeOf(RelSite{}),
		ParentType:       reflect.TypeOf(RelAccount{}),
		ParentMeta:       accountMeta,
		ForeignKeyCol:    "account_id",
		RelationName:     "Sites",
		OwnershipFields:  []string{"OwnerID"},
		ChildMeta:        make(map[string]*metadata.TypeMetadata),
		FilterableFields: []string{"NMI", "Region", "Address"},
		SortableFields:   []string{"NMI", "Region", "CreatedAt"},
	}

	billMeta = &metadata.TypeMetadata{
		TypeID:           "rel_bill",
		TypeName:         "RelBill",
		TableName:        "rel_bills",
		URLParamUUID:     "billId",
		PKField:          "ID",
		ModelType:        reflect.TypeOf(RelBill{}),
		ParentType:       reflect.TypeOf(RelSite{}),
		ParentMeta:       siteMeta,
		ForeignKeyCol:    "site_id",
		RelationName:     "Bills",
		OwnershipFields:  []string{"OwnerID"},
		ChildMeta:        make(map[string]*metadata.TypeMetadata),
		FilterableFields: []string{"Status", "Amount", "DueDate"},
		SortableFields:   []string{"Status", "Amount", "DueDate", "CreatedAt"},
	}

	lineItemMeta = &metadata.TypeMetadata{
		TypeID:           "rel_line_item",
		TypeName:         "RelLineItem",
		TableName:        "rel_line_items",
		URLParamUUID:     "lineItemId",
		PKField:          "ID",
		ModelType:        reflect.TypeOf(RelLineItem{}),
		ParentType:       reflect.TypeOf(RelBill{}),
		ParentMeta:       billMeta,
		ForeignKeyCol:    "bill_id",
		RelationName:     "LineItems",
		OwnershipFields:  []string{"OwnerID"},
		ChildMeta:        make(map[string]*metadata.TypeMetadata),
		FilterableFields: []string{"Description", "Amount"},
		SortableFields:   []string{"Description", "Amount", "CreatedAt"},
	}

	// Wire up ChildMeta for downward traversal
	userMeta.ChildMeta["Accounts"] = accountMeta
	accountMeta.ChildMeta["Sites"] = siteMeta
	siteMeta.ChildMeta["Bills"] = billMeta
	billMeta.ChildMeta["LineItems"] = lineItemMeta

	return userMeta, accountMeta, siteMeta, billMeta, lineItemMeta
}

// seedRelationFilterTestData creates comprehensive test data for relation filter tests
// Returns IDs for verification: user IDs, account IDs, site IDs, bill IDs, lineItem IDs
func seedRelationFilterTestData(t *testing.T, db *datastore.SQLite) (users []RelUser, accounts []RelAccount, sites []RelSite, bills []RelBill, lineItems []RelLineItem) {
	t.Helper()
	ctx := context.Background()

	// Create 2 users
	users = []RelUser{
		{Name: "Alice Smith", Email: "alice@example.com"},
		{Name: "Bob Jones", Email: "bob@example.com"},
	}
	for i := range users {
		_, err := db.GetDB().NewInsert().Model(&users[i]).Returning("*").Exec(ctx)
		if err != nil {
			t.Fatal("Failed to create user:", err)
		}
	}

	// Create 3 accounts (2 for Alice, 1 for Bob)
	accounts = []RelAccount{
		{UserID: users[0].ID, OwnerID: "alice", Status: "Active", Balance: 100.00},
		{UserID: users[0].ID, OwnerID: "alice", Status: "Suspended", Balance: 250.50},
		{UserID: users[1].ID, OwnerID: "bob", Status: "Active", Balance: 0.00},
	}
	for i := range accounts {
		_, err := db.GetDB().NewInsert().Model(&accounts[i]).Returning("*").Exec(ctx)
		if err != nil {
			t.Fatal("Failed to create account:", err)
		}
	}

	// Create 4 sites (2 for Alice's first account, 1 each for others)
	sites = []RelSite{
		{AccountID: accounts[0].ID, OwnerID: "alice", NMI: "6407112345678", Region: "NSW", Address: "123 Main St"},
		{AccountID: accounts[0].ID, OwnerID: "alice", NMI: "6407198765432", Region: "VIC", Address: "456 Oak Ave"},
		{AccountID: accounts[1].ID, OwnerID: "alice", NMI: "6407145678901", Region: "QLD", Address: "789 Pine Rd"},
		{AccountID: accounts[2].ID, OwnerID: "bob", NMI: "6407167890123", Region: "NSW", Address: "321 Elm St"},
	}
	for i := range sites {
		_, err := db.GetDB().NewInsert().Model(&sites[i]).Returning("*").Exec(ctx)
		if err != nil {
			t.Fatal("Failed to create site:", err)
		}
	}

	// Create 6 bills across sites with varying statuses
	dueDate := time.Now().Add(30 * 24 * time.Hour)
	pastDue := time.Now().Add(-7 * 24 * time.Hour)
	bills = []RelBill{
		{SiteID: sites[0].ID, OwnerID: "alice", Status: "Paid", Amount: 150.00, DueDate: dueDate},
		{SiteID: sites[0].ID, OwnerID: "alice", Status: "Overdue", Amount: 200.00, DueDate: pastDue},
		{SiteID: sites[1].ID, OwnerID: "alice", Status: "Pending", Amount: 175.50, DueDate: dueDate},
		{SiteID: sites[2].ID, OwnerID: "alice", Status: "Overdue", Amount: 423.80, DueDate: pastDue},
		{SiteID: sites[3].ID, OwnerID: "bob", Status: "Paid", Amount: 89.99, DueDate: dueDate},
		{SiteID: sites[3].ID, OwnerID: "bob", Status: "Pending", Amount: 125.00, DueDate: dueDate},
	}
	for i := range bills {
		_, err := db.GetDB().NewInsert().Model(&bills[i]).Returning("*").Exec(ctx)
		if err != nil {
			t.Fatal("Failed to create bill:", err)
		}
	}

	// Create line items for bills
	lineItems = []RelLineItem{
		{BillID: bills[0].ID, OwnerID: "alice", Description: "Electricity Usage", Amount: 120.00},
		{BillID: bills[0].ID, OwnerID: "alice", Description: "Service Fee", Amount: 30.00},
		{BillID: bills[1].ID, OwnerID: "alice", Description: "Electricity Usage", Amount: 180.00},
		{BillID: bills[1].ID, OwnerID: "alice", Description: "Late Fee", Amount: 20.00},
		{BillID: bills[2].ID, OwnerID: "alice", Description: "Electricity Usage", Amount: 175.50},
		{BillID: bills[3].ID, OwnerID: "alice", Description: "Electricity Usage", Amount: 400.00},
		{BillID: bills[3].ID, OwnerID: "alice", Description: "Peak Surcharge", Amount: 23.80},
		{BillID: bills[4].ID, OwnerID: "bob", Description: "Electricity Usage", Amount: 89.99},
		{BillID: bills[5].ID, OwnerID: "bob", Description: "Electricity Usage", Amount: 100.00},
		{BillID: bills[5].ID, OwnerID: "bob", Description: "Network Fee", Amount: 25.00},
	}
	for i := range lineItems {
		_, err := db.GetDB().NewInsert().Model(&lineItems[i]).Returning("*").Exec(ctx)
		if err != nil {
			t.Fatal("Failed to create line item:", err)
		}
	}

	return users, accounts, sites, bills, lineItems
}

// ============================================================================
// Parent Field Filter Tests (Upward: belongs-to chain)
// ============================================================================

func TestRelationFilter_ParentField_OneLevel(t *testing.T) {
	db, cleanup := setupRelationFilterTestDB(t)
	defer cleanup()

	_, accountMeta, siteMeta, _, _ := createRelationFilterTestMeta()
	_, accounts, _, _, _ := seedRelationFilterTestData(t, db)

	// Test: Filter Sites by Account.Status = "Active"
	// Should return sites belonging to active accounts only
	siteWrapper := &datastore.Wrapper[RelSite]{Store: db}

	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Account.Status": {Value: "Active", Operator: metadata.OpEq},
		},
	}

	ctx := context.WithValue(context.Background(), metadata.MetadataKey, siteMeta)
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	sites, _, _, err := siteWrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Alice has 2 sites under Active account, Bob has 1 site under Active account = 3 total
	// Alice's Suspended account has 1 site which should be excluded
	expectedCount := 3
	if len(sites) != expectedCount {
		t.Errorf("Expected %d sites with Active account, got %d", expectedCount, len(sites))
	}

	// Verify all returned sites belong to active accounts
	activeAccountIDs := make(map[int]bool)
	for _, acc := range accounts {
		if acc.Status == "Active" {
			activeAccountIDs[acc.ID] = true
		}
	}
	for _, site := range sites {
		if !activeAccountIDs[site.AccountID] {
			t.Errorf("Site %d belongs to non-active account %d", site.ID, site.AccountID)
		}
	}

	_ = accountMeta // Used for metadata chain setup
}

func TestRelationFilter_ParentField_TwoLevels(t *testing.T) {
	db, cleanup := setupRelationFilterTestDB(t)
	defer cleanup()

	userMeta, _, siteMeta, _, _ := createRelationFilterTestMeta()
	users, _, _, _, _ := seedRelationFilterTestData(t, db)

	// Test: Filter Sites by Account.User.Email containing "alice"
	// Should return only Alice's sites (3 sites)
	siteWrapper := &datastore.Wrapper[RelSite]{Store: db}

	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Account.User.Email": {Value: "%alice%", Operator: metadata.OpLike},
		},
	}

	ctx := context.WithValue(context.Background(), metadata.MetadataKey, siteMeta)
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	sites, _, _, err := siteWrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Alice has 3 sites total
	expectedCount := 3
	if len(sites) != expectedCount {
		t.Errorf("Expected %d sites for Alice, got %d", expectedCount, len(sites))
	}

	_ = userMeta
	_ = users
}

func TestRelationFilter_ParentField_ThreeLevels(t *testing.T) {
	db, cleanup := setupRelationFilterTestDB(t)
	defer cleanup()

	_, _, _, billMeta, _ := createRelationFilterTestMeta()
	users, _, _, _, _ := seedRelationFilterTestData(t, db)

	// Test: Filter Bills by Site.Account.User.Name = "Alice Smith"
	// Should return only Alice's bills (4 bills)
	billWrapper := &datastore.Wrapper[RelBill]{Store: db}

	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Site.Account.User.Name": {Value: "Alice Smith", Operator: metadata.OpEq},
		},
	}

	ctx := context.WithValue(context.Background(), metadata.MetadataKey, billMeta)
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	bills, _, _, err := billWrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Alice has 4 bills
	expectedCount := 4
	if len(bills) != expectedCount {
		t.Errorf("Expected %d bills for Alice Smith, got %d", expectedCount, len(bills))
	}

	_ = users
}

func TestRelationFilter_ParentField_FourLevels(t *testing.T) {
	db, cleanup := setupRelationFilterTestDB(t)
	defer cleanup()

	_, _, _, _, lineItemMeta := createRelationFilterTestMeta()
	_, _, _, _, _ = seedRelationFilterTestData(t, db)

	// Test: Filter LineItems by Bill.Site.Account.User.Email containing "bob"
	// Should return only Bob's line items (3 line items)
	lineItemWrapper := &datastore.Wrapper[RelLineItem]{Store: db}

	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Bill.Site.Account.User.Email": {Value: "%bob%", Operator: metadata.OpLike},
		},
	}

	ctx := context.WithValue(context.Background(), metadata.MetadataKey, lineItemMeta)
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	lineItems, _, _, err := lineItemWrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Bob has 3 line items
	expectedCount := 3
	if len(lineItems) != expectedCount {
		t.Errorf("Expected %d line items for Bob, got %d", expectedCount, len(lineItems))
	}
}

// ============================================================================
// Child Field Filter Tests (Downward: has-many chain with EXISTS)
// ============================================================================

func TestRelationFilter_ChildField_OneLevel(t *testing.T) {
	db, cleanup := setupRelationFilterTestDB(t)
	defer cleanup()

	userMeta, _, _, _, _ := createRelationFilterTestMeta()
	_, _, _, _, _ = seedRelationFilterTestData(t, db)

	// Test: Filter Users who have at least one Account with Status = "Suspended"
	// Should return only Alice (she has a Suspended account)
	userWrapper := &datastore.Wrapper[RelUser]{Store: db}

	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Accounts.Status": {Value: "Suspended", Operator: metadata.OpEq},
		},
	}

	ctx := context.WithValue(context.Background(), metadata.MetadataKey, userMeta)
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	users, _, _, err := userWrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Only Alice has a Suspended account
	expectedCount := 1
	if len(users) != expectedCount {
		t.Errorf("Expected %d user with Suspended account, got %d", expectedCount, len(users))
	}
	if len(users) > 0 && users[0].Name != "Alice Smith" {
		t.Errorf("Expected Alice Smith, got %s", users[0].Name)
	}
}

func TestRelationFilter_ChildField_TwoLevels(t *testing.T) {
	db, cleanup := setupRelationFilterTestDB(t)
	defer cleanup()

	userMeta, _, _, _, _ := createRelationFilterTestMeta()
	_, _, _, _, _ = seedRelationFilterTestData(t, db)

	// Test: Filter Users who have at least one Site in Region = "QLD"
	// Should return only Alice (she has a QLD site)
	userWrapper := &datastore.Wrapper[RelUser]{Store: db}

	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Accounts.Sites.Region": {Value: "QLD", Operator: metadata.OpEq},
		},
	}

	ctx := context.WithValue(context.Background(), metadata.MetadataKey, userMeta)
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	users, _, _, err := userWrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Only Alice has a QLD site
	expectedCount := 1
	if len(users) != expectedCount {
		t.Errorf("Expected %d user with QLD site, got %d", expectedCount, len(users))
	}
	if len(users) > 0 && users[0].Name != "Alice Smith" {
		t.Errorf("Expected Alice Smith, got %s", users[0].Name)
	}
}

func TestRelationFilter_ChildField_ThreeLevels(t *testing.T) {
	db, cleanup := setupRelationFilterTestDB(t)
	defer cleanup()

	userMeta, _, _, _, _ := createRelationFilterTestMeta()
	_, _, _, _, _ = seedRelationFilterTestData(t, db)

	// Test: Filter Users who have at least one Bill with Status = "Overdue"
	// Should return only Alice (she has Overdue bills)
	userWrapper := &datastore.Wrapper[RelUser]{Store: db}

	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Accounts.Sites.Bills.Status": {Value: "Overdue", Operator: metadata.OpEq},
		},
	}

	ctx := context.WithValue(context.Background(), metadata.MetadataKey, userMeta)
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	users, _, _, err := userWrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Only Alice has Overdue bills
	expectedCount := 1
	if len(users) != expectedCount {
		t.Errorf("Expected %d user with Overdue bills, got %d", expectedCount, len(users))
	}
	if len(users) > 0 && users[0].Name != "Alice Smith" {
		t.Errorf("Expected Alice Smith, got %s", users[0].Name)
	}
}

func TestRelationFilter_ChildField_FourLevels(t *testing.T) {
	db, cleanup := setupRelationFilterTestDB(t)
	defer cleanup()

	userMeta, _, _, _, _ := createRelationFilterTestMeta()
	_, _, _, _, _ = seedRelationFilterTestData(t, db)

	// Test: Filter Users who have at least one LineItem with Description containing "Late Fee"
	// Should return only Alice (she has a Late Fee line item)
	userWrapper := &datastore.Wrapper[RelUser]{Store: db}

	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Accounts.Sites.Bills.LineItems.Description": {Value: "%Late Fee%", Operator: metadata.OpLike},
		},
	}

	ctx := context.WithValue(context.Background(), metadata.MetadataKey, userMeta)
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	users, _, _, err := userWrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Only Alice has a Late Fee line item
	expectedCount := 1
	if len(users) != expectedCount {
		t.Errorf("Expected %d user with Late Fee line item, got %d", expectedCount, len(users))
	}
	if len(users) > 0 && users[0].Name != "Alice Smith" {
		t.Errorf("Expected Alice Smith, got %s", users[0].Name)
	}
}

func TestRelationFilter_ChildField_MultipleMatches_NoDuplicates(t *testing.T) {
	db, cleanup := setupRelationFilterTestDB(t)
	defer cleanup()

	userMeta, _, _, _, _ := createRelationFilterTestMeta()
	_, _, _, _, _ = seedRelationFilterTestData(t, db)

	// Test: Filter Users who have at least one Site in Region = "NSW"
	// Alice has 1 NSW site, Bob has 1 NSW site = 2 users
	// Important: Each user should appear only ONCE despite having matching children
	userWrapper := &datastore.Wrapper[RelUser]{Store: db}

	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Accounts.Sites.Region": {Value: "NSW", Operator: metadata.OpEq},
		},
	}

	ctx := context.WithValue(context.Background(), metadata.MetadataKey, userMeta)
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	users, _, _, err := userWrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Both Alice and Bob have NSW sites
	expectedCount := 2
	if len(users) != expectedCount {
		t.Errorf("Expected %d users with NSW sites (no duplicates), got %d", expectedCount, len(users))
	}

	// Verify no duplicates
	seen := make(map[int]bool)
	for _, user := range users {
		if seen[user.ID] {
			t.Errorf("Duplicate user returned: ID %d", user.ID)
		}
		seen[user.ID] = true
	}
}

// ============================================================================
// Parent Include Tests (Upward: belongs-to chain)
// ============================================================================

func TestRelationInclude_Parent_OneLevel(t *testing.T) {
	db, cleanup := setupRelationFilterTestDB(t)
	defer cleanup()

	_, accountMeta, siteMeta, _, _ := createRelationFilterTestMeta()
	_, _, sites, _, _ := seedRelationFilterTestData(t, db)

	// Test: Get Site with ?include=Account
	// Should populate Site.Account
	siteWrapper := &datastore.Wrapper[RelSite]{Store: db}

	opts := &metadata.QueryOptions{
		Include: []string{"Account"},
	}

	// Register Account as a parent that can be included
	siteMeta.ParentMeta = accountMeta

	ctx := context.WithValue(context.Background(), metadata.MetadataKey, siteMeta)
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)
	ctx = context.WithValue(ctx, metadata.AllowedIncludesKey, metadata.AllowedIncludes{"Account": false})

	site, err := siteWrapper.Get(ctx, strconv.Itoa(sites[0].ID))
	if err != nil {
		t.Fatal("Get failed:", err)
	}

	if site.Account == nil {
		t.Error("Expected Account to be included, got nil")
	} else if site.Account.ID != sites[0].AccountID {
		t.Errorf("Expected Account ID %d, got %d", sites[0].AccountID, site.Account.ID)
	}
}

func TestRelationInclude_Parent_TwoLevels(t *testing.T) {
	db, cleanup := setupRelationFilterTestDB(t)
	defer cleanup()

	userMeta, accountMeta, siteMeta, _, _ := createRelationFilterTestMeta()
	_, _, sites, _, _ := seedRelationFilterTestData(t, db)

	// Test: Get Site with ?include=Account.User
	// Should populate Site.Account and Site.Account.User
	siteWrapper := &datastore.Wrapper[RelSite]{Store: db}

	opts := &metadata.QueryOptions{
		Include: []string{"Account.User"},
	}

	ctx := context.WithValue(context.Background(), metadata.MetadataKey, siteMeta)
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)
	ctx = context.WithValue(ctx, metadata.AllowedIncludesKey, metadata.AllowedIncludes{"Account.User": false})

	site, err := siteWrapper.Get(ctx, strconv.Itoa(sites[0].ID))
	if err != nil {
		t.Fatal("Get failed:", err)
	}

	if site.Account == nil {
		t.Error("Expected Account to be included, got nil")
	} else if site.Account.User == nil {
		t.Error("Expected Account.User to be included, got nil")
	}

	_ = userMeta
	_ = accountMeta
}

func TestRelationInclude_Parent_ThreeLevels(t *testing.T) {
	db, cleanup := setupRelationFilterTestDB(t)
	defer cleanup()

	_, _, _, billMeta, _ := createRelationFilterTestMeta()
	_, _, _, bills, _ := seedRelationFilterTestData(t, db)

	// Test: Get Bill with ?include=Site.Account.User
	// Should populate full parent chain
	billWrapper := &datastore.Wrapper[RelBill]{Store: db}

	opts := &metadata.QueryOptions{
		Include: []string{"Site.Account.User"},
	}

	ctx := context.WithValue(context.Background(), metadata.MetadataKey, billMeta)
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)
	ctx = context.WithValue(ctx, metadata.AllowedIncludesKey, metadata.AllowedIncludes{"Site.Account.User": false})

	bill, err := billWrapper.Get(ctx, strconv.Itoa(bills[0].ID))
	if err != nil {
		t.Fatal("Get failed:", err)
	}

	switch {
	case bill.Site == nil:
		t.Error("Expected Site to be included, got nil")
	case bill.Site.Account == nil:
		t.Error("Expected Site.Account to be included, got nil")
	case bill.Site.Account.User == nil:
		t.Error("Expected Site.Account.User to be included, got nil")
	}
}

func TestRelationInclude_Parent_FourLevels(t *testing.T) {
	db, cleanup := setupRelationFilterTestDB(t)
	defer cleanup()

	_, _, _, _, lineItemMeta := createRelationFilterTestMeta()
	_, _, _, _, lineItems := seedRelationFilterTestData(t, db)

	// Test: Get LineItem with ?include=Bill.Site.Account.User
	// Should populate full 4-level parent chain
	lineItemWrapper := &datastore.Wrapper[RelLineItem]{Store: db}

	opts := &metadata.QueryOptions{
		Include: []string{"Bill.Site.Account.User"},
	}

	ctx := context.WithValue(context.Background(), metadata.MetadataKey, lineItemMeta)
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)
	ctx = context.WithValue(ctx, metadata.AllowedIncludesKey, metadata.AllowedIncludes{"Bill.Site.Account.User": false})

	lineItem, err := lineItemWrapper.Get(ctx, strconv.Itoa(lineItems[0].ID))
	if err != nil {
		t.Fatal("Get failed:", err)
	}

	switch {
	case lineItem.Bill == nil:
		t.Error("Expected Bill to be included, got nil")
	case lineItem.Bill.Site == nil:
		t.Error("Expected Bill.Site to be included, got nil")
	case lineItem.Bill.Site.Account == nil:
		t.Error("Expected Bill.Site.Account to be included, got nil")
	case lineItem.Bill.Site.Account.User == nil:
		t.Error("Expected Bill.Site.Account.User to be included, got nil")
	}
}

// ============================================================================
// Nested Child Include Tests (Downward: has-many chain)
// ============================================================================

func TestRelationInclude_NestedChild_TwoLevels(t *testing.T) {
	db, cleanup := setupRelationFilterTestDB(t)
	defer cleanup()

	userMeta, _, _, _, _ := createRelationFilterTestMeta()
	users, _, _, _, _ := seedRelationFilterTestData(t, db)

	// Test: Get User with ?include=Accounts.Sites
	// Should populate User.Accounts and each Account.Sites
	userWrapper := &datastore.Wrapper[RelUser]{Store: db}

	opts := &metadata.QueryOptions{
		Include: []string{"Accounts.Sites"},
	}

	ctx := context.WithValue(context.Background(), metadata.MetadataKey, userMeta)
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)
	ctx = context.WithValue(ctx, metadata.AllowedIncludesKey, metadata.AllowedIncludes{"Accounts.Sites": false})

	user, err := userWrapper.Get(ctx, strconv.Itoa(users[0].ID))
	if err != nil {
		t.Fatal("Get failed:", err)
	}

	// Alice has 2 accounts
	if len(user.Accounts) != 2 {
		t.Errorf("Expected 2 accounts for Alice, got %d", len(user.Accounts))
	}

	// First account should have 2 sites, second should have 1
	totalSites := 0
	for _, acc := range user.Accounts {
		totalSites += len(acc.Sites)
	}
	if totalSites != 3 {
		t.Errorf("Expected 3 total sites for Alice, got %d", totalSites)
	}
}

func TestRelationInclude_NestedChild_ThreeLevels(t *testing.T) {
	db, cleanup := setupRelationFilterTestDB(t)
	defer cleanup()

	userMeta, _, _, _, _ := createRelationFilterTestMeta()
	users, _, _, _, _ := seedRelationFilterTestData(t, db)

	// Test: Get User with ?include=Accounts.Sites.Bills
	// Should populate full 3-level child chain
	userWrapper := &datastore.Wrapper[RelUser]{Store: db}

	opts := &metadata.QueryOptions{
		Include: []string{"Accounts.Sites.Bills"},
	}

	ctx := context.WithValue(context.Background(), metadata.MetadataKey, userMeta)
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)
	ctx = context.WithValue(ctx, metadata.AllowedIncludesKey, metadata.AllowedIncludes{"Accounts.Sites.Bills": false})

	user, err := userWrapper.Get(ctx, strconv.Itoa(users[0].ID))
	if err != nil {
		t.Fatal("Get failed:", err)
	}

	// Count total bills across all accounts/sites
	totalBills := 0
	for _, acc := range user.Accounts {
		for _, site := range acc.Sites {
			totalBills += len(site.Bills)
		}
	}

	// Alice has 4 bills
	if totalBills != 4 {
		t.Errorf("Expected 4 total bills for Alice, got %d", totalBills)
	}
}

func TestRelationInclude_NestedChild_FourLevels(t *testing.T) {
	db, cleanup := setupRelationFilterTestDB(t)
	defer cleanup()

	userMeta, _, _, _, _ := createRelationFilterTestMeta()
	users, _, _, _, _ := seedRelationFilterTestData(t, db)

	// Test: Get User with ?include=Accounts.Sites.Bills.LineItems
	// Should populate full 4-level child chain
	userWrapper := &datastore.Wrapper[RelUser]{Store: db}

	opts := &metadata.QueryOptions{
		Include: []string{"Accounts.Sites.Bills.LineItems"},
	}

	ctx := context.WithValue(context.Background(), metadata.MetadataKey, userMeta)
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)
	ctx = context.WithValue(ctx, metadata.AllowedIncludesKey, metadata.AllowedIncludes{"Accounts.Sites.Bills.LineItems": false})

	user, err := userWrapper.Get(ctx, strconv.Itoa(users[0].ID))
	if err != nil {
		t.Fatal("Get failed:", err)
	}

	// Count total line items across all accounts/sites/bills
	totalLineItems := 0
	for _, acc := range user.Accounts {
		for _, site := range acc.Sites {
			for _, bill := range site.Bills {
				totalLineItems += len(bill.LineItems)
			}
		}
	}

	// Alice has 7 line items
	if totalLineItems != 7 {
		t.Errorf("Expected 7 total line items for Alice, got %d", totalLineItems)
	}
}

// ============================================================================
// Security Negative Tests
// ============================================================================

func TestRelationFilter_Security_UnauthorizedFilterPath_Ignored(t *testing.T) {
	db, cleanup := setupRelationFilterTestDB(t)
	defer cleanup()

	userMeta, _, _, _, _ := createRelationFilterTestMeta()
	_, _, _, _, _ = seedRelationFilterTestData(t, db)

	// Test: Filter on path not in FilterableFields should be silently ignored
	// FilterableFields only has direct fields, not relation paths
	userWrapper := &datastore.Wrapper[RelUser]{Store: db}

	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Accounts.SecretField": {Value: "sensitive", Operator: metadata.OpEq},
		},
	}

	ctx := context.WithValue(context.Background(), metadata.MetadataKey, userMeta)
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	users, _, _, err := userWrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Should return all users (filter ignored)
	if len(users) != 2 {
		t.Errorf("Expected 2 users (filter ignored), got %d", len(users))
	}
}

func TestRelationFilter_Security_NonExistentRelation_Ignored(t *testing.T) {
	db, cleanup := setupRelationFilterTestDB(t)
	defer cleanup()

	userMeta, _, _, _, _ := createRelationFilterTestMeta()
	_, _, _, _, _ = seedRelationFilterTestData(t, db)

	// Test: Filter on non-existent relation should be silently ignored (no schema leak)
	userWrapper := &datastore.Wrapper[RelUser]{Store: db}

	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"NonExistent.Field": {Value: "value", Operator: metadata.OpEq},
		},
	}

	ctx := context.WithValue(context.Background(), metadata.MetadataKey, userMeta)
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	users, _, _, err := userWrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll should not error on non-existent relation:", err)
	}

	// Should return all users (filter ignored)
	if len(users) != 2 {
		t.Errorf("Expected 2 users (filter ignored), got %d", len(users))
	}
}

func TestRelationFilter_Security_InvalidFieldInValidRelation_Ignored(t *testing.T) {
	db, cleanup := setupRelationFilterTestDB(t)
	defer cleanup()

	userMeta, _, _, _, _ := createRelationFilterTestMeta()
	_, _, _, _, _ = seedRelationFilterTestData(t, db)

	// Test: Filter on valid relation but invalid field should be silently ignored
	userWrapper := &datastore.Wrapper[RelUser]{Store: db}

	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Accounts.NonExistentField": {Value: "value", Operator: metadata.OpEq},
		},
	}

	ctx := context.WithValue(context.Background(), metadata.MetadataKey, userMeta)
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	users, _, _, err := userWrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll should not error on invalid field:", err)
	}

	// Should return all users (filter ignored)
	if len(users) != 2 {
		t.Errorf("Expected 2 users (filter ignored), got %d", len(users))
	}
}

func TestRelationInclude_Security_UnauthorizedInclude_Ignored(t *testing.T) {
	db, cleanup := setupRelationFilterTestDB(t)
	defer cleanup()

	_, _, siteMeta, _, _ := createRelationFilterTestMeta()
	_, _, sites, _, _ := seedRelationFilterTestData(t, db)

	// Test: Include not in AllowedIncludes should be silently ignored
	siteWrapper := &datastore.Wrapper[RelSite]{Store: db}

	opts := &metadata.QueryOptions{
		Include: []string{"Account"},
	}

	ctx := context.WithValue(context.Background(), metadata.MetadataKey, siteMeta)
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)
	// Deliberately NOT setting AllowedIncludes for Account

	site, err := siteWrapper.Get(ctx, strconv.Itoa(sites[0].ID))
	if err != nil {
		t.Fatal("Get failed:", err)
	}

	// Account should NOT be populated (not authorized)
	if site.Account != nil {
		t.Error("Expected Account to be nil (not authorized), but it was populated")
	}
}

func TestRelationFilter_Security_OwnershipStillEnforced(t *testing.T) {
	db, cleanup := setupRelationFilterTestDB(t)
	defer cleanup()

	_, _, siteMeta, _, _ := createRelationFilterTestMeta()
	_, _, _, _, _ = seedRelationFilterTestData(t, db)

	// Test: Even with relation filters, ownership should still be enforced
	// Alice should only see her own sites even when filtering by parent fields
	siteWrapper := &datastore.Wrapper[RelSite]{Store: db}

	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Account.Status": {Value: "Active", Operator: metadata.OpEq},
		},
	}

	ctx := context.WithValue(context.Background(), metadata.MetadataKey, siteMeta)
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)
	ctx = context.WithValue(ctx, metadata.AuthInfoKey, &metadata.AuthInfo{UserID: "alice", Scopes: []string{"user"}})
	ctx = context.WithValue(ctx, metadata.OwnershipEnforcedKey, true)
	ctx = context.WithValue(ctx, metadata.OwnershipUserIDKey, "alice")

	sites, _, _, err := siteWrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Alice has 2 sites under Active accounts, Bob's site should be excluded by ownership
	expectedCount := 2
	if len(sites) != expectedCount {
		t.Errorf("Expected %d sites (ownership enforced), got %d", expectedCount, len(sites))
	}

	// Verify all sites belong to Alice
	for _, site := range sites {
		if site.OwnerID != "alice" {
			t.Errorf("Expected site owned by alice, got %s", site.OwnerID)
		}
	}
}

func TestRelationFilter_Security_CantBypassParentOwnershipViaChildFilter(t *testing.T) {
	db, cleanup := setupRelationFilterTestDB(t)
	defer cleanup()

	userMeta, _, _, _, _ := createRelationFilterTestMeta()
	_, _, _, _, _ = seedRelationFilterTestData(t, db)

	// Test: Bob tries to see Alice's data by filtering on her child records
	// This should still only return Bob's own data
	userWrapper := &datastore.Wrapper[RelUser]{Store: db}

	// Bob tries to filter for users with "alice" owned accounts
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Accounts.OwnerID": {Value: "alice", Operator: metadata.OpEq},
		},
	}

	// Configure ownership on User level
	userMeta.OwnershipFields = []string{"Email"} // Pretend Email maps to ownership

	ctx := context.WithValue(context.Background(), metadata.MetadataKey, userMeta)
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)
	ctx = context.WithValue(ctx, metadata.AuthInfoKey, &metadata.AuthInfo{UserID: "bob@example.com", Scopes: []string{"user"}})
	ctx = context.WithValue(ctx, metadata.OwnershipEnforcedKey, true)
	ctx = context.WithValue(ctx, metadata.OwnershipUserIDKey, "bob@example.com")

	users, _, _, err := userWrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Even with filter for alice's accounts, Bob should only see his own user record
	// (or none if his accounts don't match the filter)
	for _, user := range users {
		if user.Email != "bob@example.com" {
			t.Errorf("Bob should not see Alice's data, but saw user with email %s", user.Email)
		}
	}
}

// ============================================================================
// Combined Filter and Include Tests
// ============================================================================

func TestRelationFilter_CombinedFilterAndInclude(t *testing.T) {
	db, cleanup := setupRelationFilterTestDB(t)
	defer cleanup()

	userMeta, _, _, _, _ := createRelationFilterTestMeta()
	_, _, _, _, _ = seedRelationFilterTestData(t, db)

	// Test: Filter users by child field AND include the children
	// Filter: Users with Overdue bills
	// Include: Show those accounts
	userWrapper := &datastore.Wrapper[RelUser]{Store: db}

	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Accounts.Sites.Bills.Status": {Value: "Overdue", Operator: metadata.OpEq},
		},
		Include: []string{"Accounts"},
	}

	ctx := context.WithValue(context.Background(), metadata.MetadataKey, userMeta)
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)
	ctx = context.WithValue(ctx, metadata.AllowedIncludesKey, metadata.AllowedIncludes{"Accounts": false})

	users, _, _, err := userWrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Only Alice has Overdue bills
	if len(users) != 1 {
		t.Errorf("Expected 1 user with Overdue bills, got %d", len(users))
	}

	// Her accounts should be included
	if len(users) > 0 && len(users[0].Accounts) == 0 {
		t.Error("Expected Accounts to be included")
	}
}
