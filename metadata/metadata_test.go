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

func TestQueryOptionsFromContext(t *testing.T) {
	// Create QueryOptions
	opts := &QueryOptions{
		Filters: map[string]FilterValue{
			"Name": {Value: "test", Operator: "eq"},
		},
		Sort: []SortField{
			{Field: "Name", Desc: false},
		},
		Limit:      10,
		Offset:     5,
		CountTotal: true,
	}

	// Add to context
	ctx := context.WithValue(context.Background(), QueryOptionsKey, opts)

	// Retrieve from context
	retrieved := QueryOptionsFromContext(ctx)
	if retrieved == nil {
		t.Fatal("QueryOptionsFromContext() returned nil")
		return // unreachable, but satisfies staticcheck
	}

	if retrieved.Limit != 10 {
		t.Errorf("Expected Limit 10, got %d", retrieved.Limit)
	}
	if retrieved.Offset != 5 {
		t.Errorf("Expected Offset 5, got %d", retrieved.Offset)
	}
	if !retrieved.CountTotal {
		t.Error("Expected CountTotal to be true")
	}
	if len(retrieved.Filters) != 1 {
		t.Errorf("Expected 1 filter, got %d", len(retrieved.Filters))
	}
	if retrieved.Filters["Name"].Value != "test" {
		t.Errorf("Expected filter value 'test', got '%s'", retrieved.Filters["Name"].Value)
	}
	if len(retrieved.Sort) != 1 {
		t.Errorf("Expected 1 sort field, got %d", len(retrieved.Sort))
	}
	if retrieved.Sort[0].Field != "Name" {
		t.Errorf("Expected sort field 'Name', got '%s'", retrieved.Sort[0].Field)
	}
}

func TestQueryOptionsFromContextEmpty(t *testing.T) {
	// Empty context - should return nil (not an error)
	ctx := context.Background()
	retrieved := QueryOptionsFromContext(ctx)
	if retrieved != nil {
		t.Error("QueryOptionsFromContext() should return nil for empty context")
	}
}

func TestQueryOptionsFromContextWrongType(t *testing.T) {
	// Add wrong type to context
	ctx := context.WithValue(context.Background(), QueryOptionsKey, "wrong type")

	// Should return nil (type assertion fails)
	retrieved := QueryOptionsFromContext(ctx)
	if retrieved != nil {
		t.Error("QueryOptionsFromContext() should return nil for wrong type")
	}
}

func TestAllowedIncludesFromContext(t *testing.T) {
	// Create AllowedIncludes
	includes := AllowedIncludes{
		"Posts":    true,  // Apply ownership
		"Comments": false, // Bypass ownership
	}

	// Add to context
	ctx := context.WithValue(context.Background(), AllowedIncludesKey, includes)

	// Retrieve from context
	retrieved := AllowedIncludesFromContext(ctx)
	if retrieved == nil {
		t.Fatal("AllowedIncludesFromContext() returned nil")
	}

	if len(retrieved) != 2 {
		t.Errorf("Expected 2 includes, got %d", len(retrieved))
	}

	if applyOwnership, ok := retrieved["Posts"]; !ok {
		t.Error("Expected 'Posts' in includes")
	} else if !applyOwnership {
		t.Error("Expected 'Posts' to have ApplyOwnership=true")
	}

	if applyOwnership, ok := retrieved["Comments"]; !ok {
		t.Error("Expected 'Comments' in includes")
	} else if applyOwnership {
		t.Error("Expected 'Comments' to have ApplyOwnership=false (bypass)")
	}
}

func TestAllowedIncludesFromContextEmpty(t *testing.T) {
	// Empty context - should return nil
	ctx := context.Background()
	retrieved := AllowedIncludesFromContext(ctx)
	if retrieved != nil {
		t.Error("AllowedIncludesFromContext() should return nil for empty context")
	}
}

