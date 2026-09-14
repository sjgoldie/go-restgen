package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/router"
	"github.com/sjgoldie/go-restgen/service"
)

type RegionProject struct {
	bun.BaseModel `bun:"table:region_projects"`
	ID            int           `bun:"id,pk,autoincrement" json:"id"`
	Region        string        `bun:"region" json:"region"`
	Name          string        `bun:"name" json:"name"`
	Budget        int           `bun:"budget" json:"budget"`
	Tasks         []*RegionTask `bun:"rel:has-many,join:id=project_id" json:"tasks,omitempty"`
}

type RegionTask struct {
	bun.BaseModel `bun:"table:region_tasks"`
	ID            int              `bun:"id,pk,autoincrement" json:"id"`
	ProjectID     int              `bun:"project_id" json:"project_id"`
	Project       *RegionProject   `bun:"rel:belongs-to,join:project_id=id" json:"project,omitempty"`
	Title         string           `bun:"title" json:"title"`
	Comments      []*RegionComment `bun:"rel:has-many,join:id=task_id" json:"comments,omitempty"`
}

type RegionComment struct {
	bun.BaseModel `bun:"table:region_comments"`
	ID            int         `bun:"id,pk,autoincrement" json:"id"`
	TaskID        int         `bun:"task_id" json:"task_id"`
	Task          *RegionTask `bun:"rel:belongs-to,join:task_id=id" json:"task,omitempty"`
	Text          string      `bun:"text" json:"text"`
}

type RegionMilestone struct {
	bun.BaseModel `bun:"table:region_milestones"`
	ID            int            `bun:"id,pk,autoincrement" json:"id"`
	ProjectID     int            `bun:"project_id" json:"project_id"`
	Project       *RegionProject `bun:"rel:belongs-to,join:project_id=id" json:"project,omitempty"`
	Region        string         `bun:"region" json:"region"`
	Title         string         `bun:"title" json:"title"`
}

type RegionOrder struct {
	bun.BaseModel `bun:"table:region_orders"`
	ID            int    `bun:"id,pk,autoincrement" json:"id"`
	OrgID         string `bun:"org_id" json:"org_id"`
	Region        string `bun:"region" json:"region"`
	Channel       string `bun:"channel" json:"channel"`
	CustomerID    string `bun:"customer_id" json:"customer_id"`
	Title         string `bun:"title" json:"title"`
}

type regionProjectList struct {
	Data       []RegionProject           `json:"data"`
	Counts     map[string]map[string]int `json:"counts"`
	Sums       map[string]float64        `json:"sums"`
	Pagination *struct {
		TotalCount *int `json:"total_count"`
	} `json:"pagination"`
}

type regionFixture struct {
	emeaA, emeaB, apacA int
	emeaTask, apacTask  int
}

// seedRegions creates two EMEA projects, one APAC project, and one AMER project, with one
// task and one comment under the first EMEA project and under the APAC project, and two
// milestones under the first EMEA project (one tagged EMEA, one tagged APAC).
func seedRegions(t *testing.T) regionFixture {
	t.Helper()
	db := hardTables(t, (*RegionProject)(nil), (*RegionTask)(nil), (*RegionComment)(nil), (*RegionMilestone)(nil))
	ctx := context.Background()

	projects := []RegionProject{
		{Region: "emea", Name: "Apollo", Budget: 10},
		{Region: "emea", Name: "Atlas", Budget: 20},
		{Region: "apac", Name: "Borealis", Budget: 30},
		{Region: "amer", Name: "Cygnus", Budget: 40},
	}
	if _, err := db.NewInsert().Model(&projects).Returning("*").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	tasks := []RegionTask{
		{ProjectID: projects[0].ID, Title: "emea-task"},
		{ProjectID: projects[2].ID, Title: "apac-task"},
	}
	if _, err := db.NewInsert().Model(&tasks).Returning("*").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	comments := []RegionComment{
		{TaskID: tasks[0].ID, Text: "emea-comment"},
		{TaskID: tasks[1].ID, Text: "apac-comment"},
	}
	if _, err := db.NewInsert().Model(&comments).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	milestones := []RegionMilestone{
		{ProjectID: projects[0].ID, Region: "emea", Title: "emea-milestone"},
		{ProjectID: projects[0].ID, Region: "apac", Title: "apac-milestone"},
	}
	if _, err := db.NewInsert().Model(&milestones).Exec(ctx); err != nil {
		t.Fatal(err)
	}

	return regionFixture{
		emeaA: projects[0].ID, emeaB: projects[1].ID, apacA: projects[2].ID,
		emeaTask: tasks[0].ID, apacTask: tasks[1].ID,
	}
}

