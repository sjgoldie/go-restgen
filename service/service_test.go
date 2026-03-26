//nolint:errcheck,gosec,staticcheck // Test code - unchecked test cleanup and string context keys are acceptable
package service_test

import (
	"context"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/filestore"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/service"

	"github.com/uptrace/bun"
)

// TestModel is a simple test model
type TestModel struct {
	bun.BaseModel `bun:"table:test_models"`
	ID            int    `bun:"id,pk,autoincrement"`
	Name          string `bun:"name"`
	Email         string `bun:"email,unique"`
}

const testFileContent = "file content here"

// testModelMeta is the metadata for TestModel
var testModelMeta = &metadata.TypeMetadata{
	TypeID:        "test_model_id",
	TypeName:      "TestModel",
	TableName:     "test_models",
	URLParamUUID:  "id",
	PKField:       "ID",
	ModelType:     reflect.TypeOf(TestModel{}),
	ParentType:    nil,
	ParentMeta:    nil,
	ForeignKeyCol: "",
}

// ctxWithMeta creates a context with the given metadata
func ctxWithMeta(meta *metadata.TypeMetadata) context.Context {
	return context.WithValue(context.Background(), metadata.MetadataKey, meta)
}

var testDB *datastore.SQLite

// Global mock file storage for all service tests
var testFileStorage *mockFileStorage

func TestMain(m *testing.M) {
	// Setup
	var err error
	testDB, err = datastore.NewSQLite(":memory:")
	if err != nil {
		panic("Failed to create test database: " + err.Error())
	}

	if err := datastore.Initialize(testDB); err != nil {
		testDB.Cleanup()
		panic("Failed to initialize datastore: " + err.Error())
	}

	_, err = testDB.GetDB().NewCreateTable().Model((*TestModel)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		testDB.Cleanup()
		panic("Failed to create table: " + err.Error())
	}

	// Initialize file storage once for all tests (uses sync.Once internally)
	testFileStorage = newMockFileStorage()
	if err := filestore.Initialize(testFileStorage); err != nil {
		testDB.Cleanup()
		panic("Failed to initialize file storage: " + err.Error())
	}

	// Run tests
	code := m.Run()

	// Teardown
	testDB.GetDB().NewDropTable().Model((*TestModel)(nil)).IfExists().Exec(context.Background())
	datastore.Cleanup()
	testDB.Cleanup()

	os.Exit(code)
}

func cleanTable(t *testing.T) {
	t.Helper()
	db, _ := datastore.Get()
	_, err := db.GetDB().NewDelete().Model((*TestModel)(nil)).Where("1=1").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to clean table:", err)
	}
	_, _ = db.GetDB().Exec("DELETE FROM sqlite_sequence WHERE name = 'test_models'")
}

func TestService_New(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{
			name:    "successful service creation",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := service.New[TestModel]()
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && svc == nil {
				t.Error("Expected service to be created, got nil")
			}
		})
	}
}

func TestService_GetAll(t *testing.T) {
	tests := []struct {
		name          string
		setupData     []TestModel
		expectedCount int
	}{
		{
			name:          "empty table",
			setupData:     []TestModel{},
			expectedCount: 0,
		},
		{
			name: "multiple items",
			setupData: []TestModel{
				{Name: "Item 1", Email: "item1@example.com"},
				{Name: "Item 2", Email: "item2@example.com"},
				{Name: "Item 3", Email: "item3@example.com"},
			},
			expectedCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanTable(t)

			// Setup data
			db, _ := datastore.Get()
			for _, item := range tt.setupData {
				_, err := db.GetDB().NewInsert().Model(&item).Exec(context.Background())
				if err != nil {
					t.Fatal("Failed to insert test item:", err)
				}
			}

			// Test GetAll
			svc, err := service.New[TestModel]()
			if err != nil {
				t.Fatal("Failed to create service:", err)
			}

			items, _, _, err := svc.GetAll(ctxWithMeta(testModelMeta))
			if err != nil {
				t.Fatal("GetAll failed:", err)
			}

			if len(items) != tt.expectedCount {
				t.Errorf("Expected %d items, got %d", tt.expectedCount, len(items))
			}
		})
	}
}

