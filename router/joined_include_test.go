package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/router"
)

type IncProject struct {
	bun.BaseModel `bun:"table:inc_projects"`
	ID            int             `bun:"id,pk,autoincrement" json:"id"`
	Region        string          `bun:"region" json:"region"`
	OwnerID       string          `bun:"owner_id" json:"owner_id"`
	Code          string          `bun:"code" json:"code"`
	Name          string          `bun:"name" json:"name"`
	CategoryID    int             `bun:"category_id,nullzero" json:"category_id,omitempty"`
	Category      *IncCategory    `bun:"rel:belongs-to,join:category_id=id" json:"category,omitempty"`
	Charter       *IncCharter     `bun:"rel:has-one,join:id=project_id" json:"charter,omitempty"`
	Tasks         []*IncTask      `bun:"rel:has-many,join:id=project_id" json:"tasks,omitempty"`
	Milestones    []*IncMilestone `bun:"rel:has-many,join:code=project_code" json:"milestones,omitempty"`
}

// IncCategory is a lookup the project points at. Its region and owner_id columns share their
// names with the project's, so a condition on the wrong table would still be valid SQL.
type IncCategory struct {
	bun.BaseModel `bun:"table:inc_categories"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	Region        string    `bun:"region" json:"region"`
	OwnerID       string    `bun:"owner_id" json:"owner_id"`
	Name          string    `bun:"name" json:"name"`
	Tags          []*IncTag `bun:"rel:has-many,join:id=category_id" json:"tags,omitempty"`
	GroupID       int       `bun:"group_id,nullzero" json:"group_id,omitempty"`
	Group         *IncGroup `bun:"rel:belongs-to,join:group_id=id" json:"group,omitempty"`
}

// IncGroup is a lookup the category points at, so lookups can be included two joins deep.
type IncGroup struct {
	bun.BaseModel `bun:"table:inc_groups"`
	ID            int    `bun:"id,pk,autoincrement" json:"id"`
	Region        string `bun:"region" json:"region"`
	Name          string `bun:"name" json:"name"`
}

type IncCharter struct {
	bun.BaseModel `bun:"table:inc_charters"`
	ID            int    `bun:"id,pk,autoincrement" json:"id"`
	ProjectID     int    `bun:"project_id" json:"project_id"`
	OwnerID       string `bun:"owner_id" json:"owner_id"`
	Text          string `bun:"text" json:"text"`
}

type IncTask struct {
	bun.BaseModel `bun:"table:inc_tasks"`
	ID            int          `bun:"id,pk,autoincrement" json:"id"`
	ProjectID     int          `bun:"project_id" json:"project_id"`
	Project       *IncProject  `bun:"rel:belongs-to,join:project_id=id" json:"project,omitempty"`
	CategoryID    int          `bun:"category_id,nullzero" json:"category_id,omitempty"`
	Category      *IncCategory `bun:"rel:belongs-to,join:category_id=id" json:"category,omitempty"`
	OwnerID       string       `bun:"owner_id" json:"owner_id"`
	Title         string       `bun:"title" json:"title"`
}

type IncTag struct {
	bun.BaseModel `bun:"table:inc_tags"`
	ID            int    `bun:"id,pk,autoincrement" json:"id"`
	CategoryID    int    `bun:"category_id" json:"category_id"`
	Name          string `bun:"name" json:"name"`
}

type IncMilestone struct {
	bun.BaseModel `bun:"table:inc_milestones"`
	ID            int    `bun:"id,pk,autoincrement" json:"id"`
	ProjectCode   string `bun:"project_code" json:"project_code"`
	Name          string `bun:"name" json:"name"`
}

type IncShare struct {
	bun.BaseModel `bun:"table:inc_shares"`
	ID            int    `bun:"id,pk,autoincrement" json:"id"`
	ProjectID     int    `bun:"project_id" json:"project_id"`
	UserID        string `bun:"user_id" json:"user_id"`
}

// seedIncludes creates:
//   - group group-apac (region apac)
//   - categories cat-apac (region apac, owner bob, in group-apac) and cat-emea (region emea,
//     owner alice)
//   - projects alpha (emea, owner alice, cat-apac), beta (apac, owner bob, cat-emea), and
//     gamma (emea, owner alice, no category)
//   - charters alpha-charter (owner bob) and beta-charter (owner alice)
//   - tasks alpha-task (cat-emea) and beta-task (cat-apac)
//   - tags apac-tag and emea-tag, milestones alpha-milestone and beta-milestone
//   - a share of beta with sam
func seedIncludes(t *testing.T) {
	t.Helper()
	db := hardTables(t, (*IncProject)(nil), (*IncCategory)(nil), (*IncGroup)(nil), (*IncCharter)(nil), (*IncTask)(nil), (*IncTag)(nil), (*IncMilestone)(nil), (*IncShare)(nil))
	ctx := context.Background()

	group := IncGroup{Region: "apac", Name: "group-apac"}
	if _, err := db.NewInsert().Model(&group).Returning("*").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	categories := []IncCategory{
		{Region: "apac", OwnerID: "bob", Name: "cat-apac", GroupID: group.ID},
		{Region: "emea", OwnerID: "alice", Name: "cat-emea"},
	}
	if _, err := db.NewInsert().Model(&categories).Returning("*").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	projects := []IncProject{
		{Region: "emea", OwnerID: "alice", Code: "A", Name: "alpha", CategoryID: categories[0].ID},
		{Region: "apac", OwnerID: "bob", Code: "B", Name: "beta", CategoryID: categories[1].ID},
		{Region: "emea", OwnerID: "alice", Code: "G", Name: "gamma"},
	}
	if _, err := db.NewInsert().Model(&projects).Returning("*").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	inserts := []any{
		&[]IncCharter{
			{ProjectID: projects[0].ID, OwnerID: "bob", Text: "alpha-charter"},
			{ProjectID: projects[1].ID, OwnerID: "alice", Text: "beta-charter"},
		},
		&[]IncTask{
			{ProjectID: projects[0].ID, CategoryID: categories[1].ID, OwnerID: "bob", Title: "alpha-task"},
			{ProjectID: projects[1].ID, CategoryID: categories[0].ID, OwnerID: "alice", Title: "beta-task"},
		},
		&[]IncTag{
			{CategoryID: categories[0].ID, Name: "apac-tag"},
			{CategoryID: categories[1].ID, Name: "emea-tag"},
		},
		&[]IncMilestone{
			{ProjectCode: "A", Name: "alpha-milestone"},
			{ProjectCode: "B", Name: "beta-milestone"},
		},
		&[]IncShare{{ProjectID: projects[1].ID, UserID: "sam"}},
	}
	for _, rows := range inserts {
		if _, err := db.NewInsert().Model(rows).Exec(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

// includeRoutes configures the routes registered by registerIncludes. Each field is the auth
// config of one route's GET and LIST methods; categoryOptions and groupOptions are added to
// those lookups.
type includeRoutes struct {
	project, category, group, charter, task, taskCategory, tag, milestone router.AuthConfig
	categoryOptions, groupOptions                                         []any
	categoryRelationName                                                  string
}

func readConfig(scope string) router.AuthConfig {
	return router.AuthConfig{Methods: []string{router.MethodGet, router.MethodList}, Scopes: []string{scope}}
}

func defaultIncludeRoutes() includeRoutes {
	return includeRoutes{
		project:              readConfig("project:read"),
		category:             readConfig("category:read"),
		group:                readConfig("group:read"),
		charter:              readConfig("charter:read"),
		task:                 readConfig("task:read"),
		taskCategory:         readConfig("category:read"),
		tag:                  readConfig("tag:read"),
		milestone:            readConfig("milestone:read"),
		categoryRelationName: "Category",
	}
}

func projectShareConfig(target any) *router.ShareConfig {
	return &router.ShareConfig{Model: (*IncShare)(nil), Target: target, TargetField: "ProjectID", UserField: "UserID"}
}

// registerIncludes registers projects partitioned by region, with a category lookup and a
// charter (has-one) and tasks (has-many) nested under them. Tags are nested under the category, a category lookup under each task, and
// milestones joined on the project code.
func registerIncludes(routes includeRoutes) func(*router.Builder) {
	return func(b *router.Builder) {
		router.RegisterRoutes[IncProject](b, "/projects",
			router.WithPartition("region", "Region"),
			routes.project,
			func(b *router.Builder) {
				categoryOptions := append([]any{
					router.AsSingleRoute("CategoryID"),
					router.WithRelationName(routes.categoryRelationName),
					routes.category,
					func(b *router.Builder) {
						router.RegisterRoutes[IncTag](b, "/tags", routes.tag, router.WithRelationName("Tags"))
						groupOptions := append([]any{router.AsSingleRoute("GroupID"), router.WithRelationName("Group"), routes.group}, routes.groupOptions...)
						router.RegisterRoutes[IncGroup](b, "/group", groupOptions...)
					},
				}, routes.categoryOptions...)
				router.RegisterRoutes[IncCategory](b, "/category", categoryOptions...)
				router.RegisterRoutes[IncCharter](b, "/charters", routes.charter, router.WithRelationName("Charter"))
				router.RegisterRoutes[IncTask](b, "/tasks", routes.task, router.WithRelationName("Tasks"),
					func(b *router.Builder) {
						router.RegisterRoutes[IncCategory](b, "/category", router.AsSingleRoute("CategoryID"), router.WithRelationName("Category"), routes.taskCategory)
					},
				)
				router.RegisterRoutes[IncMilestone](b, "/milestones", routes.milestone, router.WithRelationName("Milestones"), router.WithJoinOn("ProjectCode", "Code"))
			},
		)
	}
}

type includedProject struct {
	Name     string `json:"name"`
	Category *struct {
		Name string `json:"name"`
		Tags []struct {
			Name string `json:"name"`
		} `json:"tags"`
		Group *struct {
			Name string `json:"name"`
		} `json:"group"`
	} `json:"category"`
	Charter *struct {
		Text string `json:"text"`
	} `json:"charter"`
	Tasks []struct {
		Title    string `json:"title"`
		Category *struct {
			Name string `json:"name"`
		} `json:"category"`
	} `json:"tasks"`
	Milestones []struct {
		Name string `json:"name"`
	} `json:"milestones"`
}

// listIncluded lists projects at path and returns them by name.
func listIncluded(t *testing.T, authInfo *router.AuthInfo, routes includeRoutes, path string) map[string]includedProject {
	t.Helper()
	w := hardRequest(t, scopedRouter(authInfo, registerIncludes(routes)), "GET", path, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s: expected 200, got %d: %s", path, w.Code, w.Body.String())
	}
	var list struct {
		Data []includedProject `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]includedProject, len(list.Data))
	for _, p := range list.Data {
		byName[p.Name] = p
	}
	return byName
}

