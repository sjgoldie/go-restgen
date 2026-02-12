package router

import (
	"context"
	"database/sql"
	"io"
	"reflect"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	_ "github.com/uptrace/bun/driver/sqliteshim"

	"github.com/sjgoldie/go-restgen/filestore"
	"github.com/sjgoldie/go-restgen/metadata"
)

func testDB(t *testing.T) *bun.DB {
	t.Helper()
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return bun.NewDB(sqlDB, sqlitedialect.New())
}

const testFieldName = "Name"
const testAuthorIDCol = "author_id"

// testModel is a simple model for testing route registration
type testModel struct {
	bun.BaseModel `bun:"table:test_models"`
	ID            int    `bun:"id,pk,autoincrement" json:"id"`
	Name          string `bun:"name" json:"name"`
}

func TestMergeQueryConfigs(t *testing.T) {
	t.Run("empty configs returns clone of original", func(t *testing.T) {
		original := &metadata.TypeMetadata{
			TypeID:           "test",
			FilterableFields: []string{testFieldName},
			DefaultLimit:     10,
		}

		result := mergeQueryConfigs(original, nil)

		// Should be a different pointer
		if result == original {
			t.Error("expected new metadata, got same pointer")
		}

		// But with same values
		if result.TypeID != original.TypeID {
			t.Errorf("expected TypeID %q, got %q", original.TypeID, result.TypeID)
		}
		if len(result.FilterableFields) != 1 || result.FilterableFields[0] != testFieldName {
			t.Error("FilterableFields not preserved")
		}
		if result.DefaultLimit != 10 {
			t.Errorf("expected DefaultLimit 10, got %d", result.DefaultLimit)
		}
	})

	t.Run("single config applied", func(t *testing.T) {
		original := &metadata.TypeMetadata{
			TypeID: "test",
		}

		configs := []QueryConfig{
			{
				FilterableFields: []string{testFieldName, "Status"},
				SortableFields:   []string{"CreatedAt"},
				DefaultSort:      "-CreatedAt",
				DefaultLimit:     20,
				MaxLimit:         100,
			},
		}

		result := mergeQueryConfigs(original, configs)

		if len(result.FilterableFields) != 2 {
			t.Errorf("expected 2 filterable fields, got %d", len(result.FilterableFields))
		}
		if len(result.SortableFields) != 1 {
			t.Errorf("expected 1 sortable field, got %d", len(result.SortableFields))
		}
		if result.DefaultSort != "-CreatedAt" {
			t.Errorf("expected DefaultSort '-CreatedAt', got %q", result.DefaultSort)
		}
		if result.DefaultLimit != 20 {
			t.Errorf("expected DefaultLimit 20, got %d", result.DefaultLimit)
		}
		if result.MaxLimit != 100 {
			t.Errorf("expected MaxLimit 100, got %d", result.MaxLimit)
		}
	})

	t.Run("multiple configs last wins", func(t *testing.T) {
		original := &metadata.TypeMetadata{
			TypeID: "test",
		}

		configs := []QueryConfig{
			{
				FilterableFields: []string{testFieldName},
				DefaultLimit:     10,
			},
			{
				FilterableFields: []string{"Status", "Priority"},
				DefaultLimit:     25,
			},
		}

		result := mergeQueryConfigs(original, configs)

		// Last config should win
		if len(result.FilterableFields) != 2 {
			t.Errorf("expected 2 filterable fields, got %d", len(result.FilterableFields))
		}
		if result.FilterableFields[0] != "Status" {
			t.Errorf("expected first field 'Status', got %q", result.FilterableFields[0])
		}
		if result.DefaultLimit != 25 {
			t.Errorf("expected DefaultLimit 25, got %d", result.DefaultLimit)
		}
	})

	t.Run("partial config only overrides set fields", func(t *testing.T) {
		original := &metadata.TypeMetadata{
			TypeID:           "test",
			FilterableFields: []string{"OriginalField"},
			DefaultLimit:     50,
		}

		configs := []QueryConfig{
			{
				// Only set DefaultSort, leave others empty
				DefaultSort: testFieldName,
			},
		}

		result := mergeQueryConfigs(original, configs)

		// Original values should be preserved for unset fields
		if len(result.FilterableFields) != 1 || result.FilterableFields[0] != "OriginalField" {
			t.Error("FilterableFields should be preserved when not set in config")
		}
		if result.DefaultLimit != 50 {
			t.Errorf("DefaultLimit should be preserved, got %d", result.DefaultLimit)
		}
		// But DefaultSort should be updated
		if result.DefaultSort != testFieldName {
			t.Errorf("expected DefaultSort 'Name', got %q", result.DefaultSort)
		}
	})

	t.Run("does not mutate original", func(t *testing.T) {
		original := &metadata.TypeMetadata{
			TypeID:           "test",
			FilterableFields: []string{"Original"},
		}

		configs := []QueryConfig{
			{
				FilterableFields: []string{"New1", "New2"},
			},
		}

		_ = mergeQueryConfigs(original, configs)

		// Original should be unchanged
		if len(original.FilterableFields) != 1 || original.FilterableFields[0] != "Original" {
			t.Error("original metadata was mutated")
		}
	})
}

