package datastore

import (
	"context"
	"reflect"
	"testing"

	"github.com/sjgoldie/go-restgen/metadata"
)

func projectShare(levels ...string) *metadata.Share {
	return &metadata.Share{
		ModelType:   reflect.TypeFor[partProjectShare](),
		TargetType:  reflect.TypeFor[partProject](),
		TargetField: "ProjectID",
		UserField:   "UserID",
		LevelField:  "Level",
		Levels:      levels,
	}
}

func asUser(userID string) context.Context {
	return context.WithValue(context.Background(), metadata.AuthInfoKey, &metadata.AuthInfo{UserID: userID})
}

func TestShareClause(t *testing.T) {
	f := setupPartitionFixture(t)
	w := &Wrapper[partProject]{Store: f.db}

	tests := []struct {
		name  string
		ctx   context.Context
		model any
		meta  *metadata.TypeMetadata
		share *metadata.Share
		want  int
	}{
		{"shared row", asUser("bob"), (*partProject)(nil), f.projectMeta, projectShare(), 1},
		{"child of a shared row", asUser("bob"), (*partTask)(nil), f.taskMeta, projectShare(), 1},
		{"grandchild of a shared row", asUser("bob"), (*partComment)(nil), f.commentMeta, projectShare(), 1},
		{"child with the foreign key on the shared parent", asUser("bob"), (*partManager)(nil), f.managerMeta, projectShare(), 1},
		{"accepted level", asUser("bob"), (*partProject)(nil), f.projectMeta, projectShare("viewer", "editor"), 1},
		{"level not accepted", asUser("bob"), (*partProject)(nil), f.projectMeta, projectShare("editor"), 0},
		{"another user", asUser("carol"), (*partProject)(nil), f.projectMeta, projectShare(), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := w.shareClause(tt.ctx, tt.meta, columnRef{}, tt.share)
			if c == nil {
				t.Fatal("expected a share clause")
			}
			if got := countWhere(t, f.db, tt.model, *c); got != tt.want {
				t.Errorf("got %d rows, want %d", got, tt.want)
			}
		})
	}

	t.Run("no clause", func(t *testing.T) {
		if c := w.shareClause(asUser("bob"), f.projectMeta, columnRef{}, nil); c != nil {
			t.Errorf("no share setting: got %+v", c)
		}
		if c := w.shareClause(context.Background(), f.projectMeta, columnRef{}, projectShare()); c != nil {
			t.Errorf("no caller: got %+v", c)
		}
		if c := w.shareClause(asUser(""), f.projectMeta, columnRef{}, projectShare()); c != nil {
			t.Errorf("no user ID: got %+v", c)
		}
		unrelated := &metadata.TypeMetadata{ModelType: reflect.TypeFor[partManager](), TableName: "part_managers"}
		if c := w.shareClause(asUser("bob"), unrelated, columnRef{}, projectShare()); c != nil {
			t.Errorf("target not in the chain: got %+v", c)
		}
		noFK := &metadata.TypeMetadata{ModelType: reflect.TypeFor[partTask](), ParentMeta: f.projectMeta}
		if c := w.shareClause(asUser("bob"), noFK, columnRef{}, projectShare()); c != nil {
			t.Errorf("parent without a foreign key: got %+v", c)
		}
	})

	t.Run("invalid share fields match no rows", func(t *testing.T) {
		bad := projectShare()
		bad.UserField = "Missing"
		if c := w.shareClause(asUser("bob"), f.projectMeta, columnRef{}, bad); c == nil || c.query != noRows.query {
			t.Errorf("got %+v", c)
		}
		bad = projectShare("viewer")
		bad.LevelField = "Missing"
		if c := w.shareClause(asUser("bob"), f.projectMeta, columnRef{}, bad); c == nil || c.query != noRows.query {
			t.Errorf("got %+v", c)
		}
		bad = projectShare()
		bad.TargetField = "Missing"
		if c := w.shareClause(asUser("bob"), f.projectMeta, columnRef{}, bad); c == nil || c.query != noRows.query {
			t.Errorf("got %+v", c)
		}
	})
}

func TestAccessQuery(t *testing.T) {
	f := setupPartitionFixture(t)
	w := &Wrapper[partProject]{Store: f.db}
	emea := w.partitionValueClause(context.Background(), f.projectMeta, "region", columnRef{}, []string{"emea"})
	shared := w.shareClause(asUser("bob"), f.projectMeta, columnRef{}, projectShare())

	count := func(restrict []clause, share *clause) int {
		t.Helper()
		n, err := accessQuery(f.db.GetDB().NewSelect().Model((*partProject)(nil)), restrict, share).Count(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return n
	}

	if got := count(nil, shared); got != 2 {
		t.Errorf("no restrictions: got %d, want 2", got)
	}
	if got := count([]clause{emea}, nil); got != 1 {
		t.Errorf("restricted: got %d, want 1", got)
	}
	if got := count([]clause{emea}, shared); got != 2 {
		t.Errorf("restricted or shared: got %d, want 2", got)
	}
	if got := count([]clause{emea, noRows}, shared); got != 1 {
		t.Errorf("all restrictions must pass unless shared: got %d, want 1", got)
	}

	if got := accessJoinConditions(nil, shared); got != nil {
		t.Errorf("join conditions without restrictions: got %+v", got)
	}
	if got := accessJoinConditions([]clause{emea, noRows}, nil); len(got) != 2 {
		t.Errorf("join conditions without a share: got %d, want 2", len(got))
	}
	if got := accessJoinConditions([]clause{emea, noRows}, shared); len(got) != 1 {
		t.Errorf("join conditions with a share: got %d, want 1 combined", len(got))
	}
}

func TestShareFor(t *testing.T) {
	route := projectShare()
	task := projectShare("editor")
	ctx := context.WithValue(context.Background(), metadata.ShareKey, route)
	ctx = context.WithValue(ctx, metadata.IncludeSharesKey, map[string]*metadata.Share{"Tasks": task})

	if got := shareFor(ctx, ""); got != route {
		t.Errorf("route: got %+v", got)
	}
	if got := shareFor(ctx, "Tasks"); got != task {
		t.Errorf("relation: got %+v", got)
	}
	if got := shareFor(ctx, "Other"); got != nil {
		t.Errorf("relation without a share: got %+v", got)
	}
	if got := shareFor(context.Background(), ""); got != nil {
		t.Errorf("no context: got %+v", got)
	}
}