func projectNamesOf(projects map[string]includedProject) []string {
	names := make([]string, 0, len(projects))
	for name := range projects {
		names = append(names, name)
	}
	return names
}

func categoryName(p includedProject) string {
	if p.Category == nil {
		return ""
	}
	return p.Category.Name
}

func emeaGrants(scopes ...string) *router.AuthInfo {
	authInfo := &router.AuthInfo{UserID: "u"}
	for _, scope := range scopes {
		authInfo.Grants = append(authInfo.Grants, grant(scope, "region", "emea"))
	}
	return authInfo
}

func TestJoinedChildInclude_LookupUnderPartitionedParent(t *testing.T) {
	seedIncludes(t)
	routes := defaultIncludeRoutes()

	t.Run("global scopes include every lookup", func(t *testing.T) {
		projects := listIncluded(t, &router.AuthInfo{UserID: "u", Scopes: []string{"project:read", "category:read"}}, routes, "/projects?include=Category")
		if len(projects) != 3 || categoryName(projects["alpha"]) != "cat-apac" || categoryName(projects["beta"]) != "cat-emea" || projects["gamma"].Category != nil {
			t.Errorf("got %+v", projects)
		}
	})

	t.Run("a lookup grant for the parent's regions does not remove the parent rows", func(t *testing.T) {
		projects := listIncluded(t, emeaGrants("project:read", "category:read"), routes, "/projects?include=Category")
		if len(projects) != 2 {
			t.Fatalf("expected alpha and gamma, got %v", projectNamesOf(projects))
		}
		if categoryName(projects["alpha"]) != "cat-apac" {
			t.Errorf("alpha: expected its category, reached through an EMEA project, got %+v", projects["alpha"].Category)
		}
	})

	t.Run("a narrower lookup grant leaves the lookup empty, not the row", func(t *testing.T) {
		authInfo := &router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{
			grant("project:read", "region", "emea"),
			grant("category:read", "region", "apac"),
		}}
		projects := listIncluded(t, authInfo, routes, "/projects?include=Category")
		if len(projects) != 2 {
			t.Fatalf("expected alpha and gamma, got %v", projectNamesOf(projects))
		}
		if projects["alpha"].Category != nil {
			t.Errorf("alpha: cat-apac is only referenced by an EMEA project outside the category grant, got %+v", projects["alpha"].Category)
		}
	})

	t.Run("a global lookup scope with a narrowed parent", func(t *testing.T) {
		authInfo := &router.AuthInfo{UserID: "u", Scopes: []string{"category:read"}, Grants: []router.ScopedGrant{grant("project:read", "region", "emea")}}
		projects := listIncluded(t, authInfo, routes, "/projects?include=Category")
		if len(projects) != 2 || categoryName(projects["alpha"]) != "cat-apac" {
			t.Errorf("got %+v", projects)
		}
	})

	t.Run("get applies the same rule", func(t *testing.T) {
		authInfo := &router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{
			grant("project:read", "region", "emea"),
			grant("category:read", "region", "apac"),
		}}
		w := hardRequest(t, scopedRouter(authInfo, registerIncludes(routes)), "GET", "/projects/1?include=Category", "")
		var p includedProject
		if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil || w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		if p.Name != "alpha" || p.Category != nil {
			t.Errorf("expected alpha without its category, got %+v", p)
		}
	})
}

