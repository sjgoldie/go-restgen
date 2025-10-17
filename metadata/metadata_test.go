package metadata

import (
	"reflect"
	"strings"
	"sync"
	"testing"
)

// Test types
type TestUser struct {
	ID   int
	Name string
}

type TestPost struct {
	ID       int
	UserID   int
	Content  string
	AuthorID string
	EditorID string
}

type TestComment struct {
	ID     int
	PostID int
	Text   string
}

func TestRegisterAndGet(t *testing.T) {
	// Clear registry
	registryLock.Lock()
	registry = make(map[reflect.Type]*TypeMetadata)
	registryLock.Unlock()

	// Register a type
	meta := &TypeMetadata{
		TypeID:        "test_id_1",
		TypeName:      "TestUser",
		TableName:     "test_users",
		URLParamUUID:  "param_uuid_1",
		ParentType:    nil,
		ForeignKeyCol: "",
	}

	Register(meta, reflect.TypeOf(TestUser{}))

	// Get by generic type
	retrieved, err := Get[TestUser]()
	if err != nil {
		t.Fatalf("Get[TestUser]() failed: %v", err)
	}

	if retrieved.TypeID != "test_id_1" {
		t.Errorf("Expected TypeID 'test_id_1', got '%s'", retrieved.TypeID)
	}
	if retrieved.TypeName != "TestUser" {
		t.Errorf("Expected TypeName 'TestUser', got '%s'", retrieved.TypeName)
	}
	if retrieved.TableName != "test_users" {
		t.Errorf("Expected TableName 'test_users', got '%s'", retrieved.TableName)
	}
}

func TestRegisterAndGetWithPointerType(t *testing.T) {
	// Clear registry
	registryLock.Lock()
	registry = make(map[reflect.Type]*TypeMetadata)
	registryLock.Unlock()

	// Register with non-pointer type
	meta := &TypeMetadata{
		TypeID:   "test_id_2",
		TypeName: "TestPost",
	}

	Register(meta, reflect.TypeOf(TestPost{}))

	// Get by pointer type - should still work
	retrieved, err := Get[*TestPost]()
	if err != nil {
		t.Fatalf("Get[*TestPost]() failed: %v", err)
	}

	if retrieved.TypeID != "test_id_2" {
		t.Errorf("Expected TypeID 'test_id_2', got '%s'", retrieved.TypeID)
	}
}

func TestGetByType(t *testing.T) {
	// Clear registry
	registryLock.Lock()
	registry = make(map[reflect.Type]*TypeMetadata)
	registryLock.Unlock()

	// Register a type
	meta := &TypeMetadata{
		TypeID:   "test_id_3",
		TypeName: "TestComment",
	}

	tType := reflect.TypeOf(TestComment{})
	Register(meta, tType)

	// Get by reflect.Type
	retrieved, err := GetByType(tType)
	if err != nil {
		t.Fatalf("GetByType() failed: %v", err)
	}

	if retrieved.TypeID != "test_id_3" {
		t.Errorf("Expected TypeID 'test_id_3', got '%s'", retrieved.TypeID)
	}
}

func TestGetByTypeWithPointer(t *testing.T) {
	// Clear registry
	registryLock.Lock()
	registry = make(map[reflect.Type]*TypeMetadata)
	registryLock.Unlock()

	// Register with non-pointer type
	meta := &TypeMetadata{
		TypeID:   "test_id_4",
		TypeName: "TestUser",
	}

	Register(meta, reflect.TypeOf(TestUser{}))

	// Get by pointer type
	ptrType := reflect.TypeOf(&TestUser{})
	retrieved, err := GetByType(ptrType)
	if err != nil {
		t.Fatalf("GetByType() with pointer failed: %v", err)
	}

	if retrieved.TypeID != "test_id_4" {
		t.Errorf("Expected TypeID 'test_id_4', got '%s'", retrieved.TypeID)
	}
}

