package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/router"
	"github.com/sjgoldie/go-restgen/service"
)

type MXAccount struct {
	bun.BaseModel `bun:"table:mx_accounts"`
	ID            int    `bun:"id,pk,autoincrement" json:"id"`
	Plan          string `bun:"plan" json:"plan"`
}

// MXUser is keyed by the auth user ID used by AsCurrentUser; Ext holds the one used by
// AsCurrentUserExternal.
type MXUser struct {
	bun.BaseModel `bun:"table:mx_users"`
	ID            string     `bun:"id,pk" json:"id"`
	Ext           string     `bun:"ext,unique" json:"ext"`
	Name          string     `bun:"name" json:"name"`
	AccountID     int        `bun:"account_id,nullzero" json:"account_id,omitempty"`
	Account       *MXAccount `bun:"rel:belongs-to,join:account_id=id" json:"account,omitempty"`
	Tasks         []*MXTask  `bun:"rel:has-many,join:id=user_id" json:"tasks,omitempty"`
}

type MXTask struct {
	bun.BaseModel `bun:"table:mx_tasks"`
	ID            int     `bun:"id,pk,autoincrement" json:"id"`
	UserID        string  `bun:"user_id" json:"user_id"`
	User          *MXUser `bun:"rel:belongs-to,join:user_id=id" json:"user,omitempty"`
	Title         string  `bun:"title" json:"title"`
}

type MXPost struct {
	bun.BaseModel `bun:"table:mx_posts"`
	ID            int     `bun:"id,pk,autoincrement" json:"id"`
	AuthorID      string  `bun:"author_id" json:"author_id"`
	Author        *MXUser `bun:"rel:belongs-to,join:author_id=id" json:"author,omitempty"`
}

type mxFixture struct {
	aliceAccount, bobAccount int
	aliceTask, bobTask       int
	alicePost                int
}

// seedMatrix creates users alice and bob, each with an account, a task and a post. A dummy
// account and task come first, so no account, task, or post ID equals another kind's ID.
func seedMatrix(t *testing.T) mxFixture {
	t.Helper()
	db := hardTables(t, (*MXAccount)(nil), (*MXUser)(nil), (*MXTask)(nil), (*MXPost)(nil))
	ctx := context.Background()

	accounts := []MXAccount{{Plan: "dummy"}, {Plan: "alice-plan"}, {Plan: "bob-plan"}}
	if _, err := db.NewInsert().Model(&accounts).Returning("*").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	users := []MXUser{
		{ID: "alice", Ext: "ext-alice", Name: "Alice", AccountID: accounts[1].ID},
		{ID: "bob", Ext: "ext-bob", Name: "Bob", AccountID: accounts[2].ID},
	}
	if _, err := db.NewInsert().Model(&users).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	tasks := []MXTask{{UserID: "nobody", Title: "dummy"}, {UserID: "alice", Title: "alice-task"}, {UserID: "bob", Title: "bob-task"}}
	if _, err := db.NewInsert().Model(&tasks).Returning("*").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	posts := []MXPost{{AuthorID: "bob"}, {AuthorID: "bob"}, {AuthorID: "alice"}}
	if _, err := db.NewInsert().Model(&posts).Returning("*").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	return mxFixture{
		aliceAccount: accounts[1].ID, bobAccount: accounts[2].ID,
		aliceTask: tasks[1].ID, bobTask: tasks[2].ID,
		alicePost: posts[2].ID,
	}
}

func touchTask(ctx context.Context, svc *service.Common[MXTask], _ *metadata.TypeMetadata, _ *metadata.AuthInfo, id string, item *MXTask, _ []byte) (*MXTask, error) {
	item.Title += " touched"
	return svc.Update(ctx, id, *item)
}

// matrixChildren registers a user's tasks (a collection) and account (a single route).
func matrixChildren(b *router.Builder) {
	router.RegisterRoutes[MXTask](b, "/tasks",
		router.AllScopedWithBatch(router.ScopeAuthOnly),
		router.WithRelationName("Tasks"),
		router.WithAction("touch", touchTask, router.IsAuthenticated()),
	)
	router.RegisterRoutes[MXAccount](b, "/account",
		router.AsSingleRouteWithUpdate("AccountID"),
		router.IsAuthenticated(),
		router.WithRelationName("Account"),
	)
}