func TestJoinedChildInclude_Shares(t *testing.T) {
	seedIncludes(t)
	sam := &router.AuthInfo{UserID: "sam", Grants: []router.ScopedGrant{grant("project:read", "region"), grant("category:read", "region")}}

	withShares := defaultIncludeRoutes()
	withShares.project.Share = projectShareConfig(nil)
	withShares.category.Share = projectShareConfig((*IncProject)(nil))
	projects := listIncluded(t, sam, withShares, "/projects?include=Category")
	if len(projects) != 1 || categoryName(projects["beta"]) != "cat-emea" {
		t.Errorf("lookup route accepting the share: expected beta with its category, got %+v", projects)
	}

	parentOnly := defaultIncludeRoutes()
	parentOnly.project.Share = projectShareConfig(nil)
	projects = listIncluded(t, sam, parentOnly, "/projects?include=Category")
	if len(projects) != 1 || projects["beta"].Category != nil {
		t.Errorf("lookup route without a share setting: expected beta without its category, got %+v", projects)
	}
}

func TestJoinedChildInclude_OwnershipOnSameNamedColumn(t *testing.T) {
	seedIncludes(t)
	routes := defaultIncludeRoutes()
	routes.project = readConfig("project:read")
	routes.category = router.AuthConfig{
		Methods:   []string{router.MethodGet, router.MethodList},
		Scopes:    []string{"category:read"},
		Ownership: &router.OwnershipConfig{Fields: []string{"OwnerID"}},
	}

	alice := &router.AuthInfo{UserID: "alice", Scopes: []string{"project:read", "category:read"}}
	projects := listIncluded(t, alice, routes, "/projects?include=Category")
	if len(projects) != 3 {
		t.Fatalf("category ownership must not narrow the projects, got %v", projectNamesOf(projects))
	}
	if projects["alpha"].Category != nil {
		t.Errorf("alpha is owned by alice but its category by bob: expected no category, got %+v", projects["alpha"].Category)
	}
	if categoryName(projects["beta"]) != "cat-emea" {
		t.Errorf("beta is owned by bob but its category by alice: expected the category, got %+v", projects["beta"].Category)
	}
}

