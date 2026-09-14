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

type SharedProject struct {
	bun.BaseModel `bun:"table:shared_projects"`
	ID            int           `bun:"id,pk,autoincrement" json:"id"`
	OrgID         string        `bun:"org_id" json:"org_id"`
	OwnerID       string        `bun:"owner_id" json:"owner_id"`
	Region        string        `bun:"region" json:"region"`
	Name          string        `bun:"name" json:"name"`
	Tasks         []*SharedTask `bun:"rel:has-many,join:id=project_id" json:"tasks,omitempty"`
}

type SharedTask struct {
	bun.BaseModel `bun:"table:shared_tasks"`
	ID            int              `bun:"id,pk,autoincrement" json:"id"`
	OrgID         string           `bun:"org_id" json:"org_id"`
	ProjectID     int              `bun:"project_id" json:"project_id"`
	Project       *SharedProject   `bun:"rel:belongs-to,join:project_id=id" json:"project,omitempty"`
	Title         string           `bun:"title" json:"title"`
	Comments      []*SharedComment `bun:"rel:has-many,join:id=task_id" json:"comments,omitempty"`
}

type SharedComment struct {
	bun.BaseModel `bun:"table:shared_comments"`
	ID            int         `bun:"id,pk,autoincrement" json:"id"`
	OrgID         string      `bun:"org_id" json:"org_id"`
	TaskID        int         `bun:"task_id" json:"task_id"`
	Task          *SharedTask `bun:"rel:belongs-to,join:task_id=id" json:"task,omitempty"`
	Text          string      `bun:"text" json:"text"`
}

type ProjectShare struct {
	bun.BaseModel `bun:"table:project_shares"`
	ID            int    `bun:"id,pk,autoincrement" json:"id"`
	ProjectID     int    `bun:"project_id" json:"project_id"`
	UserID        string `bun:"user_id" json:"user_id"`
	Level         string `bun:"level" json:"level"`
}

type shareFixture struct {
	alpha, beta, gamma  int
	alphaTask, betaTask int
}

// seedShares creates Alice's EMEA project Alpha and APAC project Beta, and Carol's AMER project
// Gamma, all in org-a, with a task under Alpha (with a comment) and a task under Beta.
// Bob has a viewer share on Alpha; Dave has an editor share on Beta.
func seedShares(t *testing.T) shareFixture {
	t.Helper()
	db := hardTables(t, (*SharedProject)(nil), (*SharedTask)(nil), (*SharedComment)(nil), (*ProjectShare)(nil))
	ctx := context.Background()

	projects := []SharedProject{
		{OrgID: "org-a", OwnerID: "alice", Region: "emea", Name: "Alpha"},
		{OrgID: "org-a", OwnerID: "alice", Region: "apac", Name: "Beta"},
		{OrgID: "org-a", OwnerID: "carol", Region: "amer", Name: "Gamma"},
	}
	if _, err := db.NewInsert().Model(&projects).Returning("*").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	tasks := []SharedTask{
		{OrgID: "org-a", ProjectID: projects[0].ID, Title: "alpha-task"},
		{OrgID: "org-a", ProjectID: projects[1].ID, Title: "beta-task"},
	}
	if _, err := db.NewInsert().Model(&tasks).Returning("*").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	comments := []SharedComment{{OrgID: "org-a", TaskID: tasks[0].ID, Text: "alpha-comment"}}
	if _, err := db.NewInsert().Model(&comments).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	shares := []ProjectShare{
		{ProjectID: projects[0].ID, UserID: "bob", Level: "viewer"},
		{ProjectID: projects[1].ID, UserID: "dave", Level: "editor"},
	}
	if _, err := db.NewInsert().Model(&shares).Exec(ctx); err != nil {
		t.Fatal(err)
	}

	return shareFixture{
		alpha: projects[0].ID, beta: projects[1].ID, gamma: projects[2].ID,
		alphaTask: tasks[0].ID, betaTask: tasks[1].ID,
	}
}

// projectShares returns a share setting accepting the given levels (any share when none).
// target is nil for the project route itself.
func projectShares(target any, levels ...string) *router.ShareConfig {
	return &router.ShareConfig{
		Model:       (*ProjectShare)(nil),
		Target:      target,
		TargetField: "ProjectID",
		UserField:   "UserID",
		LevelField:  "Level",
		Levels:      levels,
	}
}

