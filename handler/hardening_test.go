//nolint:errcheck // Test code - unchecked test cleanup is acceptable
package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/sjgoldie/go-restgen/handler"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/service"
)

func paginatedUserMeta() *metadata.TypeMetadata {
	meta := userMeta.Clone()
	meta.Pagination = metadata.CursorPagination
	meta.DefaultLimit = 10
	return meta
}

func TestGetAll_InvalidCursorReturns400(t *testing.T) {
	cleanTable(t)

	r := chi.NewRouter()
	r.With(withMeta(paginatedUserMeta())).Get(testUsersPath, handler.GetAll[TestUser](handler.StandardGetAll[TestUser]))

	for _, query := range []string{"?after=not-a-cursor", "?before=zz"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", testUsersPath+query, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d: %s", query, w.Code, w.Body.String())
			continue
		}
		if !strings.Contains(w.Body.String(), `"bad_request"`) {
			t.Errorf("%s: expected bad_request error code, got %s", query, w.Body.String())
		}
	}
}

func TestGetAll_CountTrueReportsZeroForNoRows(t *testing.T) {
	cleanTable(t)

	r := chi.NewRouter()
	r.With(withMeta(userMeta)).Get("/offset"+testUsersPath, handler.GetAll[TestUser](handler.StandardGetAll[TestUser]))
	r.With(withMeta(paginatedUserMeta())).Get("/cursor"+testUsersPath, handler.GetAll[TestUser](handler.StandardGetAll[TestUser]))

	for _, path := range []string{"/offset" + testUsersPath + "?count=true", "/cursor" + testUsersPath + "?count=true"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d: %s", path, w.Code, w.Body.String())
		}
		var resp struct {
			Pagination *struct {
				TotalCount *int `json:"total_count"`
			} `json:"pagination"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Pagination == nil || resp.Pagination.TotalCount == nil || *resp.Pagination.TotalCount != 0 {
			t.Errorf("%s: expected pagination.total_count 0, got %s", path, w.Body.String())
		}
	}
}

func TestUpdateAndPatch_SingleRouteDoesNotStampPathIDIntoPK(t *testing.T) {
	cleanTable(t)
	user := &TestUser{Name: "Single", Email: "single@example.com"}
	if _, err := testDB.GetDB().NewInsert().Model(user).Exec(context.Background()); err != nil {
		t.Fatal(err)
	}

	singleMeta := userMeta.Clone()
	singleMeta.IsSingleRoute = true

	updateSaw, patchSaw := -1, -1
	updateFn := func(_ context.Context, _ *service.Common[TestUser], _ *metadata.TypeMetadata, _ *metadata.AuthInfo, _ string, item TestUser) (*TestUser, error) {
		updateSaw = item.ID
		if item.Email == "" {
			return nil, errors.New("email is required")
		}
		return &item, nil
	}
	patchFn := func(_ context.Context, _ *service.Common[TestUser], _ *metadata.TypeMetadata, _ *metadata.AuthInfo, _ string, _ *TestUser, patched TestUser) (*TestUser, error) {
		patchSaw = patched.ID
		if patched.Email == "" {
			return nil, errors.New("email is required")
		}
		return &patched, nil
	}

	r := chi.NewRouter()
	r.Route(testUsersPath+"/{id}", func(r chi.Router) {
		r.Use(withMeta(singleMeta))
		r.Put("/", handler.Update[TestUser](updateFn))
		r.Patch("/", handler.Patch[TestUser](patchFn, handler.StandardGet[TestUser]))
	})

	path := testUsersPath + "/" + strconv.Itoa(user.ID)
	body := `{"id":999,"name":"Renamed","email":"single@example.com"}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if updateSaw != 999 {
		t.Errorf("PUT on a single route must leave the body's ID alone, custom handler saw %d", updateSaw)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest("PATCH", path, strings.NewReader(`{"id":999}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if patchSaw != 999 {
		t.Errorf("PATCH on a single route must leave the body's ID alone, custom handler saw %d", patchSaw)
	}
}

func TestBatchPatch_MissingItemIs404(t *testing.T) {
	cleanTable(t)

	r := chi.NewRouter()
	r.With(withMeta(userMeta)).Patch(testUsersPath+"/batch", handler.BatchPatch[TestUser](handler.StandardBatchPatch[TestUser]))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("PATCH", testUsersPath+"/batch", strings.NewReader(`[{"id":424242,"name":"ghost"}]`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for a batch patch of a missing row, got %d: %s", w.Code, w.Body.String())
	}
}
