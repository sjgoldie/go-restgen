package datastore

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/uptrace/bun"

	apperrors "github.com/sjgoldie/go-restgen/errors"
	"github.com/sjgoldie/go-restgen/metadata"
)

type partProject struct {
	bun.BaseModel `bun:"table:part_projects"`
	ID            int          `bun:"id,pk,autoincrement"`
	Region        string       `bun:"region" json:"region"`
	Level         int          `bun:"level"`
	Untagged      string       `bun:"untagged" json:"-"`
	ManagerID     int          `bun:"manager_id"`
	Manager       *partManager `bun:"rel:belongs-to,join:manager_id=id"`
	Tasks         []*partTask  `bun:"rel:has-many,join:id=project_id"`
}

type partTask struct {
	bun.BaseModel `bun:"table:part_tasks"`
	ID            int    `bun:"id,pk,autoincrement"`
	ProjectID     int    `bun:"project_id"`
	Title         string `bun:"title"`
}

type partComment struct {
	bun.BaseModel `bun:"table:part_comments"`
	ID            int    `bun:"id,pk,autoincrement"`
	TaskID        int    `bun:"task_id"`
	Text          string `bun:"text"`
}

type partManager struct {
	bun.BaseModel `bun:"table:part_managers"`
	ID            int       `bun:"id,pk,autoincrement"`
	Name          string    `bun:"name"`
	DeskID        int       `bun:"desk_id,nullzero"`
	Desk          *partDesk `bun:"rel:belongs-to,join:desk_id=id"`
}

type partDesk struct {
	bun.BaseModel `bun:"table:part_desks"`
	ID            int    `bun:"id,pk,autoincrement"`
	Name          string `bun:"name"`
}

type partProjectShare struct {
	bun.BaseModel `bun:"table:part_project_shares"`
	ID            int    `bun:"id,pk,autoincrement"`
	ProjectID     int    `bun:"project_id"`
	UserID        string `bun:"user_id"`
	Level         string `bun:"level"`
}

type partitionFixture struct {
	db          *SQLite
	projectMeta *metadata.TypeMetadata
	taskMeta    *metadata.TypeMetadata
	commentMeta *metadata.TypeMetadata
	managerMeta *metadata.TypeMetadata
}

// setupPartitionFixture creates an EMEA project (ID 1) and an APAC project (ID 2), each with
// a manager, a task, and a comment. Bob has a viewer share on project 2.
func setupPartitionFixture(t *testing.T) partitionFixture {
	t.Helper()
	db, cleanup := setupHelperTestDB(t)
	t.Cleanup(cleanup)
	_ = Initialize(db)
	ctx := context.Background()

	for _, model := range []any{(*partProject)(nil), (*partTask)(nil), (*partComment)(nil), (*partManager)(nil), (*partProjectShare)(nil)} {
		if _, err := db.GetDB().NewCreateTable().Model(model).Exec(ctx); err != nil {
			t.Fatal(err)
		}
	}

	managers := []partManager{{Name: "emea-manager"}, {Name: "apac-manager"}}
	if _, err := db.GetDB().NewInsert().Model(&managers).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	projects := []partProject{
		{Region: "emea", Level: 1, ManagerID: 1},
		{Region: "apac", Level: 2, ManagerID: 2},
	}
	if _, err := db.GetDB().NewInsert().Model(&projects).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	tasks := []partTask{{ProjectID: 1, Title: "emea"}, {ProjectID: 2, Title: "apac"}}
	if _, err := db.GetDB().NewInsert().Model(&tasks).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	comments := []partComment{{TaskID: 1, Text: "emea"}, {TaskID: 2, Text: "apac"}}
	if _, err := db.GetDB().NewInsert().Model(&comments).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	shares := []partProjectShare{{ProjectID: 2, UserID: "bob", Level: "viewer"}}
	if _, err := db.GetDB().NewInsert().Model(&shares).Exec(ctx); err != nil {
		t.Fatal(err)
	}

	projectMeta := &metadata.TypeMetadata{
		TypeName:   "partProject",
		TableName:  "part_projects",
		ModelType:  reflect.TypeFor[partProject](),
		PKField:    "ID",
		Partitions: []metadata.Partition{{Name: "region", Field: "Region"}},
	}
	taskMeta := &metadata.TypeMetadata{
		TypeName:      "partTask",
		TableName:     "part_tasks",
		ModelType:     reflect.TypeFor[partTask](),
		PKField:       "ID",
		ParentMeta:    projectMeta,
		ForeignKeyCol: "project_id",
		ParentJoinCol: "id",
		Partitions:    []metadata.Partition{{Name: "region"}},
	}
	commentMeta := &metadata.TypeMetadata{
		TypeName:      "partComment",
		TableName:     "part_comments",
		ModelType:     reflect.TypeFor[partComment](),
		PKField:       "ID",
		ParentMeta:    taskMeta,
		ForeignKeyCol: "task_id",
		ParentJoinCol: "id",
		Partitions:    []metadata.Partition{{Name: "region"}},
	}
	managerMeta := &metadata.TypeMetadata{
		TypeName:      "partManager",
		TableName:     "part_managers",
		ModelType:     reflect.TypeFor[partManager](),
		PKField:       "ID",
		ParentMeta:    projectMeta,
		ForeignKeyCol: "manager_id",
		ParentJoinCol: "id",
		Partitions:    []metadata.Partition{{Name: "region"}},
	}

	return partitionFixture{db: db, projectMeta: projectMeta, taskMeta: taskMeta, commentMeta: commentMeta, managerMeta: managerMeta}
}