func TestJoinedChildInclude_OwnPartitionOnSameNamedColumn(t *testing.T) {
	seedIncludes(t)
	routes := defaultIncludeRoutes()
	routes.categoryOptions = []any{router.WithPartition("region", "Region")}

	projects := listIncluded(t, emeaGrants("project:read", "category:read"), routes, "/projects?include=Category")
	if len(projects) != 2 {
		t.Fatalf("expected alpha and gamma, got %v", projectNamesOf(projects))
	}
	if projects["alpha"].Category != nil {
		t.Errorf("alpha is in EMEA but its category is in APAC: expected no category, got %+v", projects["alpha"].Category)
	}
}

func TestJoinedChildInclude_HasOne(t *testing.T) {
	seedIncludes(t)
	routes := defaultIncludeRoutes()

	projects := listIncluded(t, &router.AuthInfo{UserID: "u", Scopes: []string{"project:read", "charter:read"}}, routes, "/projects?include=Charter")
	if len(projects) != 3 || projects["alpha"].Charter == nil || projects["beta"].Charter == nil || projects["gamma"].Charter != nil {
		t.Errorf("global scopes: got %+v", projects)
	}

	projects = listIncluded(t, emeaGrants("project:read", "charter:read"), routes, "/projects?include=Charter")
	if len(projects) != 2 || projects["alpha"].Charter == nil || projects["alpha"].Charter.Text != "alpha-charter" {
		t.Errorf("EMEA grants: expected alpha with its charter and gamma, got %+v", projects)
	}

	authInfo := &router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{grant("project:read", "region", "emea"), grant("charter:read", "region", "apac")}}
	projects = listIncluded(t, authInfo, routes, "/projects?include=Charter")
	if len(projects) != 2 || projects["alpha"].Charter != nil {
		t.Errorf("narrower charter grant: expected alpha without its charter and gamma, got %+v", projects)
	}

	owned := defaultIncludeRoutes()
	owned.charter.Ownership = &router.OwnershipConfig{Fields: []string{"OwnerID"}}
	alice := &router.AuthInfo{UserID: "alice", Scopes: []string{"project:read", "charter:read"}}
	projects = listIncluded(t, alice, owned, "/projects?include=Charter")
	if len(projects) != 3 || projects["alpha"].Charter != nil || projects["beta"].Charter == nil {
		t.Errorf("charter ownership on a same-named column: expected only beta's charter, got %+v", projects)
	}
}