func TestService_Get(t *testing.T) {
	tests := []struct {
		name         string
		setupItem    *TestModel
		getID        string
		wantErr      bool
		expectedName string
	}{
		{
			name:         "existing item",
			setupItem:    &TestModel{Name: "Test Item", Email: "test@example.com"},
			getID:        "1",
			wantErr:      false,
			expectedName: "Test Item",
		},
		{
			name:      "not found",
			setupItem: nil,
			getID:     "999",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanTable(t)

			// Setup data
			if tt.setupItem != nil {
				db, _ := datastore.Get()
				_, err := db.GetDB().NewInsert().Model(tt.setupItem).Returning("*").Exec(context.Background())
				if err != nil {
					t.Fatal("Failed to insert test item:", err)
				}
			}

			// Test Get
			svc, err := service.New[TestModel]()
			if err != nil {
				t.Fatal("Failed to create service:", err)
			}

			item, err := svc.Get(ctxWithMeta(testModelMeta), tt.getID)
			if (err != nil) != tt.wantErr {
				t.Errorf("Get() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && item.Name != tt.expectedName {
				t.Errorf("Expected name '%s', got '%s'", tt.expectedName, item.Name)
			}
		})
	}
}

func TestService_Create(t *testing.T) {
	tests := []struct {
		name      string
		item      TestModel
		wantErr   bool
		checkName string
	}{
		{
			name:      "valid item",
			item:      TestModel{Name: "New Item", Email: "new@example.com"},
			wantErr:   false,
			checkName: "New Item",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanTable(t)

			svc, err := service.New[TestModel]()
			if err != nil {
				t.Fatal("Failed to create service:", err)
			}

			created, err := svc.Create(ctxWithMeta(testModelMeta), tt.item)
			if (err != nil) != tt.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if created.ID == 0 {
					t.Error("Expected ID to be set")
				}
				if created.Name != tt.checkName {
					t.Errorf("Expected name '%s', got '%s'", tt.checkName, created.Name)
				}
			}
		})
	}
}

func TestService_Update(t *testing.T) {
	tests := []struct {
		name         string
		setupItem    *TestModel
		updateItem   TestModel
		wantErr      bool
		expectedName string
	}{
		{
			name:         "valid update",
			setupItem:    &TestModel{Name: "Original", Email: "original@example.com"},
			updateItem:   TestModel{ID: 1, Name: "Updated", Email: "updated@example.com"},
			wantErr:      false,
			expectedName: "Updated",
		},
		{
			name:       "not found",
			setupItem:  nil,
			updateItem: TestModel{ID: 999, Name: "Name", Email: "email@example.com"},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanTable(t)

			// Setup data
			if tt.setupItem != nil {
				db, _ := datastore.Get()
				_, err := db.GetDB().NewInsert().Model(tt.setupItem).Returning("*").Exec(context.Background())
				if err != nil {
					t.Fatal("Failed to insert test item:", err)
				}
			}

			// Test Update
			svc, err := service.New[TestModel]()
			if err != nil {
				t.Fatal("Failed to create service:", err)
			}

			updated, err := svc.Update(ctxWithMeta(testModelMeta), strconv.Itoa(tt.updateItem.ID), tt.updateItem)
			if (err != nil) != tt.wantErr {
				t.Errorf("Update() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && updated.Name != tt.expectedName {
				t.Errorf("Expected name '%s', got '%s'", tt.expectedName, updated.Name)
			}
		})
	}
}