// countWhere counts rows of model narrowed by a single clause.
func countWhere(t *testing.T, db *SQLite, model any, c clause) int {
	t.Helper()
	count, err := db.GetDB().NewSelect().Model(model).Where(c.query, c.args...).Count(context.Background())
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	return count
}

func TestPartitionValueClause(t *testing.T) {
	f := setupPartitionFixture(t)
	ctx := context.Background()

	tests := []struct {
		name   string
		model  any
		meta   *metadata.TypeMetadata
		values []string
		want   int
	}{
		{"own field", (*partProject)(nil), f.projectMeta, []string{"emea"}, 1},
		{"own field, several values", (*partProject)(nil), f.projectMeta, []string{"emea", "apac"}, 2},
		{"inherited from parent", (*partTask)(nil), f.taskMeta, []string{"apac"}, 1},
		{"inherited through two levels", (*partComment)(nil), f.commentMeta, []string{"emea"}, 1},
		{"inherited with the foreign key on the parent", (*partManager)(nil), f.managerMeta, []string{"apac"}, 1},
		{"no values", (*partProject)(nil), f.projectMeta, nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &Wrapper[partProject]{Store: f.db}
			c := w.partitionValueClause(ctx, tt.meta, "region", columnRef{}, tt.values)
			if got := countWhere(t, f.db, tt.model, c); got != tt.want {
				t.Errorf("got %d rows, want %d", got, tt.want)
			}
		})
	}

	t.Run("closed when it cannot be resolved", func(t *testing.T) {
		w := &Wrapper[partProject]{Store: f.db}
		cases := map[string]*metadata.TypeMetadata{
			"undeclared partition": {ModelType: reflect.TypeFor[partProject](), Partitions: []metadata.Partition{{Name: "channel", Field: "Region"}}},
			"unknown field":        {ModelType: reflect.TypeFor[partProject](), Partitions: []metadata.Partition{{Name: "region", Field: "Missing"}}},
			"primary key":          {ModelType: reflect.TypeFor[partProject](), PKField: "ID", Partitions: []metadata.Partition{{Name: "region", Field: "ID"}}},
			"inherited at a root":  {ModelType: reflect.TypeFor[partProject](), Partitions: []metadata.Partition{{Name: "region"}}},
			"inherited without a foreign key": {
				ModelType:  reflect.TypeFor[partTask](),
				ParentMeta: f.projectMeta,
				Partitions: []metadata.Partition{{Name: "region"}},
			},
		}
		for name, meta := range cases {
			if c := w.partitionValueClause(ctx, meta, "region", columnRef{}, []string{"emea"}); c.query != noRows.query {
				t.Errorf("%s: expected no rows, got %q", name, c.query)
			}
		}
	})

	t.Run("values are converted to the field's kind", func(t *testing.T) {
		got := partitionFilterValues(ctx, reflect.TypeFor[partProject](), "Level", []string{"2"})
		if len(got) != 1 || got[0] != int64(2) {
			t.Errorf("got %#v, want int64(2)", got)
		}
		got = partitionFilterValues(ctx, reflect.TypeFor[partProject](), "Missing", []string{"x"})
		if len(got) != 1 || got[0] != "x" {
			t.Errorf("got %#v, want the raw value", got)
		}
	})
}

