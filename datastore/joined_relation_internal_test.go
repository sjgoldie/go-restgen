package datastore

import (
	"context"
	"reflect"
	"testing"

	"github.com/sjgoldie/go-restgen/metadata"
)

func withIncludeScopes(ctx context.Context, scopes map[string]metadata.PartitionScope) context.Context {
	return context.WithValue(ctx, metadata.IncludePartitionScopesKey, scopes)
}

func emeaOnly() metadata.PartitionScope {
	return metadata.PartitionScope{"region": {Values: []string{"emea"}}}
}

func unrestrictedRegion() metadata.PartitionScope {
	return metadata.PartitionScope{"region": {Unrestricted: true}}
}

// managerProjectsMeta describes the projects of a manager as a child of the manager lookup,
// so a relation chain can continue past a level whose foreign key is on its parent.
func managerProjectsMeta(f partitionFixture) *metadata.TypeMetadata {
	return &metadata.TypeMetadata{
		TypeName:      "partProject",
		TableName:     "part_projects",
		ModelType:     reflect.TypeFor[partProject](),
		PKField:       "ID",
		ParentMeta:    f.managerMeta,
		ForeignKeyCol: "manager_id",
		ParentJoinCol: "id",
	}
}

func TestChildLink(t *testing.T) {
	f := setupPartitionFixture(t)
	w := &Wrapper[partProject]{Store: f.db}

	if child, parent := w.childLink(f.taskMeta); child != "project_id" || parent != "id" {
		t.Errorf("foreign key on the child: got %s = %s", child, parent)
	}
	if child, parent := w.childLink(f.managerMeta); child != "id" || parent != "manager_id" {
		t.Errorf("foreign key on the parent: got %s = %s", child, parent)
	}
}