// parentKind is one way of registering the user that the tasks and account are nested under.
type parentKind struct {
	name     string
	caller   string
	base     func(f mxFixture) string
	register func(b *router.Builder)
}

func parentKinds() []parentKind {
	userRoute := []any{router.IsAuthenticated(), router.WithFilters("Name"), matrixChildren}
	return []parentKind{
		{
			name:   "URL ID",
			caller: "alice",
			base:   func(mxFixture) string { return "/users/alice" },
			register: func(b *router.Builder) {
				router.RegisterRoutes[MXUser](b, "/users", userRoute...)
			},
		},
		{
			name:   "AsCurrentUser",
			caller: "alice",
			base:   func(mxFixture) string { return "/me" },
			register: func(b *router.Builder) {
				router.RegisterRoutes[MXUser](b, "/me", append([]any{router.AsCurrentUser()}, userRoute...)...)
			},
		},
		{
			name:   "AsCurrentUserExternal",
			caller: "ext-alice",
			base:   func(mxFixture) string { return "/me" },
			register: func(b *router.Builder) {
				router.RegisterRoutes[MXUser](b, "/me", append([]any{router.AsCurrentUserExternal("Ext")}, userRoute...)...)
			},
		},
		{
			name:   "nested single route",
			caller: "alice",
			base:   func(f mxFixture) string { return "/posts/" + strconv.Itoa(f.alicePost) + "/author" },
			register: func(b *router.Builder) {
				router.RegisterRoutes[MXPost](b, "/posts", router.IsAuthenticated(), func(b *router.Builder) {
					router.RegisterRoutes[MXUser](b, "/author", append([]any{router.AsSingleRouteWithUpdate("AuthorID"), router.WithRelationName("Author")}, userRoute...)...)
				})
			},
		},
	}
}

func decodeInto(t *testing.T, label string, code int, body []byte, wantCode int, v any) bool {
	t.Helper()
	if code != wantCode {
		t.Errorf("%s: expected %d, got %d: %s", label, wantCode, code, body)
		return false
	}
	if v != nil {
		if err := json.Unmarshal(body, v); err != nil {
			t.Errorf("%s: decode: %v: %s", label, err, body)
			return false
		}
	}
	return true
}