func TestService_Delete(t *testing.T) {
	tests := []struct {
		name      string
		setupItem *TestModel
		deleteID  string
		wantErr   bool
	}{
		{
			name:      "valid delete",
			setupItem: &TestModel{Name: "To Delete", Email: "delete@example.com"},
			deleteID:  "1",
			wantErr:   false,
		},
		{
			name:      "not found",
			setupItem: nil,
			deleteID:  "999",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanTable(t)

			// Setup data
			if tt.setupItem != nil {
				db, _ := datastore.Get()
				_, err := db.GetDB().NewInsert().Model(tt.setupItem).Returning("*").Exec(context.Background())
				if err != nil {
					t.Fatal("Failed to insert test item:", err)
				}
			}

			// Test Delete
			svc, err := service.New[TestModel]()
			if err != nil {
				t.Fatal("Failed to create service:", err)
			}

			err = svc.Delete(ctxWithMeta(testModelMeta), tt.deleteID)
			if (err != nil) != tt.wantErr {
				t.Errorf("Delete() error = %v, wantErr %v", err, tt.wantErr)
			}

			// Verify deletion if successful
			if !tt.wantErr {
				db, _ := datastore.Get()
				var check TestModel
				err := db.GetDB().NewSelect().Model(&check).Where("id = ?", tt.deleteID).Scan(context.Background())
				if err == nil {
					t.Error("Expected item to be deleted")
				}
			}
		})
	}
}

func TestService_TypeSafety(t *testing.T) {
	type User struct {
		bun.BaseModel `bun:"table:users"`
		ID            int    `bun:"id,pk,autoincrement"`
		Name          string `bun:"name"`
	}

	type Product struct {
		bun.BaseModel `bun:"table:products"`
		ID            int     `bun:"id,pk,autoincrement"`
		Price         float64 `bun:"price"`
	}

	userSvc, err := service.New[User]()
	if err != nil {
		t.Fatal("Failed to get user service:", err)
	}

	productSvc, err := service.New[Product]()
	if err != nil {
		t.Fatal("Failed to get product service:", err)
	}

	if userSvc == nil || productSvc == nil {
		t.Fatal("Failed to create type-safe services")
	}
}

// ParentModel and ChildModel for parent relation tests
type ParentModel struct {
	bun.BaseModel `bun:"table:parent_models"`
	ID            int    `bun:"id,pk,autoincrement"`
	Name          string `bun:"name"`
	ChildID       int    `bun:"child_id"`
}

type ChildModel struct {
	bun.BaseModel `bun:"table:child_models"`
	ID            int    `bun:"id,pk,autoincrement"`
	Name          string `bun:"name"`
}

func TestService_GetByParentRelation(t *testing.T) {
	// Create tables
	db, _ := datastore.Get()
	_, err := db.GetDB().NewCreateTable().Model((*ParentModel)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create parent table:", err)
	}
	_, err = db.GetDB().NewCreateTable().Model((*ChildModel)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create child table:", err)
	}
	defer func() {
		db.GetDB().NewDropTable().Model((*ParentModel)(nil)).IfExists().Exec(context.Background())
		db.GetDB().NewDropTable().Model((*ChildModel)(nil)).IfExists().Exec(context.Background())
	}()

	// Clean tables
	db.GetDB().NewDelete().Model((*ParentModel)(nil)).Where("1=1").Exec(context.Background())
	db.GetDB().NewDelete().Model((*ChildModel)(nil)).Where("1=1").Exec(context.Background())

	// Create child
	child := &ChildModel{Name: "Test Child"}
	_, err = db.GetDB().NewInsert().Model(child).Returning("*").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create child:", err)
	}

	// Create parent with reference to child
	parent := &ParentModel{Name: "Test Parent", ChildID: child.ID}
	_, err = db.GetDB().NewInsert().Model(parent).Returning("*").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create parent:", err)
	}

	// Create service
	svc, err := service.New[ChildModel]()
	if err != nil {
		t.Fatal("Failed to create service:", err)
	}

	// Create parent metadata
	parentMeta := &metadata.TypeMetadata{
		TypeID:       "parent_model_id",
		TypeName:     "ParentModel",
		TableName:    "parent_models",
		URLParamUUID: "parent_id",
		ModelType:    reflect.TypeOf(ParentModel{}),
	}

	// Create metadata for child accessed via parent
	childMeta := &metadata.TypeMetadata{
		TypeID:        "child_model_id",
		TypeName:      "ChildModel",
		TableName:     "child_models",
		URLParamUUID:  parentMeta.URLParamUUID,
		ModelType:     reflect.TypeOf(ChildModel{}),
		ParentType:    reflect.TypeOf(ParentModel{}),
		ParentMeta:    parentMeta,
		ParentFKField: "ChildID",
	}

	ctx := context.WithValue(context.Background(), metadata.MetadataKey, childMeta)

	// Get child by parent relation
	retrieved, err := svc.GetByParentRelation(ctx, strconv.Itoa(parent.ID))
	if err != nil {
		t.Fatal("GetByParentRelation failed:", err)
	}

	if retrieved.ID != child.ID {
		t.Errorf("Expected child ID %d, got %d", child.ID, retrieved.ID)
	}
	if retrieved.Name != child.Name {
		t.Errorf("Expected child name '%s', got '%s'", child.Name, retrieved.Name)
	}
}

