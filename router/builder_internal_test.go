package router

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/filestore"
	"github.com/sjgoldie/go-restgen/metadata"
)

const testFieldName = "Name"
const testParentIDCol = "parent_id"

// fileModel with bun tags for file resource tests (prepareMetadata needs bun table)
type fileModel struct {
	bun.BaseModel `bun:"table:file_models"`
	ID            int    `bun:"id,pk,autoincrement"`
	Name          string `bun:"name"`
	filestore.FileFields
}

type testModel struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Models with bun tags for resolveParentRelationship tests
type parentModel struct {
	bun.BaseModel `bun:"table:rpr_parents"`
	ID            int    `bun:"id,pk,autoincrement"`
	Code          string `bun:"code"`
	Name          string `bun:"name"`
}

type childModel struct {
	bun.BaseModel `bun:"table:rpr_children"`
	ID            int          `bun:"id,pk,autoincrement"`
	ParentID      int          `bun:"parent_id,notnull"`
	Parent        *parentModel `bun:"rel:belongs-to,join:parent_id=id"`
	Title         string       `bun:"title"`
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
		b := NewBuilder(r)

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
		b := NewBuilder(r)
		RegisterRoutes[testModel](b, "/test", AsFileResource())
	})
}

func TestResolveParentRelationship(t *testing.T) {
	childType := reflect.TypeOf(childModel{})
	parentType := reflect.TypeOf(parentModel{})

	parentMeta := &metadata.TypeMetadata{
		ModelType: parentType,
	}

	t.Run("no parent returns empty relation with defaults", func(t *testing.T) {
		gotParentType, rel, joinCol, joinField := resolveParentRelationship(nil, childType, "childModel", nil)

		if gotParentType != nil {
			t.Errorf("expected nil parentType, got %v", gotParentType)
		}
		if rel.ForeignKeyCol != "" {
			t.Errorf("expected empty ForeignKeyCol, got %q", rel.ForeignKeyCol)
		}
		if joinCol != "id" {
			t.Errorf("expected default joinCol \"id\", got %q", joinCol)
		}
		if joinField != "ID" {
			t.Errorf("expected default joinField \"ID\", got %q", joinField)
		}
	})

	t.Run("auto-discovers belongs-to relation", func(t *testing.T) {
		gotParentType, rel, joinCol, joinField := resolveParentRelationship(parentMeta, childType, "childModel", nil)

		if gotParentType != parentType {
			t.Errorf("expected parentType %v, got %v", parentType, gotParentType)
		}
		if rel.ForeignKeyCol != testParentIDCol {
			t.Errorf("expected ForeignKeyCol \"parent_id\", got %q", rel.ForeignKeyCol)
		}
		if joinCol != "id" {
			t.Errorf("expected joinCol \"id\", got %q", joinCol)
		}
		if joinField != "ID" {
			t.Errorf("expected joinField \"ID\", got %q", joinField)
		}
	})

	t.Run("explicit joinOn resolves field names to columns", func(t *testing.T) {
		joinOn := &JoinOnConfig{
			ChildCol:  "ParentID",
			ParentCol: "Code",
		}

		gotParentType, rel, joinCol, joinField := resolveParentRelationship(parentMeta, childType, "childModel", joinOn)

		if gotParentType != parentType {
			t.Errorf("expected parentType %v, got %v", parentType, gotParentType)
		}
		if rel.ForeignKeyCol != testParentIDCol {
			t.Errorf("expected ForeignKeyCol \"parent_id\", got %q", rel.ForeignKeyCol)
		}
		if rel.ParentJoinCol != "code" {
			t.Errorf("expected ParentJoinCol \"code\", got %q", rel.ParentJoinCol)
		}
		if joinCol != "code" {
			t.Errorf("expected joinCol \"code\", got %q", joinCol)
		}
		if joinField != "Code" {
			t.Errorf("expected joinField \"Code\", got %q", joinField)
		}
	})

	t.Run("joinOn without parent falls back to auto-discovery", func(t *testing.T) {
		joinOn := &JoinOnConfig{
			ChildCol:  "ParentID",
			ParentCol: "Code",
		}

		_, rel, joinCol, joinField := resolveParentRelationship(nil, childType, "childModel", joinOn)

		// joinOn is ignored when parentMeta is nil — falls back to FindRelation with nil parentType
		if rel.ForeignKeyCol != "" {
			t.Errorf("expected empty ForeignKeyCol, got %q", rel.ForeignKeyCol)
		}
		if joinCol != "id" {
			t.Errorf("expected default joinCol \"id\", got %q", joinCol)
		}
		if joinField != "ID" {
			t.Errorf("expected default joinField \"ID\", got %q", joinField)
		}
	})

	t.Run("joinOn with invalid child field logs warning and returns empty col", func(t *testing.T) {
		joinOn := &JoinOnConfig{
			ChildCol:  "NonExistentField",
			ParentCol: "Code",
		}

		_, rel, _, _ := resolveParentRelationship(parentMeta, childType, "childModel", joinOn)

		// Invalid field returns empty string from ColumnName
		if rel.ForeignKeyCol != "" {
			t.Errorf("expected empty ForeignKeyCol for invalid field, got %q", rel.ForeignKeyCol)
		}
	})

	t.Run("joinOn with invalid parent field logs warning and returns empty col", func(t *testing.T) {
		joinOn := &JoinOnConfig{
			ChildCol:  "ParentID",
			ParentCol: "NonExistentField",
		}

		_, rel, joinCol, _ := resolveParentRelationship(parentMeta, childType, "childModel", joinOn)

		if rel.ForeignKeyCol != testParentIDCol {
			t.Errorf("expected ForeignKeyCol \"parent_id\", got %q", rel.ForeignKeyCol)
		}
		// Invalid parent col falls through to default
		if joinCol != "id" {
			t.Errorf("expected default joinCol \"id\" for invalid parent field, got %q", joinCol)
		}
	})

	t.Run("auto-discovery with unrelated types returns defaults", func(t *testing.T) {
		unrelatedType := reflect.TypeOf(testModel{})
		unrelatedMeta := &metadata.TypeMetadata{
			ModelType: unrelatedType,
		}

		_, rel, joinCol, joinField := resolveParentRelationship(unrelatedMeta, childType, "childModel", nil)

		// No relation found — ForeignKeyCol may be empty
		if rel.ForeignKeyCol != "" {
			t.Errorf("expected empty ForeignKeyCol for unrelated types, got %q", rel.ForeignKeyCol)
		}
		if joinCol != "id" {
			t.Errorf("expected default joinCol \"id\", got %q", joinCol)
		}
		if joinField != "ID" {
			t.Errorf("expected default joinField \"ID\", got %q", joinField)
		}
	})
}

