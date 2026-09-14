package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/router"
)

type PathProject struct {
	bun.BaseModel `bun:"table:path_projects"`
	ID            int    `bun:"id,pk,autoincrement" json:"id"`
	OrgID         string `bun:"org_id" json:"org_id"`
	Region        string `bun:"region" json:"region"`
	Name          string `bun:"name" json:"name"`
}

type PathAssessment struct {
	bun.BaseModel `bun:"table:path_assessments"`
	ID            int          `bun:"id,pk,autoincrement" json:"id"`
	OrgID         string       `bun:"org_id" json:"org_id"`
	ProjectID     int          `bun:"project_id" json:"project_id"`
	Project       *PathProject `bun:"rel:belongs-to,join:project_id=id" json:"project,omitempty"`
	Title         string       `bun:"title" json:"title"`
	Notes         []*PathNote  `bun:"rel:has-many,join:id=assessment_id" json:"notes,omitempty"`
}

type PathFinding struct {
	bun.BaseModel `bun:"table:path_findings"`
	ID            int             `bun:"id,pk,autoincrement" json:"id"`
	AssessmentID  int             `bun:"assessment_id" json:"assessment_id"`
	Assessment    *PathAssessment `bun:"rel:belongs-to,join:assessment_id=id" json:"assessment,omitempty"`
	Text          string          `bun:"text" json:"text"`
}

type PathNote struct {
	bun.BaseModel `bun:"table:path_notes"`
	ID            int             `bun:"id,pk,autoincrement" json:"id"`
	AssessmentID  int             `bun:"assessment_id" json:"assessment_id"`
	Assessment    *PathAssessment `bun:"rel:belongs-to,join:assessment_id=id" json:"assessment,omitempty"`
	Text          string          `bun:"text" json:"text"`
}

type PathShare struct {
	bun.BaseModel `bun:"table:path_shares"`
	ID            int    `bun:"id,pk,autoincrement" json:"id"`
	ProjectID     int    `bun:"project_id" json:"project_id"`
	UserID        string `bun:"user_id" json:"user_id"`
	Level         string `bun:"level" json:"level"`
}

type pathFixture struct {
	emeaProject, apacProject, emeaProject2 int
	emeaAssessment, apacAssessment         int
}

// seedPaths creates EMEA projects Alpha and Gamma and APAC project Beta in org-a, an assessment
// under Alpha and under Beta, a finding and a note under each assessment, and shares of Beta:
// Bob as viewer and Dave as editor.
func seedPaths(t *testing.T) pathFixture {
	t.Helper()
	db := hardTables(t, (*PathProject)(nil), (*PathAssessment)(nil), (*PathFinding)(nil), (*PathNote)(nil), (*PathShare)(nil))
	ctx := context.Background()

	projects := []PathProject{
		{OrgID: "org-a", Region: "emea", Name: "Alpha"},
		{OrgID: "org-a", Region: "apac", Name: "Beta"},
		{OrgID: "org-a", Region: "emea", Name: "Gamma"},
	}
	if _, err := db.NewInsert().Model(&projects).Returning("*").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	assessments := []PathAssessment{
		{OrgID: "org-a", ProjectID: projects[0].ID, Title: "alpha-assessment"},
		{OrgID: "org-a", ProjectID: projects[1].ID, Title: "beta-assessment"},
	}
	if _, err := db.NewInsert().Model(&assessments).Returning("*").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	findings := []PathFinding{
		{AssessmentID: assessments[0].ID, Text: "alpha-finding"},
		{AssessmentID: assessments[1].ID, Text: "beta-finding"},
	}
	if _, err := db.NewInsert().Model(&findings).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	notes := []PathNote{
		{AssessmentID: assessments[0].ID, Text: "alpha-note"},
		{AssessmentID: assessments[1].ID, Text: "beta-note"},
	}
	if _, err := db.NewInsert().Model(&notes).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	shares := []PathShare{
		{ProjectID: projects[1].ID, UserID: "bob", Level: "viewer"},
		{ProjectID: projects[1].ID, UserID: "dave", Level: "editor"},
	}
	if _, err := db.NewInsert().Model(&shares).Exec(ctx); err != nil {
		t.Fatal(err)
	}

	return pathFixture{
		emeaProject: projects[0].ID, apacProject: projects[1].ID, emeaProject2: projects[2].ID,
		emeaAssessment: assessments[0].ID, apacAssessment: assessments[1].ID,
	}
}

