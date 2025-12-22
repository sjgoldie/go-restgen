package router

import (
	"testing"

	"github.com/sjgoldie/go-restgen/metadata"
)

func TestMergeQueryConfigs(t *testing.T) {
	t.Run("empty configs returns clone of original", func(t *testing.T) {
		original := &metadata.TypeMetadata{
			TypeID:           "test",
			FilterableFields: []string{"Name"},
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
		if len(result.FilterableFields) != 1 || result.FilterableFields[0] != "Name" {
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
				FilterableFields: []string{"Name", "Status"},
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
				FilterableFields: []string{"Name"},
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
				DefaultSort: "Name",
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
		if result.DefaultSort != "Name" {
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
		parentGet := &AuthConfig{Scopes: []string{"read"}}
		parentList := &AuthConfig{Scopes: []string{"list"}}

		b := &Builder{
			parentAuthGet:  parentGet,
			parentAuthList: parentList,
		}

		authMap := map[string]*AuthConfig{
			MethodGet: {Scopes: []string{"child:read"}},
		}

		registerChildAuthConfig(b, "", authMap)

		// Should not have created ChildAuth maps
		if parentGet.ChildAuth != nil {
			t.Error("ChildAuth should not be created for empty relationName")
		}
		if parentList.ChildAuth != nil {
			t.Error("ChildAuth should not be created for empty relationName")
		}
	})

	t.Run("nil childAuthGet does nothing", func(t *testing.T) {
		parentGet := &AuthConfig{Scopes: []string{"read"}}

		b := &Builder{
			parentAuthGet: parentGet,
		}

		authMap := map[string]*AuthConfig{
			// No MethodGet entry
		}

		registerChildAuthConfig(b, "posts", authMap)

		if parentGet.ChildAuth != nil {
			t.Error("ChildAuth should not be created when child has no GET auth")
		}
	})

	t.Run("registers to parentAuthGet", func(t *testing.T) {
		parentGet := &AuthConfig{Scopes: []string{"read"}}
		childGet := &AuthConfig{Scopes: []string{"child:read"}}

		b := &Builder{
			parentAuthGet: parentGet,
		}

		authMap := map[string]*AuthConfig{
			MethodGet: childGet,
		}

		registerChildAuthConfig(b, "posts", authMap)

		if parentGet.ChildAuth == nil {
			t.Fatal("ChildAuth should be created")
		}
		if parentGet.ChildAuth["posts"] != childGet {
			t.Error("child auth not registered correctly")
		}
	})

	t.Run("registers to parentAuthList", func(t *testing.T) {
		parentList := &AuthConfig{Scopes: []string{"list"}}
		childGet := &AuthConfig{Scopes: []string{"child:read"}}

		b := &Builder{
			parentAuthList: parentList,
		}

		authMap := map[string]*AuthConfig{
			MethodGet: childGet,
		}

		registerChildAuthConfig(b, "comments", authMap)

		if parentList.ChildAuth == nil {
			t.Fatal("ChildAuth should be created")
		}
		if parentList.ChildAuth["comments"] != childGet {
			t.Error("child auth not registered correctly")
		}
	})

	t.Run("registers to both parentAuthGet and parentAuthList", func(t *testing.T) {
		parentGet := &AuthConfig{Scopes: []string{"read"}}
		parentList := &AuthConfig{Scopes: []string{"list"}}
		childGet := &AuthConfig{Scopes: []string{"child:read"}}

		b := &Builder{
			parentAuthGet:  parentGet,
			parentAuthList: parentList,
		}

		authMap := map[string]*AuthConfig{
			MethodGet: childGet,
		}

		registerChildAuthConfig(b, "items", authMap)

		if parentGet.ChildAuth == nil || parentGet.ChildAuth["items"] != childGet {
			t.Error("child auth not registered to parentAuthGet")
		}
		if parentList.ChildAuth == nil || parentList.ChildAuth["items"] != childGet {
			t.Error("child auth not registered to parentAuthList")
		}
	})

	t.Run("appends to existing ChildAuth", func(t *testing.T) {
		existingChild := &AuthConfig{Scopes: []string{"existing"}}
		parentGet := &AuthConfig{
			Scopes: []string{"read"},
			ChildAuth: map[string]*AuthConfig{
				"existing": existingChild,
			},
		}
		newChild := &AuthConfig{Scopes: []string{"new:read"}}

		b := &Builder{
			parentAuthGet: parentGet,
		}

		authMap := map[string]*AuthConfig{
			MethodGet: newChild,
		}

		registerChildAuthConfig(b, "new", authMap)

		// Should have both entries
		if len(parentGet.ChildAuth) != 2 {
			t.Errorf("expected 2 child auths, got %d", len(parentGet.ChildAuth))
		}
		if parentGet.ChildAuth["existing"] != existingChild {
			t.Error("existing child auth was lost")
		}
		if parentGet.ChildAuth["new"] != newChild {
			t.Error("new child auth not added")
		}
	})
}