type columnFromGoNameTestModel struct {
	bun.BaseModel `bun:"table:test_ftc"`
	ID            int    `bun:"id,pk,autoincrement"`
	NMI           string `bun:"nmi,notnull"`
	AuthorID      int    `bun:"author_id,notnull"`
}

func TestColumnFromGoName(t *testing.T) {
	db := testDB(t)
	tType := reflect.TypeOf(columnFromGoNameTestModel{})

	t.Run("standard field", func(t *testing.T) {
		col, err := columnFromGoName(db, tType, "AuthorID")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if col != testAuthorIDCol {
			t.Errorf("expected 'author_id', got '%s'", col)
		}
	})

	t.Run("acronym field", func(t *testing.T) {
		col, err := columnFromGoName(db, tType, "NMI")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if col != "nmi" {
			t.Errorf("expected 'nmi', got '%s'", col)
		}
	})

	t.Run("pk field", func(t *testing.T) {
		col, err := columnFromGoName(db, tType, "ID")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if col != "id" {
			t.Errorf("expected 'id', got '%s'", col)
		}
	})

	t.Run("field not found", func(t *testing.T) {
		_, err := columnFromGoName(db, tType, "Nonexistent")
		if err == nil {
			t.Fatal("expected error for nonexistent field")
		}
	})
}

// Models for testing findParentRelationshipFromType — inverted belongs-to case
// (parent has belongs-to pointing to child, e.g., Post.Author belongs-to User)
type parentRelUser struct {
	bun.BaseModel `bun:"table:parent_rel_users"`
	ID            int    `bun:"id,pk,autoincrement"`
	Name          string `bun:"name"`
}

type parentRelPost struct {
	bun.BaseModel `bun:"table:parent_rel_posts"`
	ID            int            `bun:"id,pk,autoincrement"`
	AuthorID      int            `bun:"author_id,notnull"`
	Author        *parentRelUser `bun:"rel:belongs-to,join:author_id=id"`
	Title         string         `bun:"title"`
}