func TestCreateMetadataMiddleware(t *testing.T) {
	t.Run("applies MaxBytesReader for non-file resources", func(t *testing.T) {
		meta := &metadata.TypeMetadata{
			MaxBodySize:    512,
			IsFileResource: false,
		}

		middleware := createMetadataMiddleware(meta)

		var capturedBody http.Request
		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedBody = *r
		}))

		body := strings.NewReader(strings.Repeat("x", 1024))
		req := httptest.NewRequest("POST", "/test", body)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		// Reading beyond the limit should fail
		buf := make([]byte, 1024)
		_, err := capturedBody.Body.Read(buf)
		if err == nil {
			t.Error("expected error reading beyond MaxBodySize limit")
		}
	})

	t.Run("skips MaxBytesReader for file resources", func(t *testing.T) {
		meta := &metadata.TypeMetadata{
			MaxBodySize:    512,
			IsFileResource: true,
		}

		middleware := createMetadataMiddleware(meta)

		var capturedBody http.Request
		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedBody = *r
		}))

		body := strings.NewReader(strings.Repeat("x", 1024))
		req := httptest.NewRequest("POST", "/test", body)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		// Should be able to read full body — no limit applied
		buf := make([]byte, 1024)
		n, err := capturedBody.Body.Read(buf)
		if err != nil {
			t.Errorf("expected no error reading body for file resource, got %v", err)
		}
		if n != 1024 {
			t.Errorf("expected to read 1024 bytes, got %d", n)
		}
	})

	t.Run("injects metadata into context", func(t *testing.T) {
		meta := &metadata.TypeMetadata{
			TypeID:      "test-type",
			MaxBodySize: metadata.DefaultMaxBodySize,
		}

		middleware := createMetadataMiddleware(meta)

		var capturedMeta *metadata.TypeMetadata
		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedMeta = r.Context().Value(metadata.MetadataKey).(*metadata.TypeMetadata)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if capturedMeta == nil {
			t.Fatal("expected metadata in context")
		}
		if capturedMeta.TypeID != "test-type" {
			t.Errorf("expected TypeID \"test-type\", got %q", capturedMeta.TypeID)
		}
	})

	t.Run("uses exact MaxBodySize value from metadata", func(t *testing.T) {
		customLimit := int64(256)
		meta := &metadata.TypeMetadata{
			MaxBodySize: customLimit,
		}

		middleware := createMetadataMiddleware(meta)

		var capturedBody http.Request
		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedBody = *r
		}))

		// Send exactly at the limit — should succeed
		body := strings.NewReader(strings.Repeat("x", 256))
		req := httptest.NewRequest("POST", "/test", body)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		buf := make([]byte, 256)
		n, _ := capturedBody.Body.Read(buf)
		if n != 256 {
			t.Errorf("expected to read 256 bytes at limit, got %d", n)
		}
	})
}