func TestJoinedChildInclude_NestedPaths(t *testing.T) {
	seedIncludes(t)
	routes := defaultIncludeRoutes()

	t.Run("has-many then lookup", func(t *testing.T) {
		projects := listIncluded(t, emeaGrants("project:read", "task:read", "category:read"), routes, "/projects?include=Tasks.Category")
		alpha := projects["alpha"]
		if len(projects) != 2 || len(alpha.Tasks) != 1 || alpha.Tasks[0].Category == nil || alpha.Tasks[0].Category.Name != "cat-emea" {
			t.Errorf("expected alpha-task with its category, got %+v", projects)
		}

		authInfo := &router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{
			grant("project:read", "region", "emea"),
			grant("task:read", "region", "emea"),
			grant("category:read", "region", "apac"),
		}}
		projects = listIncluded(t, authInfo, routes, "/projects?include=Tasks.Category")
		alpha = projects["alpha"]
		if len(alpha.Tasks) != 1 || alpha.Tasks[0].Category != nil {
			t.Errorf("narrower category grant: expected alpha-task without its category, got %+v", alpha)
		}
	})

	t.Run("lookup then has-many", func(t *testing.T) {
		projects := listIncluded(t, emeaGrants("project:read", "category:read", "tag:read"), routes, "/projects?include=Category.Tags")
		alpha := projects["alpha"]
		if len(projects) != 2 || alpha.Category == nil || len(alpha.Category.Tags) != 1 || alpha.Category.Tags[0].Name != "apac-tag" {
			t.Errorf("expected alpha's category with its tag, got %+v", projects)
		}

		authInfo := &router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{
			grant("project:read", "region", "emea"),
			grant("category:read", "region", "emea"),
			grant("tag:read", "region", "apac"),
		}}
		projects = listIncluded(t, authInfo, routes, "/projects?include=Category.Tags")
		alpha = projects["alpha"]
		if alpha.Category == nil || len(alpha.Category.Tags) != 0 {
			t.Errorf("narrower tag grant: expected alpha's category without tags, got %+v", alpha)
		}
	})

	t.Run("lookup then lookup", func(t *testing.T) {
		projects := listIncluded(t, emeaGrants("project:read", "category:read", "group:read"), routes, "/projects?include=Category.Group")
		alpha := projects["alpha"]
		if len(projects) != 2 || alpha.Category == nil || alpha.Category.Group == nil || alpha.Category.Group.Name != "group-apac" {
			t.Errorf("expected alpha's category with its group, got %+v", alpha)
		}

		authInfo := &router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{
			grant("project:read", "region", "emea"),
			grant("category:read", "region", "emea"),
			grant("group:read", "region", "apac"),
		}}
		projects = listIncluded(t, authInfo, routes, "/projects?include=Category.Group")
		alpha = projects["alpha"]
		if len(projects) != 2 || alpha.Category == nil || alpha.Category.Group != nil {
			t.Errorf("narrower group grant: expected alpha's category without its group, got %+v", alpha)
		}

		ownPartition := defaultIncludeRoutes()
		ownPartition.groupOptions = []any{router.WithPartition("region", "Region")}
		projects = listIncluded(t, emeaGrants("project:read", "category:read", "group:read"), ownPartition, "/projects?include=Category.Group")
		alpha = projects["alpha"]
		if alpha.Category == nil || alpha.Category.Group != nil {
			t.Errorf("group in APAC under an EMEA grant, on a column named like the category's: expected no group, got %+v", alpha.Category)
		}
	})

	t.Run("owned has-many children under an unowned parent", func(t *testing.T) {
		owned := defaultIncludeRoutes()
		owned.task.Ownership = &router.OwnershipConfig{Fields: []string{"OwnerID"}}
		alice := &router.AuthInfo{UserID: "alice", Scopes: []string{"project:read", "task:read"}}
		projects := listIncluded(t, alice, owned, "/projects?include=Tasks")
		if len(projects["alpha"].Tasks) != 0 || len(projects["beta"].Tasks) != 1 {
			t.Errorf("expected only beta-task, owned by alice, got alpha %+v beta %+v", projects["alpha"].Tasks, projects["beta"].Tasks)
		}
	})

	t.Run("has-many includes are unchanged", func(t *testing.T) {
		projects := listIncluded(t, emeaGrants("project:read", "task:read"), routes, "/projects?include=Tasks")
		if len(projects) != 2 || len(projects["alpha"].Tasks) != 1 || len(projects["gamma"].Tasks) != 0 {
			t.Errorf("got %+v", projects)
		}
	})
}