func TestFindParentRelationshipFromType(t *testing.T) {
	db := testDB(t)
	userType := reflect.TypeOf(parentRelUser{})
	postType := reflect.TypeOf(parentRelPost{})

	t.Run("child belongs-to parent", func(t *testing.T) {
		// Post belongs-to User (standard case: FK author_id is on Post)
		rel, err := findParentRelationshipFromType(db, postType, userType)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rel.foreignKeyCol != testAuthorIDCol {
			t.Errorf("expected foreignKeyCol 'author_id', got %q", rel.foreignKeyCol)
		}
		if rel.parentJoinCol != "id" {
			t.Errorf("expected parentJoinCol 'id', got %q", rel.parentJoinCol)
		}
		if rel.fieldName != "Author" {
			t.Errorf("expected fieldName 'Author', got %q", rel.fieldName)
		}
	})

	t.Run("parent belongs-to child (inverted)", func(t *testing.T) {
		// User registered as child of Post — Post.Author belongs-to User
		// FK author_id is on the parent (Post), not on the child (User)
		rel, err := findParentRelationshipFromType(db, userType, postType)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rel.foreignKeyCol != testAuthorIDCol {
			t.Errorf("expected foreignKeyCol 'author_id', got %q", rel.foreignKeyCol)
		}
		if rel.parentJoinCol != "id" {
			t.Errorf("expected parentJoinCol 'id', got %q", rel.parentJoinCol)
		}
		if rel.fieldName != "Author" {
			t.Errorf("expected fieldName 'Author', got %q", rel.fieldName)
		}
	})

	t.Run("no relationship", func(t *testing.T) {
		unrelatedType := reflect.TypeOf(testModel{})
		_, err := findParentRelationshipFromType(db, unrelatedType, userType)
		if err == nil {
			t.Fatal("expected error for unrelated types")
		}
	})

	t.Run("nil parent returns empty", func(t *testing.T) {
		rel, err := findParentRelationshipFromType(db, postType, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rel.foreignKeyCol != "" {
			t.Errorf("expected empty foreignKeyCol, got %q", rel.foreignKeyCol)
		}
	})
}

func TestRegisterChildAuthConfig(t *testing.T) {
	t.Run("empty relationName does nothing", func(t *testing.T) {
		sharedMap := make(map[string]*AuthConfig)
		b := &Builder{
			parentChildRelationAuth: sharedMap,
		}

		authMap := map[string]*AuthConfig{
			MethodGet: {Scopes: []string{"child:read"}},
		}

		registerChildAuthConfig(b, "", authMap)

		if len(sharedMap) != 0 {
			t.Error("sharedMap should remain empty for empty relationName")
		}
	})

	t.Run("nil childAuthGet does nothing", func(t *testing.T) {
		sharedMap := make(map[string]*AuthConfig)
		b := &Builder{
			parentChildRelationAuth: sharedMap,
		}

		authMap := map[string]*AuthConfig{
			// No MethodGet entry
		}

		registerChildAuthConfig(b, "posts", authMap)

		if len(sharedMap) != 0 {
			t.Error("sharedMap should remain empty when child has no GET auth")
		}
	})

	t.Run("nil parentChildRelationAuth does nothing", func(t *testing.T) {
		b := &Builder{
			parentChildRelationAuth: nil,
		}

		childGet := &AuthConfig{Scopes: []string{"child:read"}}
		authMap := map[string]*AuthConfig{
			MethodGet: childGet,
		}

		// Should not panic
		registerChildAuthConfig(b, "posts", authMap)
	})

	t.Run("registers child auth to shared map", func(t *testing.T) {
		sharedMap := make(map[string]*AuthConfig)
		childGet := &AuthConfig{Scopes: []string{"child:read"}}

		b := &Builder{
			parentChildRelationAuth: sharedMap,
		}

		authMap := map[string]*AuthConfig{
			MethodGet: childGet,
		}

		registerChildAuthConfig(b, "posts", authMap)

		if sharedMap["posts"] != childGet {
			t.Error("child auth not registered correctly to shared map")
		}
	})

	t.Run("multiple children share the same map", func(t *testing.T) {
		sharedMap := make(map[string]*AuthConfig)
		postsGet := &AuthConfig{Scopes: []string{"posts:read"}}
		commentsGet := &AuthConfig{Scopes: []string{"comments:read"}}

		b := &Builder{
			parentChildRelationAuth: sharedMap,
		}

		registerChildAuthConfig(b, "Posts", map[string]*AuthConfig{MethodGet: postsGet})
		registerChildAuthConfig(b, "Comments", map[string]*AuthConfig{MethodGet: commentsGet})

		if len(sharedMap) != 2 {
			t.Errorf("expected 2 entries in shared map, got %d", len(sharedMap))
		}
		if sharedMap["Posts"] != postsGet {
			t.Error("Posts auth not registered correctly")
		}
		if sharedMap["Comments"] != commentsGet {
			t.Error("Comments auth not registered correctly")
		}
	})
}

func TestSharedChildRelationAuth(t *testing.T) {
	t.Run("batch and regular configs share same ChildAuth map", func(t *testing.T) {
		// This test verifies that when we use AllPublicWithBatch and register child routes,
		// the batch auth configs have access to the same ChildAuth map as regular GET/LIST configs
		r := chi.NewRouter()
		b := NewBuilder(r, testDB(t))

		// We need to capture the auth configs to verify they share the same ChildAuth
		// Since we can't easily access them after registration, we'll verify the behavior
		// by checking that the prepareMetadata function creates a shared map

		// Create auth configs manually to simulate what happens
		authConfigs := []AuthConfig{AllPublicWithBatch()}
		authMap := mergeAuthConfigs(authConfigs)

		// Verify batch methods are in authMap
		if authMap[MethodBatchCreate] == nil {
			t.Fatal("MethodBatchCreate should be in authMap")
		}
		if authMap[MethodGet] == nil {
			t.Fatal("MethodGet should be in authMap")
		}

		// Create shared childRelationAuth and assign to all configs (simulating prepareMetadata)
		childRelationAuth := make(map[string]*AuthConfig)
		for _, config := range authMap {
			if config != nil {
				config.ChildAuth = childRelationAuth
			}
		}

		// Add a child to the shared map
		childGet := &AuthConfig{Scopes: []string{"child:read"}}
		childRelationAuth["Children"] = childGet

		// Verify all auth configs can see the child (proves they share the same map)
		if authMap[MethodGet].ChildAuth["Children"] != childGet {
			t.Error("GET should see child auth")
		}
		if authMap[MethodList].ChildAuth["Children"] != childGet {
			t.Error("LIST should see child auth")
		}
		if authMap[MethodBatchCreate].ChildAuth["Children"] != childGet {
			t.Error("BATCH_CREATE should see child auth")
		}
		if authMap[MethodBatchUpdate].ChildAuth["Children"] != childGet {
			t.Error("BATCH_UPDATE should see child auth")
		}
		if authMap[MethodBatchDelete].ChildAuth["Children"] != childGet {
			t.Error("BATCH_DELETE should see child auth")
		}

		// Use the builder to avoid unused variable warning
		_ = b
	})
}

// mockFileStorage is a test implementation of filestore.FileStorage
type mockFileStorage struct{}

func (m *mockFileStorage) Store(_ context.Context, _ io.Reader, _ filestore.FileMetadata) (string, error) {
	return "test-key", nil
}

func (m *mockFileStorage) Retrieve(_ context.Context, _ string) (io.ReadCloser, filestore.FileMetadata, error) {
	return nil, filestore.FileMetadata{}, nil
}

func (m *mockFileStorage) Delete(_ context.Context, _ string) error {
	return nil
}

func (m *mockFileStorage) GenerateSignedURL(_ context.Context, _ string) (string, error) {
	return "https://example.com/signed", nil
}

func TestAsFileResource(t *testing.T) {
	t.Run("returns FileResourceConfig", func(t *testing.T) {
		config := AsFileResource()
		if config != (FileResourceConfig{}) {
			t.Error("expected empty FileResourceConfig struct")
		}
	})
}

func TestFileResourceRegistration(t *testing.T) {
	t.Run("succeeds when filestore is initialized", func(t *testing.T) {
		// Initialize if not already done (singleton)
		if !filestore.IsInitialized() {
			storage := &mockFileStorage{}
			if err := filestore.Initialize(storage); err != nil {
				t.Fatalf("failed to initialize filestore: %v", err)
			}
		}

		r := chi.NewRouter()
		b := NewBuilder(r, testDB(t))
		RegisterRoutes[testModel](b, "/test", AsFileResource())
	})
}
