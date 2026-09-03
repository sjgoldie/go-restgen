//nolint:errcheck // Test code - unchecked test cleanup is acceptable
package router_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/router"
	"github.com/sjgoldie/go-restgen/service"
)

type HardTask struct {
	bun.BaseModel `bun:"table:hard_tasks"`
	ID            int    `bun:"id,pk,autoincrement" json:"id"`
	Title         string `bun:"title" json:"title"`
	AuthorID      string `bun:"author_id" json:"author_id"`
	AssignedToID  string `bun:"assigned_to_id" json:"assigned_to_id"`
}

type HardEmpty struct {
	bun.BaseModel `bun:"table:hard_empties"`
	ID            int    `bun:"id,pk,autoincrement" json:"id"`
	Name          string `bun:"name" json:"name"`
}

type HardAuthor struct {
	bun.BaseModel `bun:"table:hard_authors"`
	ID            int    `bun:"id,pk,autoincrement" json:"id"`
	Name          string `bun:"name" json:"name"`
}

type HardBook struct {
	bun.BaseModel `bun:"table:hard_books"`
	ID            int         `bun:"id,pk,autoincrement" json:"id"`
	Title         string      `bun:"title" json:"title"`
	AuthorID      int         `bun:"author_id,notnull,skipupdate" json:"author_id"`
	Author        *HardAuthor `bun:"rel:belongs-to,join:author_id=id" json:"author,omitempty"`
}

func hardTables(t *testing.T, models ...any) *bun.DB {
	t.Helper()
	ds, err := datastore.Get()
	if err != nil {
		t.Fatalf("failed to get datastore: %v", err)
	}
	db := ds.GetDB()
	ctx := context.Background()
	for _, model := range models {
		if _, err := db.NewCreateTable().Model(model).IfNotExists().Exec(ctx); err != nil {
			t.Fatalf("failed to create table for %T: %v", model, err)
		}
		_, _ = db.NewDelete().Model(model).Where("1=1").Exec(ctx)
	}
	t.Cleanup(func() {
		for _, model := range models {
			_, _ = db.NewDropTable().Model(model).IfExists().Exec(ctx)
		}
	})
	return db
}