func TestService_UpdateByParentRelation(t *testing.T) {
	// Create tables
	db, _ := datastore.Get()
	_, err := db.GetDB().NewCreateTable().Model((*ParentModel)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create parent table:", err)
	}
	_, err = db.GetDB().NewCreateTable().Model((*ChildModel)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create child table:", err)
	}
	defer func() {
		db.GetDB().NewDropTable().Model((*ParentModel)(nil)).IfExists().Exec(context.Background())
		db.GetDB().NewDropTable().Model((*ChildModel)(nil)).IfExists().Exec(context.Background())
	}()

	// Clean tables
	db.GetDB().NewDelete().Model((*ParentModel)(nil)).Where("1=1").Exec(context.Background())
	db.GetDB().NewDelete().Model((*ChildModel)(nil)).Where("1=1").Exec(context.Background())

	// Create child
	child := &ChildModel{Name: "Original Name"}
	_, err = db.GetDB().NewInsert().Model(child).Returning("*").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create child:", err)
	}

	// Create parent with reference to child
	parent := &ParentModel{Name: "Test Parent", ChildID: child.ID}
	_, err = db.GetDB().NewInsert().Model(parent).Returning("*").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create parent:", err)
	}

	// Create service
	svc, err := service.New[ChildModel]()
	if err != nil {
		t.Fatal("Failed to create service:", err)
	}

	// Create parent metadata
	parentMeta := &metadata.TypeMetadata{
		TypeID:       "parent_model_id",
		TypeName:     "ParentModel",
		TableName:    "parent_models",
		URLParamUUID: "parent_id",
		ModelType:    reflect.TypeOf(ParentModel{}),
	}

	// Create metadata for child accessed via parent
	childMeta := &metadata.TypeMetadata{
		TypeID:        "child_model_id",
		TypeName:      "ChildModel",
		TableName:     "child_models",
		URLParamUUID:  parentMeta.URLParamUUID,
		PKField:       "ID",
		ModelType:     reflect.TypeOf(ChildModel{}),
		ParentType:    reflect.TypeOf(ParentModel{}),
		ParentMeta:    parentMeta,
		ParentFKField: "ChildID",
	}

	ctx := context.WithValue(context.Background(), metadata.MetadataKey, childMeta)

	// Update child by parent relation
	updatedChild := ChildModel{ID: child.ID, Name: "Updated Name"}
	updated, err := svc.UpdateByParentRelation(ctx, strconv.Itoa(parent.ID), updatedChild)
	if err != nil {
		t.Fatal("UpdateByParentRelation failed:", err)
	}

	if updated.Name != "Updated Name" {
		t.Errorf("Expected name 'Updated Name', got '%s'", updated.Name)
	}

	// Verify the update persisted
	var check ChildModel
	err = db.GetDB().NewSelect().Model(&check).Where("id = ?", child.ID).Scan(context.Background())
	if err != nil {
		t.Fatal("Failed to verify update:", err)
	}
	if check.Name != "Updated Name" {
		t.Errorf("Expected persisted name 'Updated Name', got '%s'", check.Name)
	}
}

