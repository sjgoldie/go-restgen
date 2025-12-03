package metadata

import (
	"context"
	"strings"
	"testing"

	apperrors "github.com/sjgoldie/go-restgen/errors"
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

func TestFromContext(t *testing.T) {
	// Create metadata
	meta := &TypeMetadata{
		TypeID:       "test_id_1",
		TypeName:     "TestUser",
		TableName:    "test_users",
		URLParamUUID: "param_uuid_1",
	}

	// Add to context
	ctx := context.WithValue(context.Background(), MetadataKey, meta)

	// Retrieve from context
	retrieved, err := FromContext(ctx)
	if err != nil {
		t.Fatalf("FromContext() returned error: %v", err)
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

func TestFromContextEmpty(t *testing.T) {
	// Empty context
	ctx := context.Background()

	// Retrieve from empty context
	retrieved, err := FromContext(ctx)
	if err == nil {
		t.Error("FromContext() should return error for empty context")
	}
	if retrieved != nil {
		t.Error("FromContext() should return nil metadata for empty context")
	}
	if err != apperrors.ErrMetadataNotFound {
		t.Errorf("Expected ErrMetadataNotFound, got %v", err)
	}
}

func TestFromContextWrongType(t *testing.T) {
	// Add wrong type to context
	ctx := context.WithValue(context.Background(), MetadataKey, "wrong type")

	// Retrieve from context
	retrieved, err := FromContext(ctx)
	if err == nil {
		t.Error("FromContext() should return error for wrong type")
	}
	if retrieved != nil {
		t.Error("FromContext() should return nil metadata for wrong type")
	}
	if err != apperrors.ErrMetadataNotFound {
		t.Errorf("Expected ErrMetadataNotFound, got %v", err)
	}
}

func TestParentMetaChain(t *testing.T) {
	// Create parent metadata
	parentMeta := &TypeMetadata{
		TypeID:       "parent_id",
		TypeName:     "TestUser",
		TableName:    "test_users",
		URLParamUUID: "parent_uuid",
		ParentMeta:   nil,
	}

	// Create child metadata with parent
	childMeta := &TypeMetadata{
		TypeID:        "child_id",
		TypeName:      "TestPost",
		TableName:     "test_posts",
		URLParamUUID:  "child_uuid",
		ParentMeta:    parentMeta,
		ForeignKeyCol: "user_id",
	}

	// Verify parent chain
	if childMeta.ParentMeta == nil {
		t.Fatal("Expected ParentMeta to be set")
	}

	if childMeta.ParentMeta.TypeName != "TestUser" {
		t.Errorf("Expected parent TypeName 'TestUser', got '%s'", childMeta.ParentMeta.TypeName)
	}

	if childMeta.ForeignKeyCol != "user_id" {
		t.Errorf("Expected ForeignKeyCol 'user_id', got '%s'", childMeta.ForeignKeyCol)
	}

	// Parent should have no parent
	if parentMeta.ParentMeta != nil {
		t.Error("Expected root parent to have nil ParentMeta")
	}
}

func TestMultipleLevelParentChain(t *testing.T) {
	// Create three-level hierarchy
	grandparentMeta := &TypeMetadata{
		TypeID:       "gp_id",
		TypeName:     "Organization",
		TableName:    "organizations",
		URLParamUUID: "org_uuid",
	}

	parentMeta := &TypeMetadata{
		TypeID:        "p_id",
		TypeName:      "Team",
		TableName:     "teams",
		URLParamUUID:  "team_uuid",
		ParentMeta:    grandparentMeta,
		ForeignKeyCol: "organization_id",
	}

	childMeta := &TypeMetadata{
		TypeID:        "c_id",
		TypeName:      "Member",
		TableName:     "members",
		URLParamUUID:  "member_uuid",
		ParentMeta:    parentMeta,
		ForeignKeyCol: "team_id",
	}

	// Walk up the chain
	current := childMeta
	levels := []string{}
	for current != nil {
		levels = append(levels, current.TypeName)
		current = current.ParentMeta
	}

	if len(levels) != 3 {
		t.Errorf("Expected 3 levels, got %d", len(levels))
	}

	if levels[0] != "Member" {
		t.Errorf("Expected first level 'Member', got '%s'", levels[0])
	}
	if levels[1] != "Team" {
		t.Errorf("Expected second level 'Team', got '%s'", levels[1])
	}
	if levels[2] != "Organization" {
		t.Errorf("Expected third level 'Organization', got '%s'", levels[2])
	}
}

func TestOwnershipFields(t *testing.T) {
	meta := &TypeMetadata{
		TypeID:          "test_id",
		TypeName:        "TestPost",
		OwnershipFields: []string{"AuthorID", "EditorID"},
		BypassScopes:    []string{"admin", "moderator"},
	}

	if len(meta.OwnershipFields) != 2 {
		t.Errorf("Expected 2 ownership fields, got %d", len(meta.OwnershipFields))
	}
	if meta.OwnershipFields[0] != "AuthorID" {
		t.Errorf("Expected first ownership field 'AuthorID', got '%s'", meta.OwnershipFields[0])
	}
	if meta.OwnershipFields[1] != "EditorID" {
		t.Errorf("Expected second ownership field 'EditorID', got '%s'", meta.OwnershipFields[1])
	}

	if len(meta.BypassScopes) != 2 {
		t.Errorf("Expected 2 bypass scopes, got %d", len(meta.BypassScopes))
	}
	if meta.BypassScopes[0] != "admin" {
		t.Errorf("Expected first bypass scope 'admin', got '%s'", meta.BypassScopes[0])
	}
	if meta.BypassScopes[1] != "moderator" {
		t.Errorf("Expected second bypass scope 'moderator', got '%s'", meta.BypassScopes[1])
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