func TestPartitionScopeFor(t *testing.T) {
	route := metadata.PartitionScope{"region": {Values: []string{"emea"}}}
	includes := map[string]metadata.PartitionScope{"Tasks": {"region": {Unrestricted: true}}}

	if _, enforced := partitionScopeFor(context.Background(), ""); enforced {
		t.Error("no request context: expected narrowing not enforced")
	}
	if _, enforced := partitionScopeFor(context.Background(), "Tasks"); enforced {
		t.Error("no request context: expected relation narrowing not enforced")
	}

	authorized := context.WithValue(context.Background(), metadata.IncludePartitionScopesKey, includes)
	if scope, enforced := partitionScopeFor(authorized, ""); !enforced || scope != nil {
		t.Errorf("authorized request without route scope: got %v, %v", scope, enforced)
	}
	if scope, enforced := partitionScopeFor(authorized, "Tasks"); !enforced || !scope["region"].Unrestricted {
		t.Errorf("authorized relation: got %v, %v", scope, enforced)
	}
	if scope, enforced := partitionScopeFor(authorized, "Other"); !enforced || scope != nil {
		t.Errorf("unauthorized relation: got %v, %v", scope, enforced)
	}

	withRoute := context.WithValue(authorized, metadata.PartitionScopeKey, route)
	if scope, enforced := partitionScopeFor(withRoute, ""); !enforced || scope["region"].Values[0] != "emea" {
		t.Errorf("route scope: got %v, %v", scope, enforced)
	}
}

