package datastore

import (
	"context"
	"testing"

	"github.com/sjgoldie/go-restgen/metadata"
)

func TestApplyParentFilters_MissingParentID(t *testing.T) {
	f := setupPartitionFixture(t)
	f.projectMeta.URLParamUUID = "project"
	f.projectMeta.Partitions = nil
	f.taskMeta.Partitions = nil
	w := &Wrapper[partTask]{Store: f.db}

	count := func(t *testing.T, ctx context.Context) int {
		t.Helper()
		q := f.db.GetDB().NewSelect().Model((*partTask)(nil))
		q, restrict, err := w.applyParentFiltersWithMeta(ctx, q, f.taskMeta)
		if err != nil {
			t.Fatal(err)
		}
		n, err := accessQuery(q, restrict, nil).Count(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	request := context.WithValue(context.Background(), metadata.IncludePartitionScopesKey, map[string]metadata.PartitionScope{})

	if got := count(t, request); got != 0 {
		t.Errorf("request without the parent's ID: got %d tasks, want 0", got)
	}
	other := context.WithValue(request, metadata.ParentIDsKey, map[string]string{"another-route": "1"})
	if got := count(t, other); got != 0 {
		t.Errorf("request with another route's ID only: got %d tasks, want 0", got)
	}
	scoped := context.WithValue(request, metadata.ParentIDsKey, map[string]string{"project": "1"})
	if got := count(t, scoped); got != 1 {
		t.Errorf("request with the parent's ID: got %d tasks, want 1", got)
	}
	if got := count(t, context.Background()); got != 2 {
		t.Errorf("internal use without a request: got %d tasks, want every task", got)
	}
}

func TestReassertParentKey(t *testing.T) {
	f := setupPartitionFixture(t)
	tasks := &Wrapper[partTask]{Store: f.db}

	item := &partTask{ProjectID: 2, Title: "moved"}
	tasks.reassertParentKey(f.taskMeta, &partTask{ProjectID: 1}, item)
	if item.ProjectID != 1 || item.Title != "moved" {
		t.Errorf("the parent link must come from the existing row: got %+v", item)
	}

	root := &partTask{ProjectID: 2}
	tasks.reassertParentKey(&metadata.TypeMetadata{ModelType: f.taskMeta.ModelType}, &partTask{ProjectID: 1}, root)
	if root.ProjectID != 2 {
		t.Errorf("a route with no parent is unchanged: got %+v", root)
	}

	managers := &Wrapper[partManager]{Store: f.db}
	manager := &partManager{ID: 2, Name: "renamed"}
	managers.reassertParentKey(f.managerMeta, &partManager{ID: 1}, manager)
	if manager.ID != 2 {
		t.Errorf("a row whose parent holds the link is unchanged: got %+v", manager)
	}
}