// registerSharedProjects registers owner-based projects where any share can read, editor shares
// can update, and only owners can delete, with tasks and comments accepting project shares.
func registerSharedProjects(b *router.Builder) {
	owned := &router.OwnershipConfig{Fields: []string{"OwnerID"}}
	router.RegisterRoutes[SharedProject](b, "/projects",
		router.AuthConfig{Methods: []string{router.MethodGet, router.MethodList}, Ownership: owned, Share: projectShares(nil)},
		router.AuthConfig{Methods: []string{router.MethodPut, router.MethodPatch}, Ownership: owned, Share: projectShares(nil, "editor")},
		router.AuthConfig{Methods: []string{router.MethodPost, router.MethodDelete}, Ownership: owned},
		func(b *router.Builder) {
			router.RegisterRoutes[SharedTask](b, "/tasks",
				router.AuthConfig{Methods: []string{router.MethodGet, router.MethodList}, Scopes: []string{router.ScopeAuthOnly}, Share: projectShares((*SharedProject)(nil))},
				router.AuthConfig{Methods: []string{router.MethodPost, router.MethodPut, router.MethodPatch}, Scopes: []string{router.ScopeAuthOnly}, Share: projectShares((*SharedProject)(nil), "editor")},
				router.AuthConfig{Methods: []string{router.MethodDelete}, Scopes: []string{router.ScopeAuthOnly}},
				router.WithRelationName("Tasks"),
				func(b *router.Builder) {
					router.RegisterRoutes[SharedComment](b, "/comments",
						router.AuthConfig{Methods: []string{router.MethodAll}, Scopes: []string{router.ScopeAuthOnly}, Share: projectShares((*SharedProject)(nil))},
						router.WithRelationName("Comments"),
					)
				},
			)
		},
	)
}

func asShareUser(userID string, register func(*router.Builder)) *chi.Mux {
	return scopedRouter(&router.AuthInfo{UserID: userID}, register)
}