func projectSharesVia(via string, levels ...string) *router.ShareConfig {
	return &router.ShareConfig{
		Model:       (*PathShare)(nil),
		Via:         via,
		TargetField: "ProjectID",
		UserField:   "UserID",
		LevelField:  "Level",
		Levels:      levels,
	}
}

// registerPaths registers top-level assessments and findings partitioned by the region of their
// project, with notes nested under assessments, all accepting shares of the project.
func registerPaths(b *router.Builder) {
	router.RegisterRoutes[PathAssessment](b, "/assessments",
		router.WithPartition("region", "Project.Region"),
		router.AuthConfig{Methods: []string{router.MethodGet, router.MethodList}, Scopes: []string{"assessment:read"}, Share: projectSharesVia("Project")},
		router.AuthConfig{Methods: []string{router.MethodPost, router.MethodPut, router.MethodPatch, router.MethodBatchCreate, router.MethodBatchUpdate}, Scopes: []string{"assessment:write"}, Share: projectSharesVia("Project", "editor")},
		router.AuthConfig{Methods: []string{router.MethodDelete}, Scopes: []string{"assessment:write"}},
		router.WithFilters("Title"),
		func(b *router.Builder) {
			router.RegisterRoutes[PathNote](b, "/notes",
				router.AllScoped("assessment:read"),
				router.WithRelationName("Notes"),
			)
		},
	)
	router.RegisterRoutes[PathFinding](b, "/findings",
		router.WithPartition("region", "Assessment.Project.Region"),
		router.AuthConfig{Methods: []string{router.MethodGet, router.MethodList}, Scopes: []string{"finding:read"}, Share: projectSharesVia("Assessment.Project")},
	)
}

