package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/router"
	"github.com/sjgoldie/go-restgen/service"
)

type CUBusinessUnit struct {
	bun.BaseModel `bun:"table:cu_business_units"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	Name          string    `bun:"name" json:"name"`
	Users         []*CUUser `bun:"rel:has-many,join:id=business_unit_id" json:"users,omitempty"`
}

// CUUser is keyed by the auth user ID.
type CUUser struct {
	bun.BaseModel  `bun:"table:cu_users"`
	ID             string          `bun:"id,pk" json:"id"`
	BusinessUnitID int             `bun:"business_unit_id,nullzero" json:"business_unit_id,omitempty"`
	BusinessUnit   *CUBusinessUnit `bun:"rel:belongs-to,join:business_unit_id=id" json:"business_unit,omitempty"`
	Name           string          `bun:"name" json:"name"`
	Tasks          []*CUTask       `bun:"rel:has-many,join:id=user_id" json:"tasks,omitempty"`
}

type CUTask struct {
	bun.BaseModel `bun:"table:cu_tasks"`
	ID            int     `bun:"id,pk,autoincrement" json:"id"`
	UserID        string  `bun:"user_id" json:"user_id"`
	User          *CUUser `bun:"rel:belongs-to,join:user_id=id" json:"user,omitempty"`
	Title         string  `bun:"title" json:"title"`
}

type cuFixture struct {
	sales, support int
	bobTask        int
}

// seedCurrentUsers creates business units sales and support, users alice (sales), bob (support)
// and carol (no business unit), and one task each for alice and bob.
func seedCurrentUsers(t *testing.T) cuFixture {
	t.Helper()
	db := hardTables(t, (*CUBusinessUnit)(nil), (*CUUser)(nil), (*CUTask)(nil))
	ctx := context.Background()

	units := []CUBusinessUnit{{Name: "sales"}, {Name: "support"}}
	if _, err := db.NewInsert().Model(&units).Returning("*").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	users := []CUUser{
		{ID: "alice", BusinessUnitID: units[0].ID, Name: "Alice"},
		{ID: "bob", BusinessUnitID: units[1].ID, Name: "Bob"},
		{ID: "carol", Name: "Carol"},
	}
	if _, err := db.NewInsert().Model(&users).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	tasks := []CUTask{{UserID: "alice", Title: "alice-task"}, {UserID: "bob", Title: "bob-task"}}
	if _, err := db.NewInsert().Model(&tasks).Returning("*").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	return cuFixture{sales: units[0].ID, support: units[1].ID, bobTask: tasks[1].ID}
}

func renameAction(ctx context.Context, svc *service.Common[CUUser], _ *metadata.TypeMetadata, _ *metadata.AuthInfo, id string, item *CUUser, payload []byte) (*CUUser, error) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return nil, err
	}
	item.Name = body.Name
	return svc.Update(ctx, id, *item)
}

// registerMe registers the caller's own user at /me with GET and PATCH, a rename action, and
// their tasks nested under it.
func registerMe(b *router.Builder) {
	router.RegisterRoutes[CUUser](b, "/me",
		router.AsCurrentUser(),
		router.AuthConfig{Methods: []string{router.MethodGet, router.MethodPatch}, Scopes: []string{router.ScopeAuthOnly}},
		router.WithAction("rename", renameAction, router.IsAuthenticated()),
		func(b *router.Builder) {
			router.RegisterRoutes[CUTask](b, "/tasks", router.IsAuthenticated(), router.WithRelationName("Tasks"))
		},
	)
}

func asCaller(userID string) *router.AuthInfo {
	return &router.AuthInfo{UserID: userID}
}

func decodeUser(t *testing.T, body []byte) CUUser {
	t.Helper()
	var u CUUser
	if err := json.Unmarshal(body, &u); err != nil {
		t.Fatalf("decode user: %v: %s", err, body)
	}
	return u
}

func storedUserName(t *testing.T, id string) string {
	t.Helper()
	var u CUUser
	if err := hardTables(t).NewSelect().Model(&u).Where("id = ?", id).Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	return u.Name
}

func TestCurrentUser_Get(t *testing.T) {
	seedCurrentUsers(t)

	for _, user := range []string{"alice", "bob"} {
		w := hardRequest(t, scopedRouter(asCaller(user), registerMe), "GET", "/me", "")
		if w.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d: %s", user, w.Code, w.Body.String())
		}
		if got := decodeUser(t, w.Body.Bytes()); got.ID != user {
			t.Errorf("%s: got user %q", user, got.ID)
		}
	}

	if w := hardRequest(t, scopedRouter(nil, registerMe), "GET", "/me", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("no auth: expected 401, got %d", w.Code)
	}
	if w := hardRequest(t, scopedRouter(&router.AuthInfo{Scopes: []string{"admin"}}, registerMe), "GET", "/me", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("auth without a user ID: expected 401, got %d", w.Code)
	}
	for _, method := range []string{"GET", "PATCH", "DELETE"} {
		if w := hardRequest(t, scopedRouter(asCaller("alice"), registerMe), method, "/me/bob", `{"name":"x"}`); w.Code != http.StatusNotFound {
			t.Errorf("%s /me/bob: expected 404, got %d: %s", method, w.Code, w.Body.String())
		}
	}
	if storedUserName(t, "bob") != "Bob" {
		t.Errorf("/me/bob must not reach bob's row")
	}
	if w := hardRequest(t, scopedRouter(asCaller("dave"), registerMe), "GET", "/me", ""); w.Code != http.StatusNotFound {
		t.Errorf("no row for the caller: expected 404, got %d", w.Code)
	}
	if w := hardRequest(t, scopedRouter(asCaller("alice"), registerMe), "GET", "/me?include=Tasks", ""); w.Code != http.StatusOK || len(decodeUser(t, w.Body.Bytes()).Tasks) != 1 {
		t.Errorf("include: expected alice with her task, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCurrentUser_Writes(t *testing.T) {
	seedCurrentUsers(t)
	alice := scopedRouter(asCaller("alice"), registerMe)

	w := hardRequest(t, alice, "PATCH", "/me", `{"id":"bob","name":"Alice Two"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("patch: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if storedUserName(t, "alice") != "Alice Two" || storedUserName(t, "bob") != "Bob" {
		t.Errorf("patch must change only the caller's row: alice %q, bob %q", storedUserName(t, "alice"), storedUserName(t, "bob"))
	}

	if w := hardRequest(t, alice, "POST", "/me/rename", `{"name":"Alice Three"}`); w.Code != http.StatusOK || storedUserName(t, "alice") != "Alice Three" {
		t.Errorf("action: expected alice renamed, got %d: %s", w.Code, w.Body.String())
	}

	if w := hardRequest(t, alice, "PUT", "/me", `{"name":"x"}`); w.Code != http.StatusUnauthorized {
		t.Errorf("method without an auth config: expected 401, got %d", w.Code)
	}
	if w := hardRequest(t, alice, "DELETE", "/me", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("delete without an auth config: expected 401, got %d", w.Code)
	}
	if w := hardRequest(t, alice, "POST", "/me", `{"id":"eve","name":"Eve"}`); w.Code == http.StatusCreated {
		t.Errorf("collection routes are not mounted: create returned 201")
	}

	withDelete := func(b *router.Builder) {
		router.RegisterRoutes[CUUser](b, "/me", router.AsCurrentUser(), router.IsAuthenticated())
	}
	if w := hardRequest(t, scopedRouter(asCaller("carol"), withDelete), "DELETE", "/me", ""); w.Code != http.StatusNoContent {
		t.Fatalf("configured delete: expected 204, got %d: %s", w.Code, w.Body.String())
	}
	count, err := hardTables(t).NewSelect().Model((*CUUser)(nil)).Count(context.Background())
	if err != nil || count != 2 {
		t.Errorf("delete must remove only the caller's row: %d users left, %v", count, err)
	}
}

func TestCurrentUser_Children(t *testing.T) {
	f := seedCurrentUsers(t)
	alice := scopedRouter(asCaller("alice"), registerMe)

	var list struct {
		Data []CUTask `json:"data"`
	}
	w := hardRequest(t, alice, "GET", "/me/tasks", "")
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || w.Code != http.StatusOK {
		t.Fatalf("list: %d: %s", w.Code, w.Body.String())
	}
	if len(list.Data) != 1 || list.Data[0].Title != "alice-task" {
		t.Errorf("expected only alice's task, got %+v", list.Data)
	}

	if w := hardRequest(t, alice, "GET", "/me/tasks/"+strconv.Itoa(f.bobTask), ""); w.Code != http.StatusNotFound {
		t.Errorf("another user's task: expected 404, got %d", w.Code)
	}
	if w := hardRequest(t, alice, "PATCH", "/me/tasks/"+strconv.Itoa(f.bobTask), `{"title":"taken"}`); w.Code != http.StatusNotFound {
		t.Errorf("updating another user's task: expected 404, got %d", w.Code)
	}

	w = hardRequest(t, alice, "POST", "/me/tasks", `{"user_id":"bob","title":"new"}`)
	var created CUTask
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || w.Code != http.StatusCreated {
		t.Fatalf("create: %d: %s", w.Code, w.Body.String())
	}
	if created.UserID != "alice" {
		t.Errorf("a created task belongs to the caller, got %q", created.UserID)
	}

	if w := hardRequest(t, scopedRouter(nil, registerMe), "GET", "/me/tasks", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("children without auth: expected 401, got %d", w.Code)
	}
	if w := hardRequest(t, scopedRouter(asCaller("dave"), registerMe), "GET", "/me/tasks", ""); w.Code != http.StatusOK || w.Body.String() == "" {
		t.Errorf("caller with no row: expected an empty list, got %d", w.Code)
	} else if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Data) != 0 {
		t.Errorf("caller with no row: expected no tasks, got %+v", list.Data)
	}
}