func TestJoinedChildInclude_JoinOnChild(t *testing.T) {
	seedIncludes(t)
	routes := defaultIncludeRoutes()

	projects := listIncluded(t, emeaGrants("project:read", "milestone:read"), routes, "/projects?include=Milestones")
	if len(projects) != 2 || len(projects["alpha"].Milestones) != 1 || projects["alpha"].Milestones[0].Name != "alpha-milestone" {
		t.Errorf("EMEA grants: expected alpha with its milestone, got %+v", projects)
	}

	authInfo := &router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{grant("project:read", "region", "emea"), grant("milestone:read", "region", "apac")}}
	projects = listIncluded(t, authInfo, routes, "/projects?include=Milestones")
	if len(projects) != 2 || len(projects["alpha"].Milestones) != 0 {
		t.Errorf("narrower milestone grant: expected alpha without milestones, got %+v", projects)
	}
}

func TestJoinedChildInclude_RelationNameNotMatchingTheModel(t *testing.T) {
	seedIncludes(t)
	routes := defaultIncludeRoutes()
	routes.categoryRelationName = "category"

	authInfo := &router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{grant("project:read", "region", "emea"), grant("category:read", "region", "apac")}}
	w := hardRequest(t, scopedRouter(authInfo, registerIncludes(routes)), "GET", "/projects?include=category", "")
	if w.Code != http.StatusOK {
		return
	}
	var list struct {
		Data []includedProject `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	for _, p := range list.Data {
		if p.Category != nil {
			t.Errorf("%s: a category outside the grant was included: %+v", p.Name, p.Category)
		}
	}
}

// relationCounts lists projects at path and returns each project's counts by name.
func relationCounts(t *testing.T, r http.Handler, path string) map[string]map[string]int {
	t.Helper()
	w := hardRequest(t, r, "GET", path, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s: expected 200, got %d: %s", path, w.Code, w.Body.String())
	}
	var list struct {
		Data []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
		Counts map[string]map[string]int `json:"counts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	counts := make(map[string]map[string]int, len(list.Data))
	for _, p := range list.Data {
		counts[p.Name] = map[string]int{}
		for relation, byID := range list.Counts {
			counts[p.Name][relation] = byID[strconv.Itoa(p.ID)]
		}
	}
	return counts
}