func TestChildRelationsWithAccess(t *testing.T) {
	f := setupPartitionFixture(t)
	w := &Wrapper[partProject]{Store: f.db}

	load := func(t *testing.T, ctx context.Context, parts []string, chain []*metadata.TypeMetadata, ownership bool) []partProject {
		t.Helper()
		var rows []partProject
		q := f.db.GetDB().NewSelect().Model(&rows).OrderExpr("?TableAlias.id")
		q = w.childRelationsWithAccess(ctx, q, f.projectMeta, parts, chain, func(*metadata.TypeMetadata) bool { return ownership })
		if err := q.Scan(ctx); err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 {
			t.Fatalf("the relation must not narrow the projects: got %d", len(rows))
		}
		return rows
	}
	manager := []string{"Manager"}
	managerChain := []*metadata.TypeMetadata{f.managerMeta}

	t.Run("lookup within access", func(t *testing.T) {
		rows := load(t, withIncludeScopes(context.Background(), map[string]metadata.PartitionScope{"Manager": unrestrictedRegion()}), manager, managerChain, false)
		if rows[0].Manager == nil || rows[1].Manager == nil {
			t.Errorf("expected both managers, got %+v %+v", rows[0].Manager, rows[1].Manager)
		}
	})

	t.Run("lookup narrowed in the join", func(t *testing.T) {
		rows := load(t, withIncludeScopes(context.Background(), map[string]metadata.PartitionScope{"Manager": emeaOnly()}), manager, managerChain, false)
		if rows[0].Manager == nil || rows[0].Manager.Name != "emea-manager" || rows[1].Manager != nil {
			t.Errorf("expected only the EMEA project's manager, got %+v %+v", rows[0].Manager, rows[1].Manager)
		}
	})

	t.Run("lookup shared with the caller", func(t *testing.T) {
		ctx := withIncludeScopes(asUser("bob"), map[string]metadata.PartitionScope{"Manager": {"region": {}}})
		ctx = context.WithValue(ctx, metadata.IncludeSharesKey, map[string]*metadata.Share{"Manager": projectShare()})
		rows := load(t, ctx, manager, managerChain, false)
		if rows[0].Manager != nil || rows[1].Manager == nil {
			t.Errorf("expected only the shared project's manager, got %+v %+v", rows[0].Manager, rows[1].Manager)
		}
	})

	t.Run("owned lookup", func(t *testing.T) {
		owned := *f.managerMeta
		owned.OwnershipFields = []string{"Name"}
		chain := []*metadata.TypeMetadata{&owned}
		scopes := map[string]metadata.PartitionScope{"Manager": unrestrictedRegion()}

		rows := load(t, withIncludeScopes(asUser("emea-manager"), scopes), manager, chain, true)
		if rows[0].Manager == nil || rows[1].Manager != nil {
			t.Errorf("expected only the manager owned by the caller, got %+v %+v", rows[0].Manager, rows[1].Manager)
		}
		rows = load(t, withIncludeScopes(context.Background(), scopes), manager, chain, true)
		if rows[0].Manager != nil || rows[1].Manager != nil {
			t.Errorf("no caller: expected no managers, got %+v %+v", rows[0].Manager, rows[1].Manager)
		}
	})

	t.Run("lookup joined through a lookup", func(t *testing.T) {
		ctx := context.Background()
		if _, err := f.db.GetDB().NewCreateTable().Model((*partDesk)(nil)).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.GetDB().NewInsert().Model(&partDesk{Name: "desk"}).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.GetDB().NewUpdate().Model((*partManager)(nil)).Set("desk_id = 1").Where("id = 1").Exec(ctx); err != nil {
			t.Fatal(err)
		}
		deskMeta := &metadata.TypeMetadata{
			TypeName:      "partDesk",
			TableName:     "part_desks",
			ModelType:     reflect.TypeFor[partDesk](),
			PKField:       "ID",
			ParentMeta:    f.managerMeta,
			ForeignKeyCol: "desk_id",
			ParentJoinCol: "id",
			Partitions:    []metadata.Partition{{Name: "region"}},
		}
		chain := []*metadata.TypeMetadata{f.managerMeta, deskMeta}

		scopes := map[string]metadata.PartitionScope{"Manager": unrestrictedRegion(), "Manager.Desk": emeaOnly()}
		rows := load(t, withIncludeScopes(ctx, scopes), []string{"Manager", "Desk"}, chain, false)
		if rows[0].Manager == nil || rows[0].Manager.Desk == nil {
			t.Errorf("desk of the EMEA project's manager: got %+v", rows[0].Manager)
		}

		scopes = map[string]metadata.PartitionScope{"Manager": unrestrictedRegion(), "Manager.Desk": {"region": {Values: []string{"apac"}}}}
		rows = load(t, withIncludeScopes(ctx, scopes), []string{"Manager", "Desk"}, chain, false)
		if rows[0].Manager == nil || rows[0].Manager.Desk != nil {
			t.Errorf("desk outside the APAC grant: expected the manager without its desk, got %+v", rows[0].Manager)
		}
	})

	t.Run("has-many narrowed in its own query", func(t *testing.T) {
		rows := load(t, withIncludeScopes(context.Background(), map[string]metadata.PartitionScope{"Tasks": emeaOnly()}), []string{"Tasks"}, []*metadata.TypeMetadata{f.taskMeta}, false)
		if len(rows[0].Tasks) != 1 || len(rows[1].Tasks) != 0 {
			t.Errorf("expected only the EMEA project's task, got %d and %d", len(rows[0].Tasks), len(rows[1].Tasks))
		}
	})

	t.Run("a relation name not on the model fails the query", func(t *testing.T) {
		var rows []partProject
		ctx := withIncludeScopes(context.Background(), map[string]metadata.PartitionScope{"manager": emeaOnly()})
		q := f.db.GetDB().NewSelect().Model(&rows)
		q = w.childRelationsWithAccess(ctx, q, f.projectMeta, []string{"manager"}, managerChain, func(*metadata.TypeMetadata) bool { return false })
		if err := q.Scan(ctx); err == nil {
			t.Errorf("expected an error, got rows %+v", rows)
		}
	})
}