// TestParentKindMatrix runs every child operation under every kind of parent, as the caller
// alice: each must reach alice's rows and never bob's.
func TestParentKindMatrix(t *testing.T) {
	for _, kind := range parentKinds() {
		t.Run(kind.name, func(t *testing.T) {
			f := seedMatrix(t)
			r := scopedRouter(&router.AuthInfo{UserID: kind.caller}, kind.register)
			base := kind.base(f)
			do := func(method, path, body string) (int, []byte) {
				w := hardRequest(t, r, method, base+path, body)
				return w.Code, w.Body.Bytes()
			}
			task := func(id int) string { return "/tasks/" + strconv.Itoa(id) }

			var user MXUser
			if code, body := do("GET", "?include=Tasks,Account", ""); decodeInto(t, "get parent with includes", code, body, http.StatusOK, &user) {
				if user.ID != "alice" || len(user.Tasks) != 1 || user.Tasks[0].Title != "alice-task" || user.Account == nil || user.Account.ID != f.aliceAccount {
					t.Errorf("get parent with includes: got %+v", user)
				}
			}

			var list struct {
				Data []MXTask `json:"data"`
			}
			if code, body := do("GET", "/tasks", ""); decodeInto(t, "list tasks", code, body, http.StatusOK, &list) {
				if len(list.Data) != 1 || list.Data[0].Title != "alice-task" {
					t.Errorf("list tasks: got %+v", list.Data)
				}
			}
			if code, body := do("GET", "/tasks?filter[User.Name]=Alice", ""); decodeInto(t, "parent field filter", code, body, http.StatusOK, &list) && len(list.Data) != 1 {
				t.Errorf("parent field filter matching alice: got %+v", list.Data)
			}
			if code, body := do("GET", "/tasks?filter[User.Name]=Bob", ""); decodeInto(t, "parent field filter", code, body, http.StatusOK, &list) && len(list.Data) != 0 {
				t.Errorf("parent field filter matching bob: got %+v", list.Data)
			}
			if code, body := do("GET", "/tasks?include=User", ""); decodeInto(t, "parent include", code, body, http.StatusOK, &list) {
				if len(list.Data) != 1 || list.Data[0].User == nil || list.Data[0].User.ID != "alice" {
					t.Errorf("parent include: got %+v", list.Data)
				}
			}

			if code, body := do("GET", task(f.aliceTask), ""); code != http.StatusOK {
				t.Errorf("get own task: expected 200, got %d: %s", code, body)
			}
			for _, method := range []string{"GET", "PATCH", "PUT", "DELETE"} {
				if code, _ := do(method, task(f.bobTask), `{"title":"taken"}`); code != http.StatusNotFound {
					t.Errorf("%s another user's task: expected 404, got %d", method, code)
				}
			}
			if code, _ := do("POST", task(f.bobTask)+"/touch", ""); code != http.StatusNotFound {
				t.Errorf("action on another user's task: expected 404, got %d", code)
			}

			var created MXTask
			if code, body := do("POST", "/tasks", `{"user_id":"bob","title":"new"}`); decodeInto(t, "create task", code, body, http.StatusCreated, &created) && created.UserID != "alice" {
				t.Errorf("create task: belongs to %q", created.UserID)
			}
			var batch struct {
				Data []MXTask `json:"data"`
			}
			if code, body := do("POST", "/tasks/batch", `[{"user_id":"bob","title":"batch"}]`); decodeInto(t, "batch create", code, body, http.StatusCreated, &batch) {
				if len(batch.Data) != 1 || batch.Data[0].UserID != "alice" {
					t.Errorf("batch create: got %+v", batch.Data)
				}
			}
			var updated MXTask
			if code, body := do("PATCH", task(f.aliceTask), `{"user_id":"bob","title":"patched"}`); decodeInto(t, "patch own task", code, body, http.StatusOK, &updated) && updated.UserID != "alice" {
				t.Errorf("patch own task: moved to %q", updated.UserID)
			}
			if code, body := do("PUT", task(f.aliceTask), `{"title":"put"}`); decodeInto(t, "put own task", code, body, http.StatusOK, &updated) && updated.UserID != "alice" {
				t.Errorf("put own task without its parent key: left under %q", updated.UserID)
			}
			if code, body := do("PUT", "/tasks/batch", `[{"id":`+strconv.Itoa(f.aliceTask)+`,"user_id":"bob","title":"batch put"}]`); decodeInto(t, "batch update", code, body, http.StatusOK, &batch) {
				if len(batch.Data) != 1 || batch.Data[0].UserID != "alice" {
					t.Errorf("batch update: got %+v", batch.Data)
				}
			}
			var stored MXTask
			if err := hardTables(t).NewSelect().Model(&stored).Where("id = ?", f.aliceTask).Scan(context.Background()); err != nil || stored.UserID != "alice" {
				t.Errorf("alice's task must stay hers: %+v, %v", stored, err)
			}
			if code, body := do("POST", task(f.aliceTask)+"/touch", ""); code != http.StatusOK || !strings.Contains(string(body), "touched") {
				t.Errorf("action on own task: expected 200, got %d: %s", code, body)
			}
			if code, body := do("DELETE", task(created.ID), ""); code != http.StatusNoContent {
				t.Errorf("delete own task: expected 204, got %d: %s", code, body)
			}

			var account MXAccount
			if code, body := do("GET", "/account", ""); decodeInto(t, "get account", code, body, http.StatusOK, &account) && account.ID != f.aliceAccount {
				t.Errorf("get account: got %+v", account)
			}
			if code, body := do("PATCH", "/account", `{"id":`+strconv.Itoa(f.bobAccount)+`,"plan":"patched"}`); decodeInto(t, "patch account", code, body, http.StatusOK, &account) && account.ID != f.aliceAccount {
				t.Errorf("patch account: updated %+v", account)
			}
			if code, body := do("PUT", "/account", `{"plan":"put"}`); decodeInto(t, "put account", code, body, http.StatusOK, &account) && account.ID != f.aliceAccount {
				t.Errorf("put account: updated %+v", account)
			}
			var bob MXAccount
			if err := hardTables(t).NewSelect().Model(&bob).Where("id = ?", f.bobAccount).Scan(context.Background()); err != nil || bob.Plan != "bob-plan" {
				t.Errorf("bob's account must be untouched: %+v, %v", bob, err)
			}
		})
	}
}