func TestService_PatchByParentRelation(t *testing.T) {
	db, _ := datastore.Get()
	_, err := db.GetDB().NewCreateTable().Model((*ParentModel)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create parent table:", err)
	}
	_, err = db.GetDB().NewCreateTable().Model((*ChildModel)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create child table:", err)
	}
	defer func() {
		db.GetDB().NewDropTable().Model((*ParentModel)(nil)).IfExists().Exec(context.Background())
		db.GetDB().NewDropTable().Model((*ChildModel)(nil)).IfExists().Exec(context.Background())
	}()

	db.GetDB().NewDelete().Model((*ParentModel)(nil)).Where("1=1").Exec(context.Background())
	db.GetDB().NewDelete().Model((*ChildModel)(nil)).Where("1=1").Exec(context.Background())

	child := &ChildModel{Name: "Original Name"}
	_, err = db.GetDB().NewInsert().Model(child).Returning("*").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create child:", err)
	}

	parent := &ParentModel{Name: "Test Parent", ChildID: child.ID}
	_, err = db.GetDB().NewInsert().Model(parent).Returning("*").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create parent:", err)
	}

	svc, err := service.New[ChildModel]()
	if err != nil {
		t.Fatal("Failed to create service:", err)
	}

	parentMeta := &metadata.TypeMetadata{
		TypeID:       "parent_model_id",
		TypeName:     "ParentModel",
		TableName:    "parent_models",
		URLParamUUID: "parent_id",
		ModelType:    reflect.TypeOf(ParentModel{}),
	}

	childMeta := &metadata.TypeMetadata{
		TypeID:        "child_model_id",
		TypeName:      "ChildModel",
		TableName:     "child_models",
		URLParamUUID:  parentMeta.URLParamUUID,
		PKField:       "ID",
		ModelType:     reflect.TypeOf(ChildModel{}),
		ParentType:    reflect.TypeOf(ParentModel{}),
		ParentMeta:    parentMeta,
		ParentFKField: "ChildID",
	}

	ctx := context.WithValue(context.Background(), metadata.MetadataKey, childMeta)

	patchedChild := ChildModel{Name: "Patched Name"}
	patched, err := svc.PatchByParentRelation(ctx, strconv.Itoa(parent.ID), patchedChild)
	if err != nil {
		t.Fatal("PatchByParentRelation failed:", err)
	}

	if patched.Name != "Patched Name" {
		t.Errorf("Expected name 'Patched Name', got '%s'", patched.Name)
	}

	var check ChildModel
	err = db.GetDB().NewSelect().Model(&check).Where("id = ?", child.ID).Scan(context.Background())
	if err != nil {
		t.Fatal("Failed to verify patch:", err)
	}
	if check.Name != "Patched Name" {
		t.Errorf("Expected persisted name 'Patched Name', got '%s'", check.Name)
	}
}

// FileModel is a test model that implements FileResource
type FileModel struct {
	bun.BaseModel `bun:"table:file_models"`
	ID            int    `bun:"id,pk,autoincrement"`
	Name          string `bun:"name"`
	filestore.FileFields
}

// mockFileStorage is a mock implementation of FileStorage for testing
type mockFileStorage struct {
	files     map[string]string
	signedURL string // empty string means proxy mode, non-empty means signed URL mode
}

func newMockFileStorage() *mockFileStorage {
	return &mockFileStorage{
		files:     make(map[string]string),
		signedURL: "", // Default to proxy mode
	}
}

func (m *mockFileStorage) Store(ctx context.Context, r io.Reader, meta filestore.FileMetadata) (string, error) {
	content, _ := io.ReadAll(r)
	key := "test-key-123"
	m.files[key] = string(content)
	return key, nil
}

func (m *mockFileStorage) Retrieve(ctx context.Context, key string) (io.ReadCloser, filestore.FileMetadata, error) {
	content, ok := m.files[key]
	if !ok {
		return nil, filestore.FileMetadata{}, filestore.ErrStorageKeyNotFound
	}
	return io.NopCloser(strings.NewReader(content)), filestore.FileMetadata{
		ContentType: "text/plain",
		Size:        int64(len(content)),
		Filename:    "test.txt",
	}, nil
}

func (m *mockFileStorage) Delete(ctx context.Context, key string) error {
	delete(m.files, key)
	return nil
}

func (m *mockFileStorage) GenerateSignedURL(ctx context.Context, key string) (string, error) {
	return m.signedURL, nil
}