func listNames(t *testing.T, r http.Handler, path string) map[string]bool {
	t.Helper()
	w := hardRequest(t, r, "GET", path, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s: expected 200, got %d: %s", path, w.Code, w.Body.String())
	}
	var list struct {
		Data []struct {
			Name  string `json:"name"`
			Title string `json:"title"`
			Text  string `json:"text"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	names := make(map[string]bool, len(list.Data))
	for _, item := range list.Data {
		names[item.Name+item.Title+item.Text] = true
	}
	return names
}

func TestShare_ReadsSharedRows(t *testing.T) {
	f := seedShares(t)

	bob := asShareUser("bob", registerSharedProjects)
	if names := listNames(t, bob, "/projects"); len(names) != 1 || !names["Alpha"] {
		t.Errorf("Bob: expected only the project shared with him, got %v", names)
	}
	if w := hardRequest(t, bob, "GET", "/projects/"+strconv.Itoa(f.alpha), ""); w.Code != http.StatusOK {
		t.Errorf("Bob get shared project: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w := hardRequest(t, bob, "GET", "/projects/"+strconv.Itoa(f.beta), ""); w.Code != http.StatusNotFound {
		t.Errorf("Bob get unshared project: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	alice := asShareUser("alice", registerSharedProjects)
	if names := listNames(t, alice, "/projects"); len(names) != 2 || !names["Alpha"] || !names["Beta"] {
		t.Errorf("Alice: expected her own projects, got %v", names)
	}
}

func TestShare_LevelsPerMethod(t *testing.T) {
	f := seedShares(t)

	bob := asShareUser("bob", registerSharedProjects)
	if w := hardRequest(t, bob, "PATCH", "/projects/"+strconv.Itoa(f.alpha), `{"name":"Alpha 2"}`); w.Code != http.StatusNotFound {
		t.Errorf("viewer update: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	dave := asShareUser("dave", registerSharedProjects)
	w := hardRequest(t, dave, "PATCH", "/projects/"+strconv.Itoa(f.beta), `{"name":"Beta 2","owner_id":"dave"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("editor update: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated SharedProject
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Beta 2" || updated.OwnerID != "alice" {
		t.Errorf("editor update: expected the name changed and the owner kept, got %+v", updated)
	}
	if w := hardRequest(t, dave, "DELETE", "/projects/"+strconv.Itoa(f.beta), ""); w.Code != http.StatusNotFound {
		t.Errorf("delete does not accept shares: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestShare_ChildRoutesAcceptParentShares(t *testing.T) {
	f := seedShares(t)
	alphaTasks := "/projects/" + strconv.Itoa(f.alpha) + "/tasks"
	betaTasks := "/projects/" + strconv.Itoa(f.beta) + "/tasks"

	bob := asShareUser("bob", registerSharedProjects)
	if names := listNames(t, bob, alphaTasks); len(names) != 1 || !names["alpha-task"] {
		t.Errorf("viewer: expected the shared project's tasks, got %v", names)
	}
	if names := listNames(t, bob, betaTasks); len(names) != 0 {
		t.Errorf("viewer: expected no tasks under an unshared project, got %v", names)
	}
	if names := listNames(t, bob, alphaTasks+"/"+strconv.Itoa(f.alphaTask)+"/comments"); len(names) != 1 {
		t.Errorf("viewer: expected comments two levels under the shared project, got %v", names)
	}
	if w := hardRequest(t, bob, "POST", alphaTasks, `{"title":"new"}`); w.Code != http.StatusNotFound {
		t.Errorf("viewer create task: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	dave := asShareUser("dave", registerSharedProjects)
	if w := hardRequest(t, dave, "POST", betaTasks, `{"title":"new","org_id":"org-a"}`); w.Code != http.StatusCreated {
		t.Errorf("editor create task: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if w := hardRequest(t, dave, "PATCH", betaTasks+"/"+strconv.Itoa(f.betaTask), `{"title":"renamed"}`); w.Code != http.StatusOK {
		t.Errorf("editor update task: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w := hardRequest(t, dave, "DELETE", betaTasks+"/"+strconv.Itoa(f.betaTask), ""); w.Code != http.StatusNotFound {
		t.Errorf("task delete does not accept shares: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestShare_ChildRouteWithoutShareSettingsIsNotReached(t *testing.T) {
	f := seedShares(t)
	register := func(b *router.Builder) {
		owned := &router.OwnershipConfig{Fields: []string{"OwnerID"}}
		router.RegisterRoutes[SharedProject](b, "/projects",
			router.AuthConfig{Methods: []string{router.MethodAll}, Ownership: owned, Share: projectShares(nil)},
			func(b *router.Builder) {
				router.RegisterRoutes[SharedTask](b, "/tasks", router.IsAuthenticated())
			},
		)
	}

	bob := asShareUser("bob", register)
	if w := hardRequest(t, bob, "GET", "/projects/"+strconv.Itoa(f.alpha), ""); w.Code != http.StatusOK {
		t.Errorf("shared project: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if names := listNames(t, bob, "/projects/"+strconv.Itoa(f.alpha)+"/tasks"); len(names) != 0 {
		t.Errorf("task route without share settings: expected no tasks, got %v", names)
	}
}

func TestShare_RelationsUseTheRelatedRoutesShare(t *testing.T) {
	f := seedShares(t)
	bob := asShareUser("bob", registerSharedProjects)

	w := hardRequest(t, bob, "GET", "/projects?include=Tasks&include_count=Tasks&filter[Tasks][exists]=true", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var list struct {
		Data   []SharedProject           `json:"data"`
		Counts map[string]map[string]int `json:"counts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 1 || len(list.Data[0].Tasks) != 1 {
		t.Errorf("expected the shared project with its task included, got %+v", list.Data)
	}
	if list.Counts["Tasks"][strconv.Itoa(f.alpha)] != 1 {
		t.Errorf("expected the shared project's task counted, got %v", list.Counts)
	}

	w = hardRequest(t, bob, "GET", "/projects/"+strconv.Itoa(f.alpha)+"/tasks?include=Project", "")
	var tasks struct {
		Data []SharedTask `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &tasks); err != nil {
		t.Fatal(err)
	}
	if len(tasks.Data) != 1 || tasks.Data[0].Project == nil || tasks.Data[0].Project.Name != "Alpha" {
		t.Errorf("expected the task with its shared project included, got %s", w.Body.String())
	}
}

func TestShare_WidensPastPartitions(t *testing.T) {
	f := seedShares(t)
	register := func(b *router.Builder) {
		router.RegisterRoutes[SharedProject](b, "/projects",
			router.WithPartition("region", "Region"),
			router.AuthConfig{Methods: []string{router.MethodGet, router.MethodList}, Scopes: []string{"project:read"}, Share: projectShares(nil)},
			router.AuthConfig{Methods: []string{router.MethodPut, router.MethodPatch}, Scopes: []string{"project:write"}, Share: projectShares(nil, "editor")},
		)
	}

	regionAndShare := scopedRouter(&router.AuthInfo{UserID: "bob", Grants: []router.ScopedGrant{grant("project:read", "region", "apac")}}, register)
	if names := listNames(t, regionAndShare, "/projects"); len(names) != 2 || !names["Alpha"] || !names["Beta"] {
		t.Errorf("APAC grant plus a share on an EMEA project: expected both, got %v", names)
	}

	shareOnly := scopedRouter(&router.AuthInfo{UserID: "bob", Grants: []router.ScopedGrant{grant("project:read", "region")}}, register)
	if names := listNames(t, shareOnly, "/projects"); len(names) != 1 || !names["Alpha"] {
		t.Errorf("grant with no regions plus a share: expected only the shared project, got %v", names)
	}

	dave := scopedRouter(&router.AuthInfo{UserID: "dave", Grants: []router.ScopedGrant{
		grant("project:read", "region"),
		grant("project:write", "region", "emea"),
	}}, register)
	beta := "/projects/" + strconv.Itoa(f.beta)
	if w := hardRequest(t, dave, "PATCH", beta, `{"name":"Beta 2"}`); w.Code != http.StatusOK {
		t.Errorf("editor share outside the write region: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w := hardRequest(t, dave, "PATCH", beta, `{"region":"emea"}`); w.Code != http.StatusForbidden {
		t.Errorf("moving a shared project to another region: expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if w := hardRequest(t, dave, "GET", "/projects/"+strconv.Itoa(f.gamma), ""); w.Code != http.StatusNotFound {
		t.Errorf("unshared project outside access: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestShare_TenantScopeStillApplies(t *testing.T) {
	f := seedShares(t)
	register := func(b *router.Builder) {
		owned := &router.OwnershipConfig{Fields: []string{"OwnerID"}}
		router.RegisterRoutes[SharedProject](b, "/projects",
			router.WithTenantScope("OrgID"),
			router.AuthConfig{Methods: []string{router.MethodAll}, Ownership: owned, Share: projectShares(nil)},
		)
	}

	sameOrg := addTenantAuthMiddleware(chi.NewRouter(), "bob", "org-a", nil)
	register(router.NewBuilder(sameOrg))
	if w := hardRequest(t, sameOrg, "GET", "/projects/"+strconv.Itoa(f.alpha), ""); w.Code != http.StatusOK {
		t.Errorf("share within the tenant: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	otherOrg := addTenantAuthMiddleware(chi.NewRouter(), "bob", "org-b", nil)
	register(router.NewBuilder(otherOrg))
	if w := hardRequest(t, otherOrg, "GET", "/projects/"+strconv.Itoa(f.alpha), ""); w.Code != http.StatusNotFound {
		t.Errorf("share across tenants: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestShare_RequiresAUserID(t *testing.T) {
	seedShares(t)
	register := func(b *router.Builder) {
		router.RegisterRoutes[SharedProject](b, "/projects",
			router.AuthConfig{Methods: []string{router.MethodAll}, Scopes: []string{"project:read"}, Share: projectShares(nil)},
		)
	}

	r := scopedRouter(&router.AuthInfo{Scopes: []string{"project:read"}}, register)
	if w := hardRequest(t, r, "GET", "/projects", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("share setting without a user ID: expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestShare_InvalidSettingsGrantNothing(t *testing.T) {
	f := seedShares(t)
	register := func(b *router.Builder) {
		owned := &router.OwnershipConfig{Fields: []string{"OwnerID"}}
		invalid := projectShares(nil)
		invalid.UserField = "Missing"
		router.RegisterRoutes[SharedProject](b, "/projects",
			router.AuthConfig{Methods: []string{router.MethodAll}, Ownership: owned, Share: invalid},
		)
	}

	bob := asShareUser("bob", register)
	if w := hardRequest(t, bob, "GET", "/projects/"+strconv.Itoa(f.alpha), ""); w.Code != http.StatusNotFound {
		t.Errorf("invalid share setting: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}