func TestRelationOwnershipClauses(t *testing.T) {
	f := setupPartitionFixture(t)
	w := &Wrapper[partProject]{Store: f.db}

	if got := w.relationOwnershipClauses(asUser("u"), f.managerMeta, columnRef{}); got != nil {
		t.Errorf("no ownership fields: got %+v", got)
	}

	owned := *f.managerMeta
	owned.OwnershipFields = []string{"Name"}
	owned.BypassScopes = []string{"admin"}

	if got := w.relationOwnershipClauses(context.Background(), &owned, columnRef{}); len(got) != 1 || got[0].query != noRows.query {
		t.Errorf("no caller: got %+v", got)
	}
	admin := context.WithValue(context.Background(), metadata.AuthInfoKey, &metadata.AuthInfo{UserID: "u", Scopes: []string{"admin"}})
	if got := w.relationOwnershipClauses(admin, &owned, columnRef{}); got != nil {
		t.Errorf("bypass scope: got %+v", got)
	}
	got := w.relationOwnershipClauses(asUser("apac-manager"), &owned, columnRef{})
	if len(got) != 1 || countWhere(t, f.db, (*partManager)(nil), got[0]) != 1 {
		t.Errorf("owner: got %+v", got)
	}
}

func TestRelationChains_ForeignKeyOnParent(t *testing.T) {
	f := setupPartitionFixture(t)
	w := &Wrapper[partProject]{Store: f.db}
	db := f.db.GetDB()

	projectsWhere := func(t *testing.T, ctx context.Context, query string, args ...any) int {
		t.Helper()
		count, err := db.NewSelect().Model((*partProject)(nil)).Where(query, args...).Count(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return count
	}
	managerChain := []*metadata.TypeMetadata{f.managerMeta}
	emea := withIncludeScopes(context.Background(), map[string]metadata.PartitionScope{"Manager": emeaOnly()})
	unrestricted := withIncludeScopes(context.Background(), map[string]metadata.PartitionScope{"Manager": unrestrictedRegion()})

	t.Run("exists", func(t *testing.T) {
		if got := projectsWhere(t, unrestricted, "EXISTS (?)", w.buildExistsChain(unrestricted, f.projectMeta, managerChain, []string{"Manager"}, false, nil)); got != 2 {
			t.Errorf("unrestricted: got %d projects, want 2", got)
		}
		if got := projectsWhere(t, emea, "EXISTS (?)", w.buildExistsChain(emea, f.projectMeta, managerChain, []string{"Manager"}, false, nil)); got != 1 {
			t.Errorf("EMEA: got %d projects, want 1", got)
		}
	})

	t.Run("count", func(t *testing.T) {
		if got := projectsWhere(t, emea, "(?) = 1", w.buildCountChain(emea, f.projectMeta, managerChain, []string{"Manager"}, false)); got != 1 {
			t.Errorf("EMEA: got %d projects, want 1", got)
		}
		chain := []*metadata.TypeMetadata{f.managerMeta, managerProjectsMeta(f)}
		if got := projectsWhere(t, unrestricted, "(?) = 1", w.buildCountChain(unrestricted, f.projectMeta, chain, []string{"Manager", "Projects"}, false)); got != 2 {
			t.Errorf("through the lookup: got %d projects, want 2", got)
		}
	})

	t.Run("grouped counts", func(t *testing.T) {
		pks := []any{"1", "2"}
		counts, err := w.queryRelationCounts(emea, f.projectMeta, managerChain, []string{"Manager"}, pks, false)
		if err != nil || len(counts) != 1 || counts["1"] != 1 {
			t.Errorf("lookup: got %v, %v", counts, err)
		}

		chain := []*metadata.TypeMetadata{f.managerMeta, managerProjectsMeta(f)}
		counts, err = w.queryRelationCounts(unrestricted, f.projectMeta, chain, []string{"Manager", "Projects"}, pks, false)
		if err != nil || counts["1"] != 1 || counts["2"] != 1 {
			t.Errorf("through the lookup: got %v, %v", counts, err)
		}

		tasks := withIncludeScopes(context.Background(), map[string]metadata.PartitionScope{"Tasks": emeaOnly()})
		counts, err = w.queryRelationCounts(tasks, f.projectMeta, []*metadata.TypeMetadata{f.taskMeta}, []string{"Tasks"}, pks, false)
		if err != nil || len(counts) != 1 || counts["1"] != 1 {
			t.Errorf("foreign key on the child: got %v, %v", counts, err)
		}
	})
}
