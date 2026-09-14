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

type partAssessment struct {
	bun.BaseModel `bun:"table:part_assessments"`
	ID            int          `bun:"id,pk,autoincrement"`
	ProjectID     int          `bun:"project_id" json:"project_id"`
	Project       *partProject `bun:"rel:belongs-to,join:project_id=id"`
	Score         int          `bun:"score"`
}

type partReview struct {
	bun.BaseModel `bun:"table:part_reviews"`
	ID            int             `bun:"id,pk,autoincrement"`
	AssessmentID  int             `bun:"assessment_id"`
	Assessment    *partAssessment `bun:"rel:belongs-to,join:assessment_id=id"`
}

type relationPathFixture struct {
	partitionFixture
	assessmentMeta *metadata.TypeMetadata
	reviewMeta     *metadata.TypeMetadata
}

// setupRelationPathFixture adds an assessment and a review under each project of the partition
// fixture (EMEA project 1, APAC project 2) and a second EMEA project (ID 3).
func setupRelationPathFixture(t *testing.T) relationPathFixture {
	t.Helper()
	f := setupPartitionFixture(t)
	ctx := context.Background()

	for _, model := range []any{(*partAssessment)(nil), (*partReview)(nil)} {
		if _, err := f.db.GetDB().NewCreateTable().Model(model).Exec(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.db.GetDB().NewInsert().Model(&partProject{Region: "emea"}).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	assessments := []partAssessment{{ProjectID: 1}, {ProjectID: 2}}
	if _, err := f.db.GetDB().NewInsert().Model(&assessments).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	reviews := []partReview{{AssessmentID: 1}, {AssessmentID: 2}}
	if _, err := f.db.GetDB().NewInsert().Model(&reviews).Exec(ctx); err != nil {
		t.Fatal(err)
	}

	projectVia, _, err := ResolveRelationPath(reflect.TypeFor[partAssessment](), []string{"Project"})
	if err != nil {
		t.Fatal(err)
	}
	reviewVia, _, err := ResolveRelationPath(reflect.TypeFor[partReview](), []string{"Assessment", "Project"})
	if err != nil {
		t.Fatal(err)
	}

	return relationPathFixture{
		partitionFixture: f,
		assessmentMeta: &metadata.TypeMetadata{
			TypeName:   "partAssessment",
			TableName:  "part_assessments",
			ModelType:  reflect.TypeFor[partAssessment](),
			PKField:    "ID",
			Partitions: []metadata.Partition{{Name: "region", Field: "Region", Via: projectVia}},
		},
		reviewMeta: &metadata.TypeMetadata{
			TypeName:   "partReview",
			TableName:  "part_reviews",
			ModelType:  reflect.TypeFor[partReview](),
			PKField:    "ID",
			Partitions: []metadata.Partition{{Name: "region", Field: "Region", Via: reviewVia}},
		},
	}
}

func TestResolveRelationPath(t *testing.T) {
	setupRelationPathFixture(t)

	steps, related, err := ResolveRelationPath(reflect.TypeFor[partReview](), []string{"Assessment", "Project"})
	if err != nil {
		t.Fatal(err)
	}
	if related != reflect.TypeFor[partProject]() || len(steps) != 2 {
		t.Fatalf("got %v, %+v", related, steps)
	}
	if steps[0].Table != "part_assessments" || steps[0].FKColumn != "assessment_id" || steps[1].Table != "part_projects" || steps[1].FKColumn != "project_id" {
		t.Errorf("unexpected steps: %+v", steps)
	}

	if _, _, err := ResolveRelationPath(reflect.TypeFor[partReview](), []string{"Missing"}); err == nil {
		t.Error("unknown relation: expected an error")
	}
	if got := PrimaryKeyField(reflect.TypeFor[partProject]()); got != "ID" {
		t.Errorf("primary key field: got %q", got)
	}
}

func TestPartitionValueClause_RelationPath(t *testing.T) {
	f := setupRelationPathFixture(t)
	w := &Wrapper[partAssessment]{Store: f.db}
	ctx := context.Background()

	if got := countWhere(t, f.db, (*partAssessment)(nil), w.partitionValueClause(ctx, f.assessmentMeta, "region", columnRef{}, []string{"emea"})); got != 1 {
		t.Errorf("one relation: got %d rows, want 1", got)
	}
	if got := countWhere(t, f.db, (*partReview)(nil), w.partitionValueClause(ctx, f.reviewMeta, "region", columnRef{}, []string{"apac"})); got != 1 {
		t.Errorf("two relations: got %d rows, want 1", got)
	}
	if got := countWhere(t, f.db, (*partReview)(nil), w.partitionValueClause(ctx, f.reviewMeta, "region", columnRef{}, []string{"amer"})); got != 0 {
		t.Errorf("no matching related row: got %d rows, want 0", got)
	}

	unknownField := &metadata.TypeMetadata{
		ModelType:  f.assessmentMeta.ModelType,
		Partitions: []metadata.Partition{{Name: "region", Field: "Missing", Via: f.assessmentMeta.Partitions[0].Via}},
	}
	if got := countWhere(t, f.db, (*partAssessment)(nil), w.partitionValueClause(ctx, unknownField, "region", columnRef{}, []string{"emea"})); got != 0 {
		t.Errorf("unknown field on the related model: got %d rows, want 0", got)
	}
}

func TestEnforcePartitionWrite_RelationPath(t *testing.T) {
	f := setupRelationPathFixture(t)
	assessments := &Wrapper[partAssessment]{Store: f.db}
	reviews := &Wrapper[partReview]{Store: f.db}

	withScope := func(scope metadata.PartitionScope) context.Context {
		ctx := context.WithValue(context.Background(), metadata.IncludePartitionScopesKey, map[string]metadata.PartitionScope{})
		return context.WithValue(ctx, metadata.PartitionScopeKey, scope)
	}
	emea := withScope(metadata.PartitionScope{"region": {Values: []string{"emea"}}})
	unrestricted := withScope(metadata.PartitionScope{"region": {Unrestricted: true}})

	tests := []struct {
		name     string
		ctx      context.Context
		existing *partAssessment
		item     *partAssessment
		want     error
	}{
		{"create under a related row within access", emea, nil, &partAssessment{ProjectID: 1}, nil},
		{"create under a related row outside access", emea, nil, &partAssessment{ProjectID: 2}, apperrors.ErrForbidden},
		{"create under a missing related row", emea, nil, &partAssessment{ProjectID: 99}, apperrors.ErrForbidden},
		{"unrestricted create under any related row", unrestricted, nil, &partAssessment{ProjectID: 2}, nil},
		{"update keeping a related row outside access", emea, &partAssessment{ID: 2, ProjectID: 2}, &partAssessment{ID: 2, ProjectID: 2, Score: 5}, nil},
		{"update moving out of a related row outside access", emea, &partAssessment{ID: 2, ProjectID: 2}, &partAssessment{ID: 2, ProjectID: 1}, apperrors.ErrForbidden},
		{"update moving to a related row outside access", emea, &partAssessment{ID: 1, ProjectID: 1}, &partAssessment{ID: 1, ProjectID: 2}, apperrors.ErrForbidden},
		{"update moving between related rows within access", emea, &partAssessment{ID: 1, ProjectID: 1}, &partAssessment{ID: 1, ProjectID: 3}, nil},
		{"unrestricted update moving anywhere", unrestricted, &partAssessment{ID: 1, ProjectID: 1}, &partAssessment{ID: 1, ProjectID: 2}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := assessments.enforcePartitionWrite(tt.ctx, f.assessmentMeta, tt.existing, tt.item)
			if !errors.Is(err, tt.want) && err != tt.want {
				t.Errorf("got %v, want %v", err, tt.want)
			}
		})
	}

	t.Run("missing reference names the JSON field", func(t *testing.T) {
		err := assessments.enforcePartitionWrite(unrestricted, f.assessmentMeta, nil, &partAssessment{})
		var validationErr *apperrors.ValidationError
		if !errors.As(err, &validationErr) || validationErr.Message != "project_id is required" {
			t.Errorf("got %v", err)
		}
	})

	t.Run("two relations", func(t *testing.T) {
		if err := reviews.enforcePartitionWrite(emea, f.reviewMeta, nil, &partReview{AssessmentID: 1}); err != nil {
			t.Errorf("within access: got %v", err)
		}
		if err := reviews.enforcePartitionWrite(emea, f.reviewMeta, nil, &partReview{AssessmentID: 2}); !errors.Is(err, apperrors.ErrForbidden) {
			t.Errorf("outside access: got %v, want ErrForbidden", err)
		}
	})

	t.Run("create under a related row shared with the caller", func(t *testing.T) {
		share := projectShare()
		share.BaseType = reflect.TypeFor[partAssessment]()
		share.Via = f.assessmentMeta.Partitions[0].Via

		asBob := func(ctx context.Context, s *metadata.Share) context.Context {
			ctx = context.WithValue(ctx, metadata.AuthInfoKey, &metadata.AuthInfo{UserID: "bob"})
			return context.WithValue(ctx, metadata.ShareKey, s)
		}
		if err := assessments.enforcePartitionWrite(asBob(emea, share), f.assessmentMeta, nil, &partAssessment{ProjectID: 2}); err != nil {
			t.Errorf("shared project: got %v", err)
		}
		editorOnly := projectShare("editor")
		editorOnly.BaseType = share.BaseType
		editorOnly.Via = share.Via
		if err := assessments.enforcePartitionWrite(asBob(emea, editorOnly), f.assessmentMeta, nil, &partAssessment{ProjectID: 2}); !errors.Is(err, apperrors.ErrForbidden) {
			t.Errorf("share level not accepted: got %v, want ErrForbidden", err)
		}
		if err := assessments.enforcePartitionWrite(asBob(emea, share), f.assessmentMeta, &partAssessment{ID: 1, ProjectID: 1}, &partAssessment{ID: 1, ProjectID: 2}); !errors.Is(err, apperrors.ErrForbidden) {
			t.Errorf("moving into a shared project: got %v, want ErrForbidden", err)
		}

		reviewShare := projectShare()
		reviewShare.BaseType = reflect.TypeFor[partReview]()
		reviewShare.Via = f.reviewMeta.Partitions[0].Via
		if err := reviews.enforcePartitionWrite(asBob(emea, reviewShare), f.reviewMeta, nil, &partReview{AssessmentID: 2}); err != nil {
			t.Errorf("two relations to a shared project: got %v", err)
		}
	})

	t.Run("no values grant nothing", func(t *testing.T) {
		none := withScope(metadata.PartitionScope{"region": {}})
		if err := assessments.enforcePartitionWrite(none, f.assessmentMeta, nil, &partAssessment{ProjectID: 1}); !errors.Is(err, apperrors.ErrForbidden) {
			t.Errorf("got %v, want ErrForbidden", err)
		}
	})
}

func TestShareClause_RelationPath(t *testing.T) {
	f := setupRelationPathFixture(t)
	w := &Wrapper[partAssessment]{Store: f.db}

	assessmentShare := projectShare()
	assessmentShare.BaseType = reflect.TypeFor[partAssessment]()
	assessmentShare.Via = f.assessmentMeta.Partitions[0].Via

	c := w.shareClause(asUser("bob"), f.assessmentMeta, columnRef{}, assessmentShare)
	if c == nil {
		t.Fatal("expected a share clause")
	}
	if got := countWhere(t, f.db, (*partAssessment)(nil), *c); got != 1 {
		t.Errorf("assessment of a shared project: got %d rows, want 1", got)
	}

	reviewShare := projectShare("viewer")
	reviewShare.BaseType = reflect.TypeFor[partReview]()
	reviewShare.Via = f.reviewMeta.Partitions[0].Via
	c = w.shareClause(asUser("bob"), f.reviewMeta, columnRef{}, reviewShare)
	if c == nil {
		t.Fatal("expected a share clause")
	}
	if got := countWhere(t, f.db, (*partReview)(nil), *c); got != 1 {
		t.Errorf("review two relations from a shared project: got %d rows, want 1", got)
	}

	if c := w.shareClause(asUser("bob"), f.projectMeta, columnRef{}, assessmentShare); c != nil {
		t.Errorf("a Via share only applies to its base model: got %+v", c)
	}

	editorOnly := projectShare("editor")
	editorOnly.BaseType = assessmentShare.BaseType
	editorOnly.Via = assessmentShare.Via
	c = w.shareClause(asUser("bob"), f.assessmentMeta, columnRef{}, editorOnly)
	if c == nil || countWhere(t, f.db, (*partAssessment)(nil), *c) != 0 {
		t.Errorf("level not accepted: expected no rows, got %+v", c)
	}

	invalid := projectShare()
	invalid.BaseType = assessmentShare.BaseType
	invalid.Via = assessmentShare.Via
	invalid.UserField = "Missing"
	if c := w.shareClause(asUser("bob"), f.assessmentMeta, columnRef{}, invalid); c == nil || c.query != noRows.query {
		t.Errorf("invalid share fields: got %+v", c)
	}
}

func TestHasScopedBypass_RelationPathIsNotCheckedInMemory(t *testing.T) {
	f := setupRelationPathFixture(t)
	authInfo := &metadata.AuthInfo{UserID: "u", Grants: []metadata.ScopedGrant{{Scope: "support", Partition: "region", Values: []string{"emea"}}}}
	ctx := context.WithValue(context.Background(), metadata.AuthInfoKey, authInfo)
	ctx = context.WithValue(ctx, metadata.IncludePartitionScopesKey, map[string]metadata.PartitionScope{})

	if hasScopedBypass(ctx, f.assessmentMeta, []string{"support"}, &partAssessment{ProjectID: 1}) {
		t.Error("expected no bypass for a partition held on a related row")
	}
}