func TestPartitionRestrict(t *testing.T) {
	f := setupPartitionFixture(t)
	w := &Wrapper[partProject]{Store: f.db}
	authorized := context.WithValue(context.Background(), metadata.IncludePartitionScopesKey, map[string]metadata.PartitionScope{
		"Tasks": {"region": {Values: []string{"apac"}}},
	})
	emea := context.WithValue(authorized, metadata.PartitionScopeKey, metadata.PartitionScope{"region": {Values: []string{"emea"}}})

	count := func(ctx context.Context, model any, meta *metadata.TypeMetadata, path string) int {
		t.Helper()
		q := accessQuery(f.db.GetDB().NewSelect().Model(model), w.partitionRestrict(ctx, meta, columnRef{}, path), nil)
		n, err := q.Count(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return n
	}

	if got := count(context.Background(), (*partProject)(nil), f.projectMeta, ""); got != 2 {
		t.Errorf("not enforced: got %d, want 2", got)
	}
	if got := count(emea, (*partProject)(nil), &metadata.TypeMetadata{ModelType: f.projectMeta.ModelType}, ""); got != 2 {
		t.Errorf("unpartitioned type: got %d, want 2", got)
	}
	if got := count(emea, (*partProject)(nil), f.projectMeta, ""); got != 1 {
		t.Errorf("route access: got %d, want 1", got)
	}
	if got := count(emea, (*partTask)(nil), f.taskMeta, "Tasks"); got != 1 {
		t.Errorf("relation access: got %d, want 1", got)
	}
	if got := count(emea, (*partTask)(nil), f.taskMeta, "Unauthorized"); got != 0 {
		t.Errorf("relation without access: got %d, want 0", got)
	}
}

func TestPartitionClauses_MissingOrUnrestricted(t *testing.T) {
	f := setupPartitionFixture(t)
	w := &Wrapper[partProject]{Store: f.db}
	ctx := context.Background()

	if got := w.partitionClauses(ctx, f.projectMeta, columnRef{}, metadata.PartitionScope{}); len(got) != 1 || got[0].query != noRows.query {
		t.Errorf("partition missing from scope: expected a no-rows clause, got %+v", got)
	}
	if got := w.partitionClauses(ctx, f.projectMeta, columnRef{}, metadata.PartitionScope{"region": {Unrestricted: true}}); len(got) != 0 {
		t.Errorf("unrestricted: expected no clauses, got %+v", got)
	}
}

func TestEnforcePartitionWrite(t *testing.T) {
	f := setupPartitionFixture(t)
	projects := &Wrapper[partProject]{Store: f.db}
	tasks := &Wrapper[partTask]{Store: f.db}

	withScope := func(scope metadata.PartitionScope) context.Context {
		ctx := context.WithValue(context.Background(), metadata.IncludePartitionScopesKey, map[string]metadata.PartitionScope{})
		return context.WithValue(ctx, metadata.PartitionScopeKey, scope)
	}
	emea := withScope(metadata.PartitionScope{"region": {Values: []string{"emea"}}})
	unrestricted := withScope(metadata.PartitionScope{"region": {Unrestricted: true}})

	t.Run("not enforced without a request context", func(t *testing.T) {
		if err := projects.enforcePartitionWrite(context.Background(), f.projectMeta, nil, &partProject{}); err != nil {
			t.Errorf("got %v", err)
		}
	})
	t.Run("unpartitioned type", func(t *testing.T) {
		if err := projects.enforcePartitionWrite(emea, &metadata.TypeMetadata{}, nil, &partProject{}); err != nil {
			t.Errorf("got %v", err)
		}
	})
	t.Run("value within access", func(t *testing.T) {
		if err := projects.enforcePartitionWrite(emea, f.projectMeta, nil, &partProject{Region: "emea"}); err != nil {
			t.Errorf("got %v", err)
		}
	})
	t.Run("value outside access", func(t *testing.T) {
		if err := projects.enforcePartitionWrite(emea, f.projectMeta, nil, &partProject{Region: "apac"}); !errors.Is(err, apperrors.ErrForbidden) {
			t.Errorf("got %v, want ErrForbidden", err)
		}
	})
	t.Run("missing value names the JSON field", func(t *testing.T) {
		err := projects.enforcePartitionWrite(unrestricted, f.projectMeta, nil, &partProject{})
		var validationErr *apperrors.ValidationError
		if !errors.As(err, &validationErr) || validationErr.Message != "region is required" {
			t.Errorf("got %v", err)
		}
	})
	t.Run("partition on the primary key", func(t *testing.T) {
		pkMeta := &metadata.TypeMetadata{
			ModelType:  reflect.TypeFor[partProject](),
			PKField:    "ID",
			Partitions: []metadata.Partition{{Name: "project", Field: "ID"}},
		}
		if err := projects.enforcePartitionWrite(withScope(metadata.PartitionScope{"project": {Unrestricted: true}}), pkMeta, nil, &partProject{}); err != nil {
			t.Errorf("unrestricted: got %v", err)
		}
		if err := projects.enforcePartitionWrite(withScope(metadata.PartitionScope{"project": {Values: []string{"1"}}}), pkMeta, &partProject{ID: 1}, &partProject{ID: 1}); !errors.Is(err, apperrors.ErrForbidden) {
			t.Errorf("restricted, even within values: got %v, want ErrForbidden", err)
		}
	})
	t.Run("partition missing from scope", func(t *testing.T) {
		if err := projects.enforcePartitionWrite(withScope(metadata.PartitionScope{}), f.projectMeta, nil, &partProject{Region: "emea"}); !errors.Is(err, apperrors.ErrForbidden) {
			t.Errorf("got %v, want ErrForbidden", err)
		}
	})
	t.Run("row stored outside access can be edited but not moved", func(t *testing.T) {
		existing := &partProject{ID: 2, Region: "apac"}
		if err := projects.enforcePartitionWrite(emea, f.projectMeta, existing, &partProject{ID: 2, Region: "apac", Level: 3}); err != nil {
			t.Errorf("unchanged value: got %v", err)
		}
		if err := projects.enforcePartitionWrite(emea, f.projectMeta, existing, &partProject{ID: 2, Region: "amer"}); !errors.Is(err, apperrors.ErrForbidden) {
			t.Errorf("moved to a value outside access: got %v, want ErrForbidden", err)
		}
		if err := projects.enforcePartitionWrite(emea, f.projectMeta, existing, &partProject{ID: 2, Region: "emea"}); !errors.Is(err, apperrors.ErrForbidden) {
			t.Errorf("moved into the caller's access: got %v, want ErrForbidden", err)
		}
	})
	t.Run("restricted caller cannot move an inherited row to another parent", func(t *testing.T) {
		if err := tasks.enforcePartitionWrite(emea, f.taskMeta, &partTask{ProjectID: 1}, &partTask{ProjectID: 2}); !errors.Is(err, apperrors.ErrForbidden) {
			t.Errorf("got %v, want ErrForbidden", err)
		}
		if err := tasks.enforcePartitionWrite(emea, f.taskMeta, &partTask{ProjectID: 1}, &partTask{ProjectID: 1, Title: "renamed"}); err != nil {
			t.Errorf("same parent: got %v", err)
		}
	})
	t.Run("unrestricted caller can move an inherited row", func(t *testing.T) {
		if err := tasks.enforcePartitionWrite(unrestricted, f.taskMeta, &partTask{ProjectID: 1}, &partTask{ProjectID: 2}); err != nil {
			t.Errorf("got %v", err)
		}
	})
}

func TestScopedBypass(t *testing.T) {
	f := setupPartitionFixture(t)
	authInfo := &metadata.AuthInfo{UserID: "u", Grants: []metadata.ScopedGrant{
		{Scope: "support", Partition: "region", Values: []string{"emea"}},
		{Scope: "support", Partition: "region", Values: []string{"emea", "amer"}},
		{Scope: "support", Partition: "channel", Values: []string{"online"}},
		{Scope: "support", Partition: "", Values: []string{"apac"}},
		{Scope: "reader", Partition: "region", Values: []string{"apac"}},
	}}
	ctx := context.WithValue(context.Background(), metadata.AuthInfoKey, authInfo)
	ctx = context.WithValue(ctx, metadata.IncludePartitionScopesKey, map[string]metadata.PartitionScope{})

	values := scopedBypassValues(ctx, f.projectMeta, []string{"support"})
	if len(values) != 1 || len(values["region"]) != 2 {
		t.Errorf("expected emea and amer for region only, got %v", values)
	}

	if !hasScopedBypass(ctx, f.projectMeta, []string{"support"}, &partProject{Region: "emea"}) {
		t.Error("EMEA row: expected bypass")
	}
	if hasScopedBypass(ctx, f.projectMeta, []string{"support"}, &partProject{Region: "apac"}) {
		t.Error("APAC row: expected no bypass")
	}
	if hasScopedBypass(ctx, f.taskMeta, []string{"support"}, &partTask{ProjectID: 1}) {
		t.Error("inherited partition cannot be checked in memory: expected no bypass")
	}
	if hasScopedBypass(context.WithValue(context.Background(), metadata.AuthInfoKey, authInfo), f.projectMeta, []string{"support"}, &partProject{Region: "emea"}) {
		t.Error("no request context: expected no bypass")
	}

	w := &Wrapper[partProject]{Store: f.db}
	clauses := w.scopedBypassClauses(ctx, f.projectMeta, columnRef{}, []string{"support"})
	if len(clauses) != 1 || countWhere(t, f.db, (*partProject)(nil), clauses[0]) != 1 {
		t.Errorf("expected one clause matching the EMEA project, got %+v", clauses)
	}
	if got := w.scopedBypassClauses(ctx, f.projectMeta, columnRef{}, nil); got != nil {
		t.Errorf("no bypass scopes: expected nil, got %+v", got)
	}
}

func TestPartitionFieldLabel(t *testing.T) {
	meta := &metadata.TypeMetadata{ModelType: reflect.TypeFor[partProject]()}
	if got := partitionFieldLabel(meta, "Region"); got != "region" {
		t.Errorf("json tag: got %q", got)
	}
	if got := partitionFieldLabel(meta, "Untagged"); got != "Untagged" {
		t.Errorf("json:\"-\": got %q", got)
	}
	if got := partitionFieldLabel(meta, "Level"); got != "Level" {
		t.Errorf("no json tag: got %q", got)
	}
	if got := partitionFieldLabel(meta, "Missing"); got != "Missing" {
		t.Errorf("missing field: got %q", got)
	}
}