func archiveProject(ctx context.Context, svc *service.Common[RegionProject], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *RegionProject, payload []byte) (*RegionProject, error) {
	return item, nil
}

func registerRegions(b *router.Builder) {
	writeMethods := []string{router.MethodPost, router.MethodPut, router.MethodPatch, router.MethodDelete, router.MethodBatchCreate, router.MethodBatchUpdate}

	router.RegisterRoutes[RegionProject](b, "/projects",
		router.WithPartition("region", "Region"),
		router.AuthConfig{Methods: []string{router.MethodGet, router.MethodList}, Scopes: []string{"project:read"}},
		router.AuthConfig{Methods: writeMethods, Scopes: []string{"project:write"}},
		router.WithFilters("Name"),
		router.WithSums("Budget"),
		router.WithAction("archive", archiveProject, router.AuthConfig{Scopes: []string{"project:archive"}}),
		func(b *router.Builder) {
			router.RegisterRoutes[RegionTask](b, "/tasks",
				router.AuthConfig{Methods: []string{router.MethodGet, router.MethodList}, Scopes: []string{"task:read"}},
				router.AuthConfig{Methods: writeMethods, Scopes: []string{"task:write"}},
				router.WithRelationName("Tasks"),
				router.WithFilters("Title"),
				func(b *router.Builder) {
					router.RegisterRoutes[RegionComment](b, "/comments",
						router.AllScoped("task:read"),
						router.WithRelationName("Comments"),
					)
				},
			)
			router.RegisterRoutes[RegionMilestone](b, "/milestones",
				router.WithPartition("region", "Region"),
				router.AllScoped("project:read"),
			)
		},
	)
}

// scopedRouter builds a router whose requests carry authInfo (none when nil).
func scopedRouter(authInfo *router.AuthInfo, register func(*router.Builder)) *chi.Mux {
	r := chi.NewRouter()
	if authInfo != nil {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				next.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), router.AuthInfoKey, authInfo)))
			})
		})
	}
	register(router.NewBuilder(r))
	return r
}

func grant(scope, partition string, values ...string) router.ScopedGrant {
	return router.ScopedGrant{Scope: scope, Partition: partition, Values: values}
}

func decodeProjects(t *testing.T, body []byte) regionProjectList {
	t.Helper()
	var list regionProjectList
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("failed to decode list: %v: %s", err, body)
	}
	return list
}

func projectNames(projects []RegionProject) map[string]bool {
	names := make(map[string]bool, len(projects))
	for _, p := range projects {
		names[p.Name] = true
	}
	return names
}

func TestPartition_GlobalScopeIsUnrestricted(t *testing.T) {
	seedRegions(t)
	r := scopedRouter(&router.AuthInfo{UserID: "u", Scopes: []string{"project:read"}}, registerRegions)

	w := hardRequest(t, r, "GET", "/projects", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := len(decodeProjects(t, w.Body.Bytes()).Data); got != 4 {
		t.Errorf("expected all 4 projects, got %d", got)
	}
}

func TestPartition_GrantNarrowsListCountAndSums(t *testing.T) {
	seedRegions(t)
	r := scopedRouter(&router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{grant("project:read", "region", "emea")}}, registerRegions)

	w := hardRequest(t, r, "GET", "/projects?count=true&sum=Budget", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	list := decodeProjects(t, w.Body.Bytes())
	names := projectNames(list.Data)
	if len(list.Data) != 2 || !names["Apollo"] || !names["Atlas"] {
		t.Errorf("expected only EMEA projects, got %+v", list.Data)
	}
	if list.Pagination == nil || list.Pagination.TotalCount == nil || *list.Pagination.TotalCount != 2 {
		t.Errorf("expected total_count 2, got %+v", list.Pagination)
	}
	if list.Sums["Budget"] != 30 {
		t.Errorf("expected Budget sum 30 across EMEA only, got %v", list.Sums["Budget"])
	}
}

func TestPartition_GrantsForTheSameScopeUnion(t *testing.T) {
	seedRegions(t)
	r := scopedRouter(&router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{
		grant("project:read", "region", "emea"),
		grant("project:read", "region", "apac", "emea"),
	}}, registerRegions)

	w := hardRequest(t, r, "GET", "/projects", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if names := projectNames(decodeProjects(t, w.Body.Bytes()).Data); len(names) != 3 || names["Cygnus"] {
		t.Errorf("expected EMEA and APAC projects, got %v", names)
	}
}

func TestPartition_AccessDenied(t *testing.T) {
	seedRegions(t)

	tests := []struct {
		name     string
		authInfo *router.AuthInfo
		want     int
	}{
		{"no auth", nil, http.StatusUnauthorized},
		{"no scope or grant", &router.AuthInfo{UserID: "u"}, http.StatusForbidden},
		{"grant for another scope", &router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{grant("task:read", "region", "emea")}}, http.StatusForbidden},
		{"grant for an undeclared partition", &router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{grant("project:read", "channel", "online")}}, http.StatusForbidden},
		{"grant with no partition", &router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{grant("project:read", "", "emea")}}, http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := scopedRouter(tt.authInfo, registerRegions)
			if w := hardRequest(t, r, "GET", "/projects", ""); w.Code != tt.want {
				t.Errorf("expected %d, got %d: %s", tt.want, w.Code, w.Body.String())
			}
		})
	}
}