func TestJoinedChildInclude_CountsAndFilters(t *testing.T) {
	seedIncludes(t)
	routes := defaultIncludeRoutes()
	matching := scopedRouter(emeaGrants("project:read", "category:read", "tag:read"), registerIncludes(routes))
	narrower := scopedRouter(&router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{grant("project:read", "region", "emea"), grant("category:read", "region", "apac")}}, registerIncludes(routes))

	t.Run("include counts of a lookup", func(t *testing.T) {
		counts := relationCounts(t, matching, "/projects?include_count=Category")
		if len(counts) != 2 || counts["alpha"]["Category"] != 1 || counts["gamma"]["Category"] != 0 {
			t.Errorf("matching grant: expected alpha 1 and gamma 0, got %+v", counts)
		}
		counts = relationCounts(t, narrower, "/projects?include_count=Category")
		if len(counts) != 2 || counts["alpha"]["Category"] != 0 {
			t.Errorf("narrower grant: expected no category counted, got %+v", counts)
		}
	})

	t.Run("include counts through a lookup", func(t *testing.T) {
		counts := relationCounts(t, matching, "/projects?include_count=Category.Tags")
		if counts["alpha"]["Category.Tags"] != 1 || counts["gamma"]["Category.Tags"] != 0 {
			t.Errorf("expected alpha 1 and gamma 0, got %+v", counts)
		}
	})

	t.Run("owned lookup counts", func(t *testing.T) {
		owned := defaultIncludeRoutes()
		owned.category.Ownership = &router.OwnershipConfig{Fields: []string{"OwnerID"}}
		alice := scopedRouter(&router.AuthInfo{UserID: "alice", Scopes: []string{"project:read", "category:read"}}, registerIncludes(owned))
		counts := relationCounts(t, alice, "/projects?include_count=Category")
		if counts["alpha"]["Category"] != 0 || counts["beta"]["Category"] != 1 {
			t.Errorf("expected only beta's category, owned by alice, counted, got %+v", counts)
		}
	})

	t.Run("has-many counts are unchanged", func(t *testing.T) {
		r := scopedRouter(emeaGrants("project:read", "task:read"), registerIncludes(routes))
		counts := relationCounts(t, r, "/projects?include_count=Tasks")
		if len(counts) != 2 || counts["alpha"]["Tasks"] != 1 || counts["gamma"]["Tasks"] != 0 {
			t.Errorf("expected alpha 1 and gamma 0, got %+v", counts)
		}
	})

	t.Run("exists filters on a lookup", func(t *testing.T) {
		projects := listIncluded(t, emeaGrants("project:read", "category:read"), routes, "/projects?filter[Category][exists]=true")
		if len(projects) != 1 || projects["alpha"].Name != "alpha" {
			t.Errorf("matching grant: expected alpha, got %v", projectNamesOf(projects))
		}
		projects = listIncluded(t, emeaGrants("project:read", "category:read"), routes, "/projects?filter[Category][exists]=false")
		if len(projects) != 1 || projects["gamma"].Name != "gamma" {
			t.Errorf("matching grant, not exists: expected gamma, got %v", projectNamesOf(projects))
		}

		authInfo := &router.AuthInfo{UserID: "u", Grants: []router.ScopedGrant{grant("project:read", "region", "emea"), grant("category:read", "region", "apac")}}
		projects = listIncluded(t, authInfo, routes, "/projects?filter[Category][exists]=true")
		if len(projects) != 0 {
			t.Errorf("narrower grant: no category is visible, got %v", projectNamesOf(projects))
		}
	})

	t.Run("count filters on a lookup", func(t *testing.T) {
		projects := listIncluded(t, emeaGrants("project:read", "category:read"), routes, "/projects?filter[Category][count_gte]=1")
		if len(projects) != 1 || projects["alpha"].Name != "alpha" {
			t.Errorf("expected alpha, got %v", projectNamesOf(projects))
		}
		projects = listIncluded(t, emeaGrants("project:read", "category:read", "tag:read"), routes, "/projects?filter[Category.Tags][count_gte]=1")
		if len(projects) != 1 || projects["alpha"].Name != "alpha" {
			t.Errorf("through a lookup: expected alpha, got %v", projectNamesOf(projects))
		}
	})
}