func hardRequest(t *testing.T, r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// #1: per-method ownership configs are honoured at the datastore.

func TestPerMethodOwnership_IsHonoured(t *testing.T) {
	db := hardTables(t, (*HardTask)(nil))
	if _, err := db.NewInsert().Model(&HardTask{Title: "shared", AuthorID: "author-1", AssignedToID: "assignee-1"}).Exec(context.Background()); err != nil {
		t.Fatal(err)
	}

	register := func(b *router.Builder) {
		router.RegisterRoutes[HardTask](b, "/tasks",
			router.AuthConfig{
				Methods:   []string{router.MethodGet, router.MethodList, router.MethodPut},
				Ownership: &router.OwnershipConfig{Fields: []string{"AuthorID", "AssignedToID"}},
			},
			router.AuthConfig{
				Methods:   []string{router.MethodDelete},
				Ownership: &router.OwnershipConfig{Fields: []string{"AuthorID"}},
			},
		)
	}

	asAssignee := addAuthMiddleware(chi.NewRouter(), "assignee-1", nil)
	register(router.NewBuilder(asAssignee))

	if w := hardRequest(t, asAssignee, "GET", "/tasks/1", ""); w.Code != http.StatusOK {
		t.Errorf("assignee GET: expected 200 (author OR assignee), got %d: %s", w.Code, w.Body.String())
	}
	if w := hardRequest(t, asAssignee, "GET", "/tasks", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"shared"`) {
		t.Errorf("assignee LIST: expected the task listed, got %d: %s", w.Code, w.Body.String())
	}
	if w := hardRequest(t, asAssignee, "DELETE", "/tasks/1", ""); w.Code != http.StatusNotFound {
		t.Errorf("assignee DELETE: expected 404 (author only), got %d: %s", w.Code, w.Body.String())
	}

	asAuthor := addAuthMiddleware(chi.NewRouter(), "author-1", nil)
	register(router.NewBuilder(asAuthor))

	if w := hardRequest(t, asAuthor, "DELETE", "/tasks/1", ""); w.Code != http.StatusNoContent {
		t.Errorf("author DELETE: expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

// #15 and #5: count=true always reports total_count; a malformed cursor is a 400.

type hardListResponse struct {
	Data       []HardEmpty `json:"data"`
	Pagination *struct {
		TotalCount *int `json:"total_count"`
	} `json:"pagination"`
	Error string `json:"error"`
}

func TestCountTrue_ZeroRowsReportsZero(t *testing.T) {
	hardTables(t, (*HardEmpty)(nil))
	r := chi.NewRouter()
	b := router.NewBuilder(r)
	router.RegisterRoutes[HardEmpty](b, "/empties", router.AllPublic(), router.WithPagination(20, 100))
	router.RegisterRoutes[HardEmpty](b, "/empties-offset", router.AllPublic(), router.WithPagination(20, 100, router.OffsetMode))

	for _, path := range []string{"/empties?count=true", "/empties-offset?count=true", "/empties-offset?limit=5&count=true"} {
		w := hardRequest(t, r, "GET", path, "")
		if w.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d: %s", path, w.Code, w.Body.String())
		}
		var resp hardListResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("%s: decode: %v", path, err)
		}
		if resp.Pagination == nil || resp.Pagination.TotalCount == nil {
			t.Errorf("%s: expected pagination.total_count present, got %s", path, w.Body.String())
			continue
		}
		if *resp.Pagination.TotalCount != 0 {
			t.Errorf("%s: expected total_count 0, got %d", path, *resp.Pagination.TotalCount)
		}
	}
}

func TestInvalidCursor_Returns400(t *testing.T) {
	hardTables(t, (*HardEmpty)(nil))
	r := chi.NewRouter()
	b := router.NewBuilder(r)
	router.RegisterRoutes[HardEmpty](b, "/empties", router.AllPublic(), router.WithPagination(20, 100))

	for _, path := range []string{"/empties?after=not-a-cursor", "/empties?before=zz", "/empties?after=e30"} {
		w := hardRequest(t, r, "GET", path, "")
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d: %s", path, w.Code, w.Body.String())
			continue
		}
		var resp hardListResponse
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Error != "bad_request" {
			t.Errorf("%s: expected error bad_request, got %q", path, resp.Error)
		}
	}
}

// #18: an AuthConfig without Methods is reported, not silently dropped.

func TestAuthConfigWithoutMethods_Warns(t *testing.T) {
	hardTables(t, (*HardEmpty)(nil))

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	r := chi.NewRouter()
	b := router.NewBuilder(r)
	router.RegisterRoutes[HardEmpty](b, "/misconfigured", router.AuthConfig{Scopes: []string{"admin"}})

	if !strings.Contains(logs.String(), "AuthConfig has no Methods") {
		t.Errorf("expected a warning about the missing Methods, got logs: %s", logs.String())
	}
	if w := hardRequest(t, r, "GET", "/misconfigured", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("route must stay blocked, got %d", w.Code)
	}
}

// #22: custom update and patch handlers on a nested single route get the child
// item as sent, not one whose PK was overwritten with the parent's path ID.

func TestSingleRoute_CustomUpdateDoesNotReceiveParentIDAsPK(t *testing.T) {
	db := hardTables(t, (*HardAuthor)(nil), (*HardBook)(nil))
	ctx := context.Background()
	for _, a := range []*HardAuthor{{Name: "first"}, {Name: "second"}} {
		if _, err := db.NewInsert().Model(a).Exec(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.NewInsert().Model(&HardBook{Title: "book", AuthorID: 2}).Exec(ctx); err != nil {
		t.Fatal(err)
	}

	var updateSawID, patchSawID = -1, -1
	r := chi.NewRouter()
	b := router.NewBuilder(r)
	router.RegisterRoutes[HardBook](b, "/books", router.AllPublic(), func(b *router.Builder) {
		router.RegisterRoutes[HardAuthor](b, "/author",
			router.WithRelationName("Author"),
			router.AsSingleRouteWithUpdate("AuthorID"),
			router.AllPublic(),
			router.WithCustomUpdate(func(ctx context.Context, svc *service.Common[HardAuthor], _ *metadata.TypeMetadata, _ *metadata.AuthInfo, id string, item HardAuthor) (*HardAuthor, error) {
				updateSawID = item.ID
				return svc.UpdateByParentRelation(ctx, id, item)
			}),
			router.WithCustomPatch(func(ctx context.Context, svc *service.Common[HardAuthor], _ *metadata.TypeMetadata, _ *metadata.AuthInfo, id string, _ *HardAuthor, patched HardAuthor) (*HardAuthor, error) {
				patchSawID = patched.ID
				return svc.PatchByParentRelation(ctx, id, patched)
			}),
		)
	})

	w := hardRequest(t, r, "PUT", "/books/1/author", `{"name":"renamed"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if updateSawID == 1 {
		t.Error("custom update received the parent book ID stamped as the author PK")
	}
	var updated HardAuthor
	_ = json.Unmarshal(w.Body.Bytes(), &updated)
	if updated.ID != 2 || updated.Name != "renamed" {
		t.Errorf("expected author 2 renamed, got %+v", updated)
	}

	w = hardRequest(t, r, "PATCH", "/books/1/author", `{"name":"patched"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if patchSawID != 2 {
		t.Errorf("custom patch must receive the fetched author's own PK 2, got %d", patchSawID)
	}
}