func TestTypeMetadata_Clone(t *testing.T) {
	// Create a fully populated metadata
	parentMeta := &TypeMetadata{
		TypeID:   "parent_id",
		TypeName: "Parent",
	}

	childMeta1 := &TypeMetadata{
		TypeID:   "child1_id",
		TypeName: "Child1",
	}

	original := &TypeMetadata{
		TypeID:           "test_id",
		TypeName:         "TestType",
		TableName:        "test_table",
		URLParamUUID:     "param_uuid",
		ForeignKeyCol:    "parent_id",
		OwnershipFields:  []string{"OwnerID", "CreatorID"},
		BypassScopes:     []string{"admin", "superuser"},
		FilterableFields: []string{"Name", "Status"},
		SortableFields:   []string{"Name", "CreatedAt"},
		DefaultSort:      "-CreatedAt",
		DefaultLimit:     20,
		MaxLimit:         100,
		ParentMeta:       parentMeta,
		ChildMeta: map[string]*TypeMetadata{
			"child1": childMeta1,
		},
	}

	// Clone it
	cloned := original.Clone()

	// Verify all fields are copied
	t.Run("value fields copied", func(t *testing.T) {
		if cloned.TypeID != original.TypeID {
			t.Errorf("TypeID: expected %q, got %q", original.TypeID, cloned.TypeID)
		}
		if cloned.TypeName != original.TypeName {
			t.Errorf("TypeName: expected %q, got %q", original.TypeName, cloned.TypeName)
		}
		if cloned.TableName != original.TableName {
			t.Errorf("TableName: expected %q, got %q", original.TableName, cloned.TableName)
		}
		if cloned.URLParamUUID != original.URLParamUUID {
			t.Errorf("URLParamUUID: expected %q, got %q", original.URLParamUUID, cloned.URLParamUUID)
		}
		if cloned.ForeignKeyCol != original.ForeignKeyCol {
			t.Errorf("ForeignKeyCol: expected %q, got %q", original.ForeignKeyCol, cloned.ForeignKeyCol)
		}
		if cloned.DefaultSort != original.DefaultSort {
			t.Errorf("DefaultSort: expected %q, got %q", original.DefaultSort, cloned.DefaultSort)
		}
		if cloned.DefaultLimit != original.DefaultLimit {
			t.Errorf("DefaultLimit: expected %d, got %d", original.DefaultLimit, cloned.DefaultLimit)
		}
		if cloned.MaxLimit != original.MaxLimit {
			t.Errorf("MaxLimit: expected %d, got %d", original.MaxLimit, cloned.MaxLimit)
		}
	})

	t.Run("slices are deep copied", func(t *testing.T) {
		// Verify slice contents match
		if len(cloned.OwnershipFields) != len(original.OwnershipFields) {
			t.Fatalf("OwnershipFields length: expected %d, got %d", len(original.OwnershipFields), len(cloned.OwnershipFields))
		}
		for i, v := range original.OwnershipFields {
			if cloned.OwnershipFields[i] != v {
				t.Errorf("OwnershipFields[%d]: expected %q, got %q", i, v, cloned.OwnershipFields[i])
			}
		}

		// Modify cloned slice and verify original is unchanged
		cloned.OwnershipFields[0] = "Modified"
		if original.OwnershipFields[0] == "Modified" {
			t.Error("modifying cloned OwnershipFields affected original - not a deep copy")
		}

		// Same for other slices
		cloned.BypassScopes[0] = "modified_scope"
		if original.BypassScopes[0] == "modified_scope" {
			t.Error("modifying cloned BypassScopes affected original - not a deep copy")
		}

		cloned.FilterableFields[0] = "ModifiedFilter"
		if original.FilterableFields[0] == "ModifiedFilter" {
			t.Error("modifying cloned FilterableFields affected original - not a deep copy")
		}

		cloned.SortableFields[0] = "ModifiedSort"
		if original.SortableFields[0] == "ModifiedSort" {
			t.Error("modifying cloned SortableFields affected original - not a deep copy")
		}
	})

	t.Run("ChildMeta map is deep copied", func(t *testing.T) {
		if len(cloned.ChildMeta) != len(original.ChildMeta) {
			t.Fatalf("ChildMeta length: expected %d, got %d", len(original.ChildMeta), len(cloned.ChildMeta))
		}

		// Values should point to the same metadata (intentionally shared)
		if cloned.ChildMeta["child1"] != original.ChildMeta["child1"] {
			t.Error("ChildMeta values should be shared pointers")
		}

		// But modifying the map itself should not affect original
		cloned.ChildMeta["child2"] = &TypeMetadata{TypeID: "new_child"}
		if _, exists := original.ChildMeta["child2"]; exists {
			t.Error("adding to cloned ChildMeta affected original - map not copied")
		}
	})

	t.Run("ParentMeta is shared", func(t *testing.T) {
		// ParentMeta should be the same pointer (intentionally shared)
		if cloned.ParentMeta != original.ParentMeta {
			t.Error("ParentMeta should be shared between original and clone")
		}
	})

	t.Run("nil slices remain nil", func(t *testing.T) {
		emptyMeta := &TypeMetadata{
			TypeID:   "empty",
			TypeName: "Empty",
		}
		emptyClone := emptyMeta.Clone()

		if emptyClone.OwnershipFields != nil {
			t.Error("nil OwnershipFields should remain nil after clone")
		}
		if emptyClone.BypassScopes != nil {
			t.Error("nil BypassScopes should remain nil after clone")
		}
		if emptyClone.FilterableFields != nil {
			t.Error("nil FilterableFields should remain nil after clone")
		}
		if emptyClone.SortableFields != nil {
			t.Error("nil SortableFields should remain nil after clone")
		}
		if emptyClone.ChildMeta != nil {
			t.Error("nil ChildMeta should remain nil after clone")
		}
	})
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