func TestService_Download_ProxyMode(t *testing.T) {
	// Create table
	db, _ := datastore.Get()
	_, err := db.GetDB().NewCreateTable().Model((*FileModel)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create file_models table:", err)
	}
	defer db.GetDB().NewDropTable().Model((*FileModel)(nil)).IfExists().Exec(context.Background())

	// Clean table
	db.GetDB().NewDelete().Model((*FileModel)(nil)).Where("1=1").Exec(context.Background())

	// Add file to shared mock storage
	testFileStorage.files["test-storage-key"] = testFileContent
	defer delete(testFileStorage.files, "test-storage-key")

	// Create file record
	file := &FileModel{
		Name: "Test File",
		FileFields: filestore.FileFields{
			StorageKey:  "test-storage-key",
			Filename:    "test.txt",
			ContentType: "text/plain",
			Size:        17,
		},
	}
	_, err = db.GetDB().NewInsert().Model(file).Returning("*").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create file:", err)
	}

	// Create service
	svc, err := service.New[FileModel]()
	if err != nil {
		t.Fatal("Failed to create service:", err)
	}

	// Create metadata
	fileMeta := &metadata.TypeMetadata{
		TypeID:       "file_model_id",
		TypeName:     "FileModel",
		TableName:    "file_models",
		URLParamUUID: "file_id",
		ModelType:    reflect.TypeOf(FileModel{}),
	}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, fileMeta)

	// Test Download
	result, err := svc.Download(ctx, strconv.Itoa(file.ID))
	if err != nil {
		t.Fatal("Download failed:", err)
	}

	// In proxy mode, should have reader
	if result.Reader == nil {
		t.Error("Expected reader in proxy mode")
	}
	if result.SignedURL != "" {
		t.Error("Expected empty SignedURL in proxy mode")
	}
	if result.ContentType != "text/plain" {
		t.Errorf("Expected content type 'text/plain', got '%s'", result.ContentType)
	}
	if result.Filename != "test.txt" {
		t.Errorf("Expected filename 'test.txt', got '%s'", result.Filename)
	}

	// Read content
	content, _ := io.ReadAll(result.Reader)
	result.Reader.Close()
	if string(content) != testFileContent {
		t.Errorf("Expected content '%s', got '%s'", testFileContent, string(content))
	}
}

func TestService_Download_SignedURLMode(t *testing.T) {
	// Create table
	db, _ := datastore.Get()
	_, err := db.GetDB().NewCreateTable().Model((*FileModel)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create file_models table:", err)
	}
	defer db.GetDB().NewDropTable().Model((*FileModel)(nil)).IfExists().Exec(context.Background())

	// Clean table
	db.GetDB().NewDelete().Model((*FileModel)(nil)).Where("1=1").Exec(context.Background())

	// Configure mock storage to return signed URLs
	testFileStorage.files["test-storage-key"] = testFileContent
	testFileStorage.signedURL = "https://signed.example.com/file"
	defer func() {
		delete(testFileStorage.files, "test-storage-key")
		testFileStorage.signedURL = "" // Reset to proxy mode
	}()

	// Create file record
	file := &FileModel{
		Name: "Test File",
		FileFields: filestore.FileFields{
			StorageKey:  "test-storage-key",
			Filename:    "test.txt",
			ContentType: "text/plain",
			Size:        17,
		},
	}
	_, err = db.GetDB().NewInsert().Model(file).Returning("*").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create file:", err)
	}

	// Create service
	svc, err := service.New[FileModel]()
	if err != nil {
		t.Fatal("Failed to create service:", err)
	}

	// Create metadata
	fileMeta := &metadata.TypeMetadata{
		TypeID:       "file_model_id",
		TypeName:     "FileModel",
		TableName:    "file_models",
		URLParamUUID: "file_id",
		ModelType:    reflect.TypeOf(FileModel{}),
	}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, fileMeta)

	// Test Download
	result, err := svc.Download(ctx, strconv.Itoa(file.ID))
	if err != nil {
		t.Fatal("Download failed:", err)
	}

	// In signed URL mode, should have signed URL, no reader
	if result.Reader != nil {
		t.Error("Expected nil reader in signed URL mode")
	}
	if result.SignedURL != "https://signed.example.com/file" {
		t.Errorf("Expected signed URL, got '%s'", result.SignedURL)
	}
}

