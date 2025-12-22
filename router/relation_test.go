package router

import (
	"testing"
)

func TestWithRelationName(t *testing.T) {
	config := WithRelationName("Posts")

	if config.Name != "Posts" {
		t.Errorf("expected relation name 'Posts', got '%s'", config.Name)
	}
}

func TestAsSingleRoute(t *testing.T) {
	t.Run("with parent FK field", func(t *testing.T) {
		config := AsSingleRoute("AuthorID")

		if config.ParentFKField != "AuthorID" {
			t.Errorf("expected ParentFKField 'AuthorID', got '%s'", config.ParentFKField)
		}
		if config.WithPut {
			t.Error("expected WithPut to be false")
		}
	})

	t.Run("with empty parent FK field for root routes", func(t *testing.T) {
		config := AsSingleRoute("")

		if config.ParentFKField != "" {
			t.Errorf("expected empty ParentFKField, got '%s'", config.ParentFKField)
		}
		if config.WithPut {
			t.Error("expected WithPut to be false")
		}
	})
}

func TestAsSingleRouteWithPut(t *testing.T) {
	t.Run("with parent FK field", func(t *testing.T) {
		config := AsSingleRouteWithPut("AuthorID")

		if config.ParentFKField != "AuthorID" {
			t.Errorf("expected ParentFKField 'AuthorID', got '%s'", config.ParentFKField)
		}
		if !config.WithPut {
			t.Error("expected WithPut to be true")
		}
	})

	t.Run("with empty parent FK field for root routes", func(t *testing.T) {
		config := AsSingleRouteWithPut("")

		if config.ParentFKField != "" {
			t.Errorf("expected empty ParentFKField, got '%s'", config.ParentFKField)
		}
		if !config.WithPut {
			t.Error("expected WithPut to be true")
		}
	})
}