func TestPartition_EmptyGrantValuesSeeAndWriteNothing(t *testing.T) {
	seedRegions(t)
	r := scopedRouter(&router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{
		grant("project:read", "region"),
		grant("project:write", "region"),
	}}, registerRegions)

	w := hardRequest(t, r, "GET", "/projects", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := len(decodeProjects(t, w.Body.Bytes()).Data); got != 0 {
		t.Errorf("expected no projects, got %d", got)
	}
	if w := hardRequest(t, r, "POST", "/projects", `{"region":"emea","name":"x"}`); w.Code != http.StatusForbidden {
		t.Errorf("create: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPartition_GetOutsideAccessIs404(t *testing.T) {
	f := seedRegions(t)
	r := scopedRouter(&router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{grant("project:read", "region", "emea")}}, registerRegions)

	if w := hardRequest(t, r, "GET", "/projects/"+strconv.Itoa(f.emeaA), ""); w.Code != http.StatusOK {
		t.Errorf("EMEA project: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w := hardRequest(t, r, "GET", "/projects/"+strconv.Itoa(f.apacA), ""); w.Code != http.StatusNotFound {
		t.Errorf("APAC project: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPartition_WritesUseTheMethodsOwnAccess(t *testing.T) {
	f := seedRegions(t)
	r := scopedRouter(&router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{
		grant("project:read", "region", "emea", "apac"),
		grant("project:write", "region", "emea"),
	}}, registerRegions)

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{"create in writable region", "POST", "/projects", `{"region":"emea","name":"Aurora"}`, http.StatusCreated},
		{"create in read-only region", "POST", "/projects", `{"region":"apac","name":"Bamboo"}`, http.StatusForbidden},
		{"create without partition value", "POST", "/projects", `{"name":"Nowhere"}`, http.StatusBadRequest},
		{"update moving a row out of the writable region", "PUT", "/projects/" + strconv.Itoa(f.emeaA), `{"region":"apac","name":"Apollo"}`, http.StatusForbidden},
		{"update clearing the partition value", "PUT", "/projects/" + strconv.Itoa(f.emeaA), `{"name":"Apollo"}`, http.StatusBadRequest},
		{"update a readable but not writable row", "PUT", "/projects/" + strconv.Itoa(f.apacA), `{"region":"apac","name":"Borealis 2"}`, http.StatusNotFound},
		{"patch within the writable region", "PATCH", "/projects/" + strconv.Itoa(f.emeaB), `{"name":"Atlas 2"}`, http.StatusOK},
		{"delete a readable but not writable row", "DELETE", "/projects/" + strconv.Itoa(f.apacA), "", http.StatusNotFound},
		{"delete within the writable region", "DELETE", "/projects/" + strconv.Itoa(f.emeaB), "", http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if w := hardRequest(t, r, tt.method, tt.path, tt.body); w.Code != tt.want {
				t.Errorf("expected %d, got %d: %s", tt.want, w.Code, w.Body.String())
			}
		})
	}
}

func TestPartition_BatchWrites(t *testing.T) {
	f := seedRegions(t)
	r := scopedRouter(&router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{
		grant("project:read", "region", "emea", "apac"),
		grant("project:write", "region", "emea"),
	}}, registerRegions)

	body := `[{"region":"emea","name":"Aurora"},{"region":"apac","name":"Bamboo"}]`
	if w := hardRequest(t, r, "POST", "/projects/batch", body); w.Code != http.StatusForbidden {
		t.Errorf("batch create with one row outside access: expected 403, got %d: %s", w.Code, w.Body.String())
	}
	w := hardRequest(t, r, "GET", "/projects", "")
	if names := projectNames(decodeProjects(t, w.Body.Bytes()).Data); names["Aurora"] {
		t.Error("batch create must be all-or-nothing: Aurora was created")
	}

	body = `[{"id":` + strconv.Itoa(f.emeaA) + `,"region":"apac","name":"Apollo"}]`
	if w := hardRequest(t, r, "PUT", "/projects/batch", body); w.Code != http.StatusForbidden {
		t.Errorf("batch update moving a row out of access: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPartition_ActionUsesItsOwnScopes(t *testing.T) {
	f := seedRegions(t)
	r := scopedRouter(&router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{grant("project:archive", "region", "emea")}}, registerRegions)

	if w := hardRequest(t, r, "POST", "/projects/"+strconv.Itoa(f.emeaA)+"/archive", ""); w.Code != http.StatusOK {
		t.Errorf("archive EMEA project: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w := hardRequest(t, r, "POST", "/projects/"+strconv.Itoa(f.apacA)+"/archive", ""); w.Code != http.StatusNotFound {
		t.Errorf("archive APAC project: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPartition_ChildrenInheritThroughTheirParent(t *testing.T) {
	f := seedRegions(t)
	r := scopedRouter(&router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{
		grant("task:read", "region", "emea"),
		grant("task:write", "region", "emea"),
	}}, registerRegions)

	listCount := func(path string) int {
		t.Helper()
		w := hardRequest(t, r, "GET", path, "")
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s: expected 200, got %d: %s", path, w.Code, w.Body.String())
		}
		var list struct {
			Data []json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
			t.Fatal(err)
		}
		return len(list.Data)
	}

	emea := "/projects/" + strconv.Itoa(f.emeaA) + "/tasks"
	apac := "/projects/" + strconv.Itoa(f.apacA) + "/tasks"

	if got := listCount(emea); got != 1 {
		t.Errorf("tasks under EMEA project: expected 1, got %d", got)
	}
	if got := listCount(apac); got != 0 {
		t.Errorf("tasks under APAC project: expected 0, got %d", got)
	}
	if w := hardRequest(t, r, "GET", apac+"/"+strconv.Itoa(f.apacTask), ""); w.Code != http.StatusNotFound {
		t.Errorf("get APAC task: expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if w := hardRequest(t, r, "POST", apac, `{"title":"new"}`); w.Code != http.StatusNotFound {
		t.Errorf("create under APAC project: expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if w := hardRequest(t, r, "POST", emea, `{"title":"new"}`); w.Code != http.StatusCreated {
		t.Errorf("create under EMEA project: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	body := `{"project_id":` + strconv.Itoa(f.apacA) + `,"title":"moved"}`
	if w := hardRequest(t, r, "PUT", emea+"/"+strconv.Itoa(f.emeaTask), body); w.Code != http.StatusForbidden {
		t.Errorf("move task to another project: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	if got := listCount(emea + "/" + strconv.Itoa(f.emeaTask) + "/comments"); got != 1 {
		t.Errorf("comments two levels under EMEA project: expected 1, got %d", got)
	}
	if got := listCount(apac + "/" + strconv.Itoa(f.apacTask) + "/comments"); got != 0 {
		t.Errorf("comments two levels under APAC project: expected 0, got %d", got)
	}
}

func TestPartition_ChildWithItsOwnField(t *testing.T) {
	f := seedRegions(t)
	r := scopedRouter(&router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{grant("project:read", "region", "emea")}}, registerRegions)

	w := hardRequest(t, r, "GET", "/projects/"+strconv.Itoa(f.emeaA)+"/milestones", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var list struct {
		Data []RegionMilestone `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 1 || list.Data[0].Title != "emea-milestone" {
		t.Errorf("expected only the milestone tagged EMEA, got %+v", list.Data)
	}
}

func TestPartition_RelationsUseTheChildRoutesAccess(t *testing.T) {
	f := seedRegions(t)
	r := scopedRouter(&router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{
		grant("project:read", "region", "emea", "apac"),
		grant("task:read", "region", "emea"),
	}}, registerRegions)

	w := hardRequest(t, r, "GET", "/projects?include=Tasks&include_count=Tasks", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	list := decodeProjects(t, w.Body.Bytes())
	for _, p := range list.Data {
		switch p.ID {
		case f.emeaA:
			if len(p.Tasks) != 1 {
				t.Errorf("EMEA project: expected its task included, got %d", len(p.Tasks))
			}
		case f.apacA:
			if len(p.Tasks) != 0 {
				t.Errorf("APAC project: task outside task:read access was included: %+v", p.Tasks)
			}
		}
	}
	if list.Counts["Tasks"][strconv.Itoa(f.apacA)] != 0 || list.Counts["Tasks"][strconv.Itoa(f.emeaA)] != 1 {
		t.Errorf("include_count must only count visible tasks, got %v", list.Counts)
	}

	w = hardRequest(t, r, "GET", "/projects?filter[Tasks][exists]=true", "")
	if names := projectNames(decodeProjects(t, w.Body.Bytes()).Data); len(names) != 1 || !names["Apollo"] {
		t.Errorf("exists filter must only see visible tasks, got %v", names)
	}

	w = hardRequest(t, r, "GET", "/projects?filter[Tasks.Title]=apac-task", "")
	if got := len(decodeProjects(t, w.Body.Bytes()).Data); got != 0 {
		t.Errorf("child field filter must not match tasks outside access, got %d projects", got)
	}
}

func TestPartition_ParentRelationsUseTheParentRoutesAccess(t *testing.T) {
	f := seedRegions(t)
	path := "/projects/" + strconv.Itoa(f.emeaA) + "/tasks"

	decodeTasks := func(t *testing.T, r http.Handler, query string) []RegionTask {
		t.Helper()
		w := hardRequest(t, r, "GET", path+query, "")
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var list struct {
			Data []RegionTask `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
			t.Fatal(err)
		}
		return list.Data
	}

	withEMEA := scopedRouter(&router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{
		grant("task:read", "region", "emea"),
		grant("project:read", "region", "emea"),
	}}, registerRegions)
	tasks := decodeTasks(t, withEMEA, "?include=Project")
	if len(tasks) != 1 || tasks[0].Project == nil || tasks[0].Project.Name != "Apollo" {
		t.Errorf("parent include within project:read access: expected Apollo, got %+v", tasks)
	}
	if got := decodeTasks(t, withEMEA, "?filter[Project.Name]=Apollo"); len(got) != 1 {
		t.Errorf("parent filter within access: expected 1, got %d", len(got))
	}

	withAPAC := scopedRouter(&router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{
		grant("task:read", "region", "emea"),
		grant("project:read", "region", "apac"),
	}}, registerRegions)
	tasks = decodeTasks(t, withAPAC, "?include=Project")
	if len(tasks) != 1 || tasks[0].Project != nil {
		t.Errorf("parent include outside project:read access: expected task without its project, got %+v", tasks)
	}
	if got := decodeTasks(t, withAPAC, "?filter[Project.Name]=Apollo"); len(got) != 0 {
		t.Errorf("parent filter outside access: expected 0, got %d", len(got))
	}
}

func TestPartition_NestedIncludesUseEachLevelsAccess(t *testing.T) {
	f := seedRegions(t)
	r := scopedRouter(&router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{
		grant("project:read", "region", "emea", "apac"),
		grant("task:read", "region", "emea"),
	}}, registerRegions)

	w := hardRequest(t, r, "GET", "/projects?include=Tasks.Comments", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	for _, p := range decodeProjects(t, w.Body.Bytes()).Data {
		switch p.ID {
		case f.emeaA:
			if len(p.Tasks) != 1 || len(p.Tasks[0].Comments) != 1 {
				t.Errorf("EMEA project: expected task with its comment, got %+v", p.Tasks)
			}
		case f.apacA:
			if len(p.Tasks) != 0 {
				t.Errorf("APAC project: task outside access was included: %+v", p.Tasks)
			}
		}
	}

	apacProjects := scopedRouter(&router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{
		grant("task:read", "region", "emea"),
		grant("project:read", "region", "apac"),
	}}, registerRegions)
	path := "/projects/" + strconv.Itoa(f.emeaA) + "/tasks/" + strconv.Itoa(f.emeaTask) + "/comments?include=Task.Project"
	w = hardRequest(t, apacProjects, "GET", path, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var comments struct {
		Data []RegionComment `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &comments); err != nil {
		t.Fatal(err)
	}
	if len(comments.Data) != 1 || comments.Data[0].Task == nil {
		t.Fatalf("expected the comment with its task included, got %+v", comments.Data)
	}
	if comments.Data[0].Task.Project != nil {
		t.Errorf("grandparent outside project:read access was included: %+v", comments.Data[0].Task.Project)
	}

	emeaProjects := scopedRouter(&router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{
		grant("task:read", "region", "emea"),
		grant("project:read", "region", "emea"),
	}}, registerRegions)
	w = hardRequest(t, emeaProjects, "GET", path, "")
	comments.Data = nil
	if err := json.Unmarshal(w.Body.Bytes(), &comments); err != nil {
		t.Fatal(err)
	}
	if len(comments.Data) != 1 || comments.Data[0].Task == nil || comments.Data[0].Task.Project == nil || comments.Data[0].Task.Project.Name != "Apollo" {
		t.Errorf("grandparent within project:read access: expected Apollo included, got %s", w.Body.String())
	}
}

func TestPartition_PrimaryKeyIsNotAPartition(t *testing.T) {
	f := seedRegions(t)
	register := func(b *router.Builder) {
		router.RegisterRoutes[RegionProject](b, "/projects",
			router.WithPartition("project", "ID"),
			router.AllScoped("project:read"),
			router.AuthConfig{Methods: []string{router.MethodPost, router.MethodPatch}, Scopes: []string{"project:write"}},
		)
	}

	scoped := scopedRouter(&router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{
		grant("project:read", "project", strconv.Itoa(f.emeaA)),
		grant("project:write", "project", strconv.Itoa(f.emeaA)),
	}}, register)
	w := hardRequest(t, scoped, "GET", "/projects", "")
	if got := len(decodeProjects(t, w.Body.Bytes()).Data); got != 0 {
		t.Errorf("scoped list on a primary key partition: expected no rows, got %d", got)
	}
	if w := hardRequest(t, scoped, "PATCH", "/projects/"+strconv.Itoa(f.emeaA), `{"name":"x"}`); w.Code != http.StatusNotFound {
		t.Errorf("scoped update on a primary key partition: expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if w := hardRequest(t, scoped, "POST", "/projects", `{"region":"emea","name":"New"}`); w.Code != http.StatusForbidden {
		t.Errorf("scoped create on a primary key partition: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	global := scopedRouter(&router.AuthInfo{UserID: "u", Scopes: []string{"project:read", "project:write"}}, register)
	w = hardRequest(t, global, "GET", "/projects", "")
	if got := len(decodeProjects(t, w.Body.Bytes()).Data); got != 4 {
		t.Errorf("global list: expected all 4 projects, got %d", got)
	}
	if w := hardRequest(t, global, "POST", "/projects", `{"region":"emea","name":"New"}`); w.Code != http.StatusCreated {
		t.Errorf("global create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPartition_MisconfiguredScopesAreBlocked(t *testing.T) {
	seedRegions(t)
	register := func(b *router.Builder) {
		router.RegisterRoutes[RegionProject](b, "/projects",
			router.WithPartition("region", "Region"),
			router.PublicReadOnly(),
			router.AuthConfig{Methods: []string{router.MethodPost}, Scopes: []string{router.ScopeAuthOnly}},
		)
	}

	if w := hardRequest(t, scopedRouter(nil, register), "GET", "/projects", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("public config without auth: expected 401, got %d: %s", w.Code, w.Body.String())
	}
	r := scopedRouter(&router.AuthInfo{UserID: "u", Scopes: []string{"project:read"}}, register)
	if w := hardRequest(t, r, "GET", "/projects", ""); w.Code != http.StatusForbidden {
		t.Errorf("public config with auth: expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if w := hardRequest(t, r, "POST", "/projects", `{"region":"emea","name":"x"}`); w.Code != http.StatusForbidden {
		t.Errorf("auth-only config: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func seedOrders(t *testing.T) {
	t.Helper()
	db := hardTables(t, (*RegionOrder)(nil))
	orders := []RegionOrder{
		{OrgID: "org-a", Region: "emea", Channel: "online", CustomerID: "bob", Title: "emea-online-bob"},
		{OrgID: "org-a", Region: "emea", Channel: "retail", CustomerID: "bob", Title: "emea-retail-bob"},
		{OrgID: "org-a", Region: "apac", Channel: "online", CustomerID: "alice", Title: "apac-online-alice"},
		{OrgID: "org-a", Region: "apac", Channel: "online", CustomerID: "bob", Title: "apac-online-bob"},
		{OrgID: "org-b", Region: "emea", Channel: "online", CustomerID: "alice", Title: "other-org"},
	}
	if _, err := db.NewInsert().Model(&orders).Exec(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func orderTitles(t *testing.T, r http.Handler) map[string]bool {
	t.Helper()
	w := hardRequest(t, r, "GET", "/orders", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var list struct {
		Data []RegionOrder `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	titles := make(map[string]bool, len(list.Data))
	for _, o := range list.Data {
		titles[o.Title] = true
	}
	return titles
}

func TestPartition_MultiplePartitionsCombineWithAND(t *testing.T) {
	seedOrders(t)
	register := func(b *router.Builder) {
		router.RegisterRoutes[RegionOrder](b, "/orders",
			router.WithPartition("region", "Region"),
			router.WithPartition("channel", "Channel"),
			router.AllScoped("order:read"),
		)
	}

	both := scopedRouter(&router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{
		grant("order:read", "region", "emea"),
		grant("order:read", "channel", "online"),
	}}, register)
	if titles := orderTitles(t, both); len(titles) != 2 || !titles["emea-online-bob"] || !titles["other-org"] {
		t.Errorf("expected EMEA AND online only, got %v", titles)
	}

	oneOnly := scopedRouter(&router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{grant("order:read", "region", "emea")}}, register)
	if w := hardRequest(t, oneOnly, "GET", "/orders", ""); w.Code != http.StatusForbidden {
		t.Errorf("grant for only one of two partitions: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPartition_TenantAndPartitionTogether(t *testing.T) {
	seedOrders(t)
	r := addTenantAuthMiddleware(chi.NewRouter(), "u", "org-a", []string{"order:read"})
	router.RegisterRoutes[RegionOrder](router.NewBuilder(r), "/orders",
		router.WithTenantScope("OrgID"),
		router.WithPartition("region", "Region"),
		router.AllScoped("order:read"),
	)

	if titles := orderTitles(t, r); len(titles) != 4 || titles["other-org"] {
		t.Errorf("expected org-a orders only, got %v", titles)
	}
}

func TestPartition_ScopedOwnershipBypass(t *testing.T) {
	seedOrders(t)
	register := func(b *router.Builder) {
		router.RegisterRoutes[RegionOrder](b, "/orders",
			router.WithPartition("region", "Region"),
			router.AuthConfig{
				Methods:   []string{router.MethodAll},
				Scopes:    []string{"order:read"},
				Ownership: &router.OwnershipConfig{Fields: []string{"CustomerID"}, BypassScopes: []string{"support"}},
			},
		)
	}

	customer := scopedRouter(&router.AuthInfo{UserID: "alice", Scopes: []string{"order:read"}}, register)
	if titles := orderTitles(t, customer); len(titles) != 2 || !titles["apac-online-alice"] || !titles["other-org"] {
		t.Errorf("customer: expected only own orders, got %v", titles)
	}

	regionSupport := scopedRouter(&router.AuthInfo{
		UserID: "alice",
		Scopes: []string{"order:read"},
		Grants: []router.ScopedGrant{grant("support", "region", "emea")},
	}, register)
	titles := orderTitles(t, regionSupport)
	if len(titles) != 4 || !titles["emea-online-bob"] || !titles["emea-retail-bob"] || !titles["apac-online-alice"] || !titles["other-org"] {
		t.Errorf("EMEA support: expected own orders plus all EMEA orders, got %v", titles)
	}

	globalSupport := scopedRouter(&router.AuthInfo{UserID: "alice", Scopes: []string{"order:read", "support"}}, register)
	if titles := orderTitles(t, globalSupport); len(titles) != 5 {
		t.Errorf("global support: expected all orders, got %v", titles)
	}

	scopedReader := scopedRouter(&router.AuthInfo{
		UserID: "alice",
		Grants: []router.ScopedGrant{grant("order:read", "region", "apac"), grant("support", "region", "emea")},
	}, register)
	if titles := orderTitles(t, scopedReader); len(titles) != 1 || !titles["apac-online-alice"] {
		t.Errorf("bypass must not widen partition access: expected own APAC order only, got %v", titles)
	}
}