func TestService_Download_NotFileResource(t *testing.T) {
	cleanTable(t)

	// Create a regular item (not a file resource)
	db, _ := datastore.Get()
	item := &TestModel{Name: "Regular Item", Email: "regular@example.com"}
	_, err := db.GetDB().NewInsert().Model(item).Returning("*").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create item:", err)
	}

	// Create service
	svc, err := service.New[TestModel]()
	if err != nil {
		t.Fatal("Failed to create service:", err)
	}

	ctx := ctxWithMeta(testModelMeta)

	// Test Download - should fail because TestModel is not a FileResource
	_, err = svc.Download(ctx, strconv.Itoa(item.ID))
	if err != filestore.ErrStorageKeyNotFound {
		t.Errorf("Expected ErrStorageKeyNotFound, got %v", err)
	}
}

func TestService_Download_EmptyStorageKey(t *testing.T) {
	// Create table
	db, _ := datastore.Get()
	_, err := db.GetDB().NewCreateTable().Model((*FileModel)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create file_models table:", err)
	}
	defer db.GetDB().NewDropTable().Model((*FileModel)(nil)).IfExists().Exec(context.Background())

	// Clean table
	db.GetDB().NewDelete().Model((*FileModel)(nil)).Where("1=1").Exec(context.Background())

	// Create file record with empty storage key
	file := &FileModel{
		Name: "Empty File",
		FileFields: filestore.FileFields{
			StorageKey: "", // Empty!
		},
	}
	_, err = db.GetDB().NewInsert().Model(file).Returning("*").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create file:", err)
	}

	// Create service
	svc, err := service.New[FileModel]()
	if err != nil {
		t.Fatal("Failed to create service:", err)
	}

	// Create metadata
	fileMeta := &metadata.TypeMetadata{
		TypeID:       "file_model_id",
		TypeName:     "FileModel",
		TableName:    "file_models",
		URLParamUUID: "file_id",
		ModelType:    reflect.TypeOf(FileModel{}),
	}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, fileMeta)

	// Test Download - should fail because storage key is empty
	_, err = svc.Download(ctx, strconv.Itoa(file.ID))
	if err != filestore.ErrStorageKeyNotFound {
		t.Errorf("Expected ErrStorageKeyNotFound, got %v", err)
	}
}

func TestService_StoreFile(t *testing.T) {
	// Create service
	svc, err := service.New[TestModel]()
	if err != nil {
		t.Fatal("Failed to create service:", err)
	}

	ctx := ctxWithMeta(testModelMeta)

	// Test StoreFile
	content := strings.NewReader("test file content")
	fileMeta := filestore.FileMetadata{
		Filename:    "test.txt",
		ContentType: "text/plain",
		Size:        17,
	}

	key, err := svc.StoreFile(ctx, content, fileMeta)
	if err != nil {
		t.Fatal("StoreFile failed:", err)
	}

	if key == "" {
		t.Error("Expected non-empty storage key")
	}

	// Verify file was stored
	if _, ok := testFileStorage.files[key]; !ok {
		t.Error("File was not stored in mock storage")
	}
	// Clean up
	delete(testFileStorage.files, key)
}

func TestService_RetrieveFile(t *testing.T) {
	// Add file to shared mock storage
	testFileStorage.files["existing-key"] = "stored content"
	defer delete(testFileStorage.files, "existing-key")

	// Create service
	svc, err := service.New[TestModel]()
	if err != nil {
		t.Fatal("Failed to create service:", err)
	}

	ctx := ctxWithMeta(testModelMeta)

	// Test RetrieveFile
	reader, err := svc.RetrieveFile(ctx, "existing-key")
	if err != nil {
		t.Fatal("RetrieveFile failed:", err)
	}
	defer reader.Close()

	content, _ := io.ReadAll(reader)
	if string(content) != "stored content" {
		t.Errorf("Expected 'stored content', got '%s'", string(content))
	}
}