func pathTitles(t *testing.T, r http.Handler, path string) map[string]bool {
	t.Helper()
	w := hardRequest(t, r, "GET", path, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s: expected 200, got %d: %s", path, w.Code, w.Body.String())
	}
	var list struct {
		Data []struct {
			Title string `json:"title"`
			Text  string `json:"text"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	titles := make(map[string]bool, len(list.Data))
	for _, item := range list.Data {
		titles[item.Title+item.Text] = true
	}
	return titles
}

func TestRelationPathPartition_ReadsFollowTheRelatedRow(t *testing.T) {
	f := seedPaths(t)

	global := scopedRouter(&router.AuthInfo{UserID: "u", Scopes: []string{"assessment:read", "finding:read"}}, registerPaths)
	if titles := pathTitles(t, global, "/assessments"); len(titles) != 2 {
		t.Errorf("global: expected every assessment, got %v", titles)
	}

	emea := scopedRouter(&router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{
		grant("assessment:read", "region", "emea"),
		grant("finding:read", "region", "emea"),
	}}, registerPaths)
	if titles := pathTitles(t, emea, "/assessments"); len(titles) != 1 || !titles["alpha-assessment"] {
		t.Errorf("EMEA: expected only the assessment of an EMEA project, got %v", titles)
	}
	if titles := pathTitles(t, emea, "/findings"); len(titles) != 1 || !titles["alpha-finding"] {
		t.Errorf("EMEA findings two relations away: expected only the EMEA finding, got %v", titles)
	}
	if w := hardRequest(t, emea, "GET", "/assessments/"+strconv.Itoa(f.apacAssessment), ""); w.Code != http.StatusNotFound {
		t.Errorf("APAC assessment: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	w := hardRequest(t, emea, "GET", "/assessments?count=true", "")
	var counted struct {
		Pagination struct {
			TotalCount int `json:"total_count"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &counted); err != nil {
		t.Fatal(err)
	}
	if counted.Pagination.TotalCount != 1 {
		t.Errorf("total_count: expected 1, got %d", counted.Pagination.TotalCount)
	}
}

func TestRelationPathPartition_NestedChildrenInherit(t *testing.T) {
	f := seedPaths(t)
	emea := scopedRouter(&router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{grant("assessment:read", "region", "emea")}}, registerPaths)

	if titles := pathTitles(t, emea, "/assessments/"+strconv.Itoa(f.emeaAssessment)+"/notes"); len(titles) != 1 {
		t.Errorf("notes under an EMEA assessment: expected 1, got %v", titles)
	}
	if titles := pathTitles(t, emea, "/assessments/"+strconv.Itoa(f.apacAssessment)+"/notes"); len(titles) != 0 {
		t.Errorf("notes under an APAC assessment: expected none, got %v", titles)
	}

	w := hardRequest(t, emea, "GET", "/assessments?include=Notes", "")
	var list struct {
		Data []PathAssessment `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 1 || len(list.Data[0].Notes) != 1 {
		t.Errorf("include notes: expected the EMEA assessment with its note, got %+v", list.Data)
	}
}

func TestRelationPathPartition_Writes(t *testing.T) {
	f := seedPaths(t)
	r := scopedRouter(&router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{
		grant("assessment:read", "region", "emea", "apac"),
		grant("assessment:write", "region", "emea"),
	}}, registerPaths)

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{"create under an EMEA project", "POST", "/assessments", `{"project_id":` + strconv.Itoa(f.emeaProject) + `,"title":"new"}`, http.StatusCreated},
		{"create under an APAC project", "POST", "/assessments", `{"project_id":` + strconv.Itoa(f.apacProject) + `,"title":"new"}`, http.StatusForbidden},
		{"create under a missing project", "POST", "/assessments", `{"project_id":99999,"title":"new"}`, http.StatusForbidden},
		{"create without a project", "POST", "/assessments", `{"title":"new"}`, http.StatusBadRequest},
		{"batch create with one outside access", "POST", "/assessments/batch", `[{"project_id":` + strconv.Itoa(f.emeaProject) + `},{"project_id":` + strconv.Itoa(f.apacProject) + `}]`, http.StatusForbidden},
		{"move to another EMEA project", "PATCH", "/assessments/" + strconv.Itoa(f.emeaAssessment), `{"project_id":` + strconv.Itoa(f.emeaProject2) + `}`, http.StatusOK},
		{"move to an APAC project", "PATCH", "/assessments/" + strconv.Itoa(f.emeaAssessment), `{"project_id":` + strconv.Itoa(f.apacProject) + `}`, http.StatusForbidden},
		{"batch move to an APAC project", "PUT", "/assessments/batch", `[{"id":` + strconv.Itoa(f.emeaAssessment) + `,"project_id":` + strconv.Itoa(f.apacProject) + `,"title":"moved"}]`, http.StatusForbidden},
		{"update an APAC assessment", "PATCH", "/assessments/" + strconv.Itoa(f.apacAssessment), `{"title":"x"}`, http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if w := hardRequest(t, r, tt.method, tt.path, tt.body); w.Code != tt.want {
				t.Errorf("expected %d, got %d: %s", tt.want, w.Code, w.Body.String())
			}
		})
	}

	if titles := pathTitles(t, r, "/assessments?filter[Title]=new"); len(titles) != 1 {
		t.Errorf("only the allowed create should exist (batch is all-or-nothing), got %v", titles)
	}
}

func TestRelationPathShare(t *testing.T) {
	f := seedPaths(t)
	apacAssessment := "/assessments/" + strconv.Itoa(f.apacAssessment)

	bob := scopedRouter(&router.AuthInfo{UserID: "bob", Grants: []router.ScopedGrant{
		grant("assessment:read", "region"),
		grant("assessment:write", "region"),
		grant("finding:read", "region"),
	}}, registerPaths)
	if titles := pathTitles(t, bob, "/assessments"); len(titles) != 1 || !titles["beta-assessment"] {
		t.Errorf("viewer: expected the assessment of the shared project, got %v", titles)
	}
	if titles := pathTitles(t, bob, "/findings"); len(titles) != 1 || !titles["beta-finding"] {
		t.Errorf("viewer: expected the finding two relations from the shared project, got %v", titles)
	}
	if w := hardRequest(t, bob, "PATCH", apacAssessment, `{"title":"bob"}`); w.Code != http.StatusNotFound {
		t.Errorf("viewer update: expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if w := hardRequest(t, bob, "POST", "/assessments", `{"project_id":`+strconv.Itoa(f.apacProject)+`}`); w.Code != http.StatusForbidden {
		t.Errorf("viewer create under the shared project: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	dave := scopedRouter(&router.AuthInfo{UserID: "dave", Grants: []router.ScopedGrant{
		grant("assessment:read", "region"),
		grant("assessment:write", "region", "emea"),
	}}, registerPaths)
	if w := hardRequest(t, dave, "PATCH", apacAssessment, `{"title":"dave"}`); w.Code != http.StatusOK {
		t.Errorf("editor update: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w := hardRequest(t, dave, "PATCH", apacAssessment, `{"project_id":`+strconv.Itoa(f.emeaProject)+`}`); w.Code != http.StatusForbidden {
		t.Errorf("editor moving a shared assessment: expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if w := hardRequest(t, dave, "POST", "/assessments", `{"project_id":`+strconv.Itoa(f.apacProject)+`,"title":"dave-new"}`); w.Code != http.StatusCreated {
		t.Errorf("editor create under the shared project: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if w := hardRequest(t, dave, "DELETE", apacAssessment, ""); w.Code != http.StatusNotFound {
		t.Errorf("delete does not accept shares: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRelationPath_TenantStillApplies(t *testing.T) {
	f := seedPaths(t)
	register := func(b *router.Builder) {
		router.RegisterRoutes[PathAssessment](b, "/assessments",
			router.WithTenantScope("OrgID"),
			router.WithPartition("region", "Project.Region"),
			router.AuthConfig{Methods: []string{router.MethodAll}, Scopes: []string{"assessment:read"}, Share: projectSharesVia("Project")},
		)
	}

	otherOrg := addTenantAuthMiddleware(chi.NewRouter(), "bob", "org-b", []string{"assessment:read"})
	register(router.NewBuilder(otherOrg))
	if w := hardRequest(t, otherOrg, "GET", "/assessments/"+strconv.Itoa(f.apacAssessment), ""); w.Code != http.StatusNotFound {
		t.Errorf("another tenant, even with a share: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRelationPath_InvalidDeclarationsGrantNothing(t *testing.T) {
	f := seedPaths(t)
	register := func(b *router.Builder) {
		router.RegisterRoutes[PathAssessment](b, "/assessments",
			router.WithPartition("region", "Owner.Region"),
			router.AuthConfig{Methods: []string{router.MethodAll}, Scopes: []string{"assessment:read"}, Share: projectSharesVia("Owner")},
		)
	}

	bob := scopedRouter(&router.AuthInfo{UserID: "bob", Grants: []router.ScopedGrant{grant("assessment:read", "region", "emea", "apac")}}, register)
	if titles := pathTitles(t, bob, "/assessments"); len(titles) != 0 {
		t.Errorf("invalid relation path: expected no rows for a scoped caller, got %v", titles)
	}
	global := scopedRouter(&router.AuthInfo{UserID: "u", Scopes: []string{"assessment:read"}}, register)
	if w := hardRequest(t, global, "GET", "/assessments/"+strconv.Itoa(f.emeaAssessment), ""); w.Code != http.StatusOK {
		t.Errorf("global scope: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}