func TestGetUnregisteredType(t *testing.T) {
	// Clear registry
	registryLock.Lock()
	registry = make(map[reflect.Type]*TypeMetadata)
	registryLock.Unlock()

	// Try to get unregistered type
	_, err := Get[TestUser]()
	if err == nil {
		t.Fatal("Expected error for unregistered type, got nil")
	}

	expectedError := "type TestUser not registered"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

func TestUpdateOwnership(t *testing.T) {
	// Clear registry
	registryLock.Lock()
	registry = make(map[reflect.Type]*TypeMetadata)
	registryLock.Unlock()

	// Register a type without ownership
	meta := &TypeMetadata{
		TypeID:          "test_id_5",
		TypeName:        "TestPost",
		OwnershipFields: nil,
		BypassScopes:    nil,
	}

	tType := reflect.TypeOf(TestPost{})
	Register(meta, tType)

	// Update ownership
	ownershipFields := []string{"AuthorID", "EditorID"}
	bypassScopes := []string{"admin", "moderator"}

	err := UpdateOwnership(tType, ownershipFields, bypassScopes)
	if err != nil {
		t.Fatalf("UpdateOwnership() failed: %v", err)
	}

	// Verify ownership was updated
	retrieved, _ := GetByType(tType)
	if len(retrieved.OwnershipFields) != 2 {
		t.Errorf("Expected 2 ownership fields, got %d", len(retrieved.OwnershipFields))
	}
	if retrieved.OwnershipFields[0] != "AuthorID" {
		t.Errorf("Expected first ownership field 'AuthorID', got '%s'", retrieved.OwnershipFields[0])
	}
	if retrieved.OwnershipFields[1] != "EditorID" {
		t.Errorf("Expected second ownership field 'EditorID', got '%s'", retrieved.OwnershipFields[1])
	}

	if len(retrieved.BypassScopes) != 2 {
		t.Errorf("Expected 2 bypass scopes, got %d", len(retrieved.BypassScopes))
	}
	if retrieved.BypassScopes[0] != "admin" {
		t.Errorf("Expected first bypass scope 'admin', got '%s'", retrieved.BypassScopes[0])
	}
	if retrieved.BypassScopes[1] != "moderator" {
		t.Errorf("Expected second bypass scope 'moderator', got '%s'", retrieved.BypassScopes[1])
	}
}

func TestUpdateOwnershipWithPointerType(t *testing.T) {
	// Clear registry
	registryLock.Lock()
	registry = make(map[reflect.Type]*TypeMetadata)
	registryLock.Unlock()

	// Register with non-pointer type
	meta := &TypeMetadata{
		TypeID:   "test_id_6",
		TypeName: "TestUser",
	}

	Register(meta, reflect.TypeOf(TestUser{}))

	// Update ownership using pointer type
	ptrType := reflect.TypeOf(&TestUser{})
	err := UpdateOwnership(ptrType, []string{"ID"}, []string{"admin"})
	if err != nil {
		t.Fatalf("UpdateOwnership() with pointer type failed: %v", err)
	}

	// Verify it worked
	retrieved, _ := Get[TestUser]()
	if len(retrieved.OwnershipFields) != 1 {
		t.Errorf("Expected 1 ownership field, got %d", len(retrieved.OwnershipFields))
	}
	if retrieved.OwnershipFields[0] != "ID" {
		t.Errorf("Expected ownership field 'ID', got '%s'", retrieved.OwnershipFields[0])
	}
}

func TestUpdateOwnershipUnregisteredType(t *testing.T) {
	// Clear registry
	registryLock.Lock()
	registry = make(map[reflect.Type]*TypeMetadata)
	registryLock.Unlock()

	// Try to update ownership for unregistered type
	tType := reflect.TypeOf(TestUser{})
	err := UpdateOwnership(tType, []string{"ID"}, []string{"admin"})
	if err == nil {
		t.Fatal("Expected error for unregistered type, got nil")
	}

	expectedError := "type TestUser not registered"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

func TestGenerateTypeID(t *testing.T) {
	// Generate a few IDs
	id1 := GenerateTypeID()
	id2 := GenerateTypeID()
	id3 := GenerateTypeID()

	// Check they're not empty
	if id1 == "" {
		t.Error("GenerateTypeID() returned empty string")
	}
	if id2 == "" {
		t.Error("GenerateTypeID() returned empty string")
	}
	if id3 == "" {
		t.Error("GenerateTypeID() returned empty string")
	}

	// Check they're unique
	if id1 == id2 || id1 == id3 || id2 == id3 {
		t.Error("GenerateTypeID() generated duplicate IDs")
	}

	// Check they don't contain hyphens (should be replaced with underscores)
	if strings.Contains(id1, "-") {
		t.Error("GenerateTypeID() returned ID with hyphens")
	}
	if strings.Contains(id2, "-") {
		t.Error("GenerateTypeID() returned ID with hyphens")
	}
	if strings.Contains(id3, "-") {
		t.Error("GenerateTypeID() returned ID with hyphens")
	}

	// Check they contain underscores (UUID format with hyphens replaced)
	if !strings.Contains(id1, "_") {
		t.Error("GenerateTypeID() returned ID without underscores")
	}
}

func TestConcurrentAccess(t *testing.T) {
	// Clear registry
	registryLock.Lock()
	registry = make(map[reflect.Type]*TypeMetadata)
	registryLock.Unlock()

	// Register a type
	meta := &TypeMetadata{
		TypeID:   "test_id_7",
		TypeName: "TestUser",
	}
	Register(meta, reflect.TypeOf(TestUser{}))

	// Concurrent reads and writes
	var wg sync.WaitGroup
	iterations := 100

	// Concurrent reads
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := Get[TestUser]()
			if err != nil {
				t.Errorf("Concurrent Get[TestUser]() failed: %v", err)
			}
		}()
	}

	// Concurrent ownership updates
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			fields := []string{"field_" + string(rune(i%10+'0'))}
			err := UpdateOwnership(reflect.TypeOf(TestUser{}), fields, nil)
			if err != nil {
				t.Errorf("Concurrent UpdateOwnership() failed: %v", err)
			}
		}(i)
	}

	wg.Wait()
}