func TestService_RetrieveFile_EmptyKey(t *testing.T) {
	// Create service
	svc, err := service.New[TestModel]()
	if err != nil {
		t.Fatal("Failed to create service:", err)
	}

	ctx := ctxWithMeta(testModelMeta)

	// Test RetrieveFile with empty key
	_, err = svc.RetrieveFile(ctx, "")
	if err != filestore.ErrStorageKeyNotFound {
		t.Errorf("Expected ErrStorageKeyNotFound, got %v", err)
	}
}

func TestService_DeleteStoredFile(t *testing.T) {
	// Add file to shared mock storage
	testFileStorage.files["key-to-delete"] = "content"

	// Create service
	svc, err := service.New[TestModel]()
	if err != nil {
		t.Fatal("Failed to create service:", err)
	}

	ctx := ctxWithMeta(testModelMeta)

	// Verify file exists
	if _, ok := testFileStorage.files["key-to-delete"]; !ok {
		t.Fatal("File should exist before delete")
	}

	// Test DeleteStoredFile
	err = svc.DeleteStoredFile(ctx, "key-to-delete")
	if err != nil {
		t.Fatal("DeleteFile failed:", err)
	}

	// Verify file was deleted
	if _, ok := testFileStorage.files["key-to-delete"]; ok {
		t.Error("File should have been deleted")
	}
}

// Batch operation tests

func TestService_BatchCreate(t *testing.T) {
	cleanTable(t)

	svc, err := service.New[TestModel]()
	if err != nil {
		t.Fatal("Failed to create service:", err)
	}
	ctx := ctxWithMeta(testModelMeta)

	items := []TestModel{
		{Name: "User 1", Email: "batch1@example.com"},
		{Name: "User 2", Email: "batch2@example.com"},
	}

	results, err := svc.BatchCreate(ctx, items)
	if err != nil {
		t.Fatal("BatchCreate failed:", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}
}

func TestService_BatchUpdate(t *testing.T) {
	cleanTable(t)

	svc, err := service.New[TestModel]()
	if err != nil {
		t.Fatal("Failed to create service:", err)
	}
	ctx := ctxWithMeta(testModelMeta)

	// Create items first
	items := []TestModel{
		{Name: "User 1", Email: "batchupd1@example.com"},
		{Name: "User 2", Email: "batchupd2@example.com"},
	}
	created, err := svc.BatchCreate(ctx, items)
	if err != nil {
		t.Fatal("BatchCreate failed:", err)
	}

	// Update them
	updates := []TestModel{
		{ID: created[0].ID, Name: "Updated 1", Email: "batchupd1@example.com"},
		{ID: created[1].ID, Name: "Updated 2", Email: "batchupd2@example.com"},
	}

	results, err := svc.BatchUpdate(ctx, updates)
	if err != nil {
		t.Fatal("BatchUpdate failed:", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}

	if results[0].Name != "Updated 1" {
		t.Errorf("Expected 'Updated 1', got '%s'", results[0].Name)
	}
}

func TestService_BatchDelete(t *testing.T) {
	cleanTable(t)

	svc, err := service.New[TestModel]()
	if err != nil {
		t.Fatal("Failed to create service:", err)
	}
	ctx := ctxWithMeta(testModelMeta)

	// Create items first
	items := []TestModel{
		{Name: "User 1", Email: "batchdel1@example.com"},
		{Name: "User 2", Email: "batchdel2@example.com"},
	}
	created, err := svc.BatchCreate(ctx, items)
	if err != nil {
		t.Fatal("BatchCreate failed:", err)
	}

	// Delete them
	deletes := []TestModel{
		{ID: created[0].ID},
		{ID: created[1].ID},
	}

	err = svc.BatchDelete(ctx, deletes)
	if err != nil {
		t.Fatal("BatchDelete failed:", err)
	}

	// Verify they're gone
	all, _, _, err := svc.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}
	if len(all) != 0 {
		t.Errorf("Expected 0 users after delete, got %d", len(all))
	}
}