func TestCurrentUser_NestedUnderParent(t *testing.T) {
	f := seedCurrentUsers(t)
	register := func(b *router.Builder) {
		router.RegisterRoutes[CUBusinessUnit](b, "/businessunits",
			router.IsAuthenticated(),
			func(b *router.Builder) {
				router.RegisterRoutes[CUUser](b, "/user",
					router.AsCurrentUser(),
					router.IsAuthenticated(),
					func(b *router.Builder) {
						router.RegisterRoutes[CUTask](b, "/tasks", router.IsAuthenticated(), router.WithRelationName("Tasks"))
					},
				)
			},
		)
	}
	alice := scopedRouter(asCaller("alice"), register)

	w := hardRequest(t, alice, "GET", "/businessunits/"+strconv.Itoa(f.sales)+"/user", "")
	if w.Code != http.StatusOK || decodeUser(t, w.Body.Bytes()).ID != "alice" {
		t.Errorf("own business unit: expected alice, got %d: %s", w.Code, w.Body.String())
	}
	if w := hardRequest(t, alice, "GET", "/businessunits/"+strconv.Itoa(f.support)+"/user", ""); w.Code != http.StatusNotFound {
		t.Errorf("another business unit: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	var list struct {
		Data []CUTask `json:"data"`
	}
	w = hardRequest(t, alice, "GET", "/businessunits/"+strconv.Itoa(f.sales)+"/user/tasks", "")
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Data) != 1 {
		t.Errorf("own business unit tasks: expected alice's task, got %d: %s", w.Code, w.Body.String())
	}
	w = hardRequest(t, alice, "GET", "/businessunits/"+strconv.Itoa(f.support)+"/user/tasks", "")
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Data) != 0 {
		t.Errorf("another business unit's tasks: expected none, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSingleRoute_UnusableRoutesAreNotRegistered(t *testing.T) {
	seedCurrentUsers(t)
	children := func(b *router.Builder) {
		router.RegisterRoutes[CUTask](b, "/tasks", router.IsAuthenticated(), router.WithRelationName("Tasks"))
	}

	cases := map[string]func(*router.Builder){
		"top level without a parent field": func(b *router.Builder) {
			router.RegisterRoutes[CUUser](b, "/me", router.AsSingleRoute(""), router.IsAuthenticated(), children)
		},
		"top level with a parent field": func(b *router.Builder) {
			router.RegisterRoutes[CUUser](b, "/me", router.AsSingleRouteWithUpdate("ID"), router.IsAuthenticated(), children)
		},
		"combined with AsCurrentUser": func(b *router.Builder) {
			router.RegisterRoutes[CUUser](b, "/me", router.AsCurrentUser(), router.AsSingleRoute("ID"), router.IsAuthenticated(), children)
		},
	}
	for name, register := range cases {
		t.Run(name, func(t *testing.T) {
			r := scopedRouter(asCaller("alice"), register)
			for _, path := range []string{"/me", "/me/tasks"} {
				if w := hardRequest(t, r, "GET", path, ""); w.Code != http.StatusNotFound {
					t.Errorf("GET %s: expected 404, got %d: %s", path, w.Code, w.Body.String())
				}
			}
		})
	}

	t.Run("nested without a parent field", func(t *testing.T) {
		r := scopedRouter(asCaller("alice"), func(b *router.Builder) {
			router.RegisterRoutes[CUTask](b, "/tasks", router.IsAuthenticated(), func(b *router.Builder) {
				router.RegisterRoutes[CUUser](b, "/user", router.AsSingleRoute(""), router.IsAuthenticated())
			})
		})
		if w := hardRequest(t, r, "GET", "/tasks/1/user", ""); w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
		}
	})
}