func TestMetadataWithParentType(t *testing.T) {
	// Clear registry
	registryLock.Lock()
	registry = make(map[reflect.Type]*TypeMetadata)
	registryLock.Unlock()

	// Register parent type
	parentType := reflect.TypeOf(TestUser{})
	parentMeta := &TypeMetadata{
		TypeID:        "parent_id",
		TypeName:      "TestUser",
		TableName:     "test_users",
		ParentType:    nil,
		ForeignKeyCol: "",
	}
	Register(parentMeta, parentType)

	// Register child type with parent relationship
	childType := reflect.TypeOf(TestPost{})
	childMeta := &TypeMetadata{
		TypeID:        "child_id",
		TypeName:      "TestPost",
		TableName:     "test_posts",
		ParentType:    parentType,
		ForeignKeyCol: "user_id",
	}
	Register(childMeta, childType)

	// Retrieve and verify child has parent relationship
	retrieved, err := Get[TestPost]()
	if err != nil {
		t.Fatalf("Get[TestPost]() failed: %v", err)
	}

	if retrieved.ParentType == nil {
		t.Fatal("Expected ParentType to be set, got nil")
	}

	if retrieved.ParentType != parentType {
		t.Error("ParentType doesn't match registered parent type")
	}

	if retrieved.ForeignKeyCol != "user_id" {
		t.Errorf("Expected ForeignKeyCol 'user_id', got '%s'", retrieved.ForeignKeyCol)
	}
}

func TestUpdateOwnershipMultipleTimes(t *testing.T) {
	// Clear registry
	registryLock.Lock()
	registry = make(map[reflect.Type]*TypeMetadata)
	registryLock.Unlock()

	// Register a type
	meta := &TypeMetadata{
		TypeID:   "test_id_8",
		TypeName: "TestPost",
	}
	tType := reflect.TypeOf(TestPost{})
	Register(meta, tType)

	// First update
	err := UpdateOwnership(tType, []string{"AuthorID"}, []string{"admin"})
	if err != nil {
		t.Fatalf("First UpdateOwnership() failed: %v", err)
	}

	retrieved, _ := Get[TestPost]()
	if len(retrieved.OwnershipFields) != 1 || retrieved.OwnershipFields[0] != "AuthorID" {
		t.Error("First update didn't set ownership correctly")
	}

	// Second update (should replace first)
	err = UpdateOwnership(tType, []string{"AuthorID", "EditorID"}, []string{"admin", "moderator"})
	if err != nil {
		t.Fatalf("Second UpdateOwnership() failed: %v", err)
	}

	retrieved, _ = Get[TestPost]()
	if len(retrieved.OwnershipFields) != 2 {
		t.Errorf("Expected 2 ownership fields after second update, got %d", len(retrieved.OwnershipFields))
	}
	if len(retrieved.BypassScopes) != 2 {
		t.Errorf("Expected 2 bypass scopes after second update, got %d", len(retrieved.BypassScopes))
	}
}
