package datastore

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	apperrors "github.com/sjgoldie/go-restgen/errors"
	"github.com/sjgoldie/go-restgen/metadata"
)

func TestResolveSingleRouteKey(t *testing.T) {
	f := setupPartitionFixture(t)
	t.Cleanup(func() { singleton = nil; once = sync.Once{} })
	ctx := context.Background()
	manager := &metadata.TypeMetadata{
		TypeName:      "partManager",
		ModelType:     reflect.TypeFor[partManager](),
		ParentMeta:    f.projectMeta,
		ParentFKField: "ManagerID",
	}

	if key, err := ResolveSingleRouteKey(ctx, f.db, manager, "2"); err != nil || key != "2" {
		t.Errorf("project 2's manager: got %q, %v", key, err)
	}
	if _, err := ResolveSingleRouteKey(ctx, f.db, manager, "99"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Errorf("missing parent: got %v, want ErrNotFound", err)
	}

	if _, err := f.db.GetDB().ExecContext(ctx, "INSERT INTO part_projects (region, manager_id) VALUES ('emea', NULL)"); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveSingleRouteKey(ctx, f.db, manager, "3"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Errorf("parent pointing at no row: got %v, want ErrNotFound", err)
	}

	notSingle := &metadata.TypeMetadata{TypeName: "partManager", ModelType: reflect.TypeFor[partManager]()}
	if _, err := ResolveSingleRouteKey(ctx, f.db, notSingle, "1"); err == nil {
		t.Error("not a nested single route: expected an error")
	}
	badField := *manager
	badField.ParentFKField = "Missing"
	if _, err := ResolveSingleRouteKey(ctx, f.db, &badField, "1"); err == nil {
		t.Error("unknown parent field: expected an error")
	}
	composite := *manager
	composite.ParentMeta = &metadata.TypeMetadata{ModelType: reflect.TypeFor[cuPair]()}
	composite.ParentFKField = "Subject"
	if _, err := ResolveSingleRouteKey(ctx, f.db, &composite, "1"); err == nil {
		t.Error("parent with a composite key: expected an error")
	}
}