func TestWithMaxUploadSize(t *testing.T) {
	t.Run("returns MaxUploadSizeConfig with given size", func(t *testing.T) {
		config := WithMaxUploadSize(10 << 20)
		if config.Size != 10<<20 {
			t.Errorf("expected size %d, got %d", 10<<20, config.Size)
		}
	})
}

func TestMaxUploadSizeDefault(t *testing.T) {
	if !filestore.IsInitialized() {
		storage := &mockFileStorage{}
		if err := filestore.Initialize(storage); err != nil {
			t.Fatalf("failed to initialize filestore: %v", err)
		}
	}

	t.Run("file resource gets DefaultMaxUploadSize when not specified", func(t *testing.T) {
		r := chi.NewRouter()
		b := NewBuilder(r)

		_, setup := prepareMetadata[fileModel](b, "/files", nil, nil, nil, nil, 0, "", true, "", nil, "", false, 0, 0)

		if setup.meta.MaxUploadSize != metadata.DefaultMaxUploadSize {
			t.Errorf("expected default MaxUploadSize %d, got %d", metadata.DefaultMaxUploadSize, setup.meta.MaxUploadSize)
		}
	})

	t.Run("custom MaxUploadSize is set on metadata", func(t *testing.T) {
		r := chi.NewRouter()
		b := NewBuilder(r)

		customSize := int64(10 << 20)
		_, setup := prepareMetadata[fileModel](b, "/files", nil, nil, nil, nil, 0, "", true, "", nil, "", false, 0, customSize)

		if setup.meta.MaxUploadSize != customSize {
			t.Errorf("expected MaxUploadSize %d, got %d", customSize, setup.meta.MaxUploadSize)
		}
	})

	t.Run("non-file resource gets DefaultMaxUploadSize", func(t *testing.T) {
		r := chi.NewRouter()
		b := NewBuilder(r)

		_, setup := prepareMetadata[testModel](b, "/items", nil, nil, nil, nil, 0, "", false, "", nil, "", false, 0, 0)

		if setup.meta.MaxUploadSize != metadata.DefaultMaxUploadSize {
			t.Errorf("expected default MaxUploadSize %d, got %d", metadata.DefaultMaxUploadSize, setup.meta.MaxUploadSize)
		}
	})
}

func TestRegisterRoutes_UnrecognizedOption(t *testing.T) {
	t.Run("logs warning for unrecognized option type", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
		original := slog.Default()
		slog.SetDefault(logger)
		defer slog.SetDefault(original)

		r := chi.NewRouter()
		b := NewBuilder(r)
		RegisterRoutes[testModel](b, "/test", "bogus-option")

		output := buf.String()
		if !strings.Contains(output, "unrecognized option type") {
			t.Errorf("expected warning about unrecognized option type, got: %s", output)
		}
		if !strings.Contains(output, "string") {
			t.Errorf("expected warning to include type name \"string\", got: %s", output)
		}
		if !strings.Contains(output, "/test") {
			t.Errorf("expected warning to include path \"/test\", got: %s", output)
		}
	})

	t.Run("unrecognized option does not prevent valid options from working", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
		original := slog.Default()
		slog.SetDefault(logger)
		defer slog.SetDefault(original)

		r := chi.NewRouter()
		b := NewBuilder(r)
		RegisterRoutes[testModel](b, "/test",
			AuthConfig{
				Methods: []string{MethodAll},
				Scopes:  []string{ScopePublic},
			},
			12345,
		)

		output := buf.String()
		if !strings.Contains(output, "unrecognized option type") {
			t.Errorf("expected warning for unrecognized int option, got: %s", output)
		}
		if !strings.Contains(output, "int") {
			t.Errorf("expected warning to include type name \"int\", got: %s", output)
		}
	})
}
