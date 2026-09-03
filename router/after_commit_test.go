//nolint:errcheck // Test code - unchecked test cleanup is acceptable
package router_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/datastore"
	apperrors "github.com/sjgoldie/go-restgen/errors"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/router"
	"github.com/sjgoldie/go-restgen/service"
)

type HookedTask struct {
	bun.BaseModel `bun:"table:hooked_tasks"`
	ID            int    `bun:"id,pk,autoincrement" json:"id"`
	Title         string `bun:"title,notnull" json:"title"`
	Status        string `bun:"status,notnull" json:"status"`
}

type hookedTaskCall struct {
	op        metadata.Operation
	id        int
	oldStatus string
	newStatus string
	visible   bool
}

func TestWithAfterCommit(t *testing.T) {
	db, err := datastore.Get()
	if err != nil {
		t.Fatal("failed to get datastore:", err)
	}
	if _, err := db.GetDB().NewCreateTable().Model((*HookedTask)(nil)).IfNotExists().Exec(context.Background()); err != nil {
		t.Fatal("failed to create hooked_tasks table:", err)
	}
	defer db.GetDB().NewDropTable().Model((*HookedTask)(nil)).IfExists().Exec(context.Background())

	var mu sync.Mutex
	var calls []hookedTaskCall
	takeCalls := func() []hookedTaskCall {
		mu.Lock()
		defer mu.Unlock()
		taken := calls
		calls = nil
		return taken
	}

	r := chi.NewRouter()
	b := router.NewBuilder(r)
	router.RegisterRoutes[HookedTask](b, "/tasks",
		router.AllPublicWithBatch(),
		router.WithAfterCommit(func(ac metadata.AfterCommitContext[HookedTask]) error {
			call := hookedTaskCall{op: ac.Operation}
			if ac.Old != nil {
				call.id = ac.Old.ID
				call.oldStatus = ac.Old.Status
			}
			if ac.New != nil {
				call.id = ac.New.ID
				call.newStatus = ac.New.Status
			}

			// Read back through the service layer with the hook context, as an
			// application hook would, to observe the committed state.
			svc, err := service.New[HookedTask]()
			if err != nil {
				return err
			}
			_, getErr := svc.Get(ac.Ctx, strconv.Itoa(call.id))
			switch {
			case getErr == nil:
				call.visible = true
			case errors.Is(getErr, apperrors.ErrNotFound):
				call.visible = false
			default:
				return getErr
			}

			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, call)
			return nil
		}),
	)

	do := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	var created HookedTask

	t.Run("create fires hook with committed item visible", func(t *testing.T) {
		w := do("POST", "/tasks", `{"title":"Task","status":"pending"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
			t.Fatal("failed to decode response:", err)
		}

		got := takeCalls()
		if len(got) != 1 {
			t.Fatalf("expected 1 hook call, got %d", len(got))
		}
		if got[0].op != metadata.OpCreate || got[0].id != created.ID || got[0].newStatus != "pending" || got[0].oldStatus != "" {
			t.Errorf("unexpected create call: %+v", got[0])
		}
		if !got[0].visible {
			t.Error("created item must be visible from the hook")
		}
	})

	t.Run("update fires hook with old and new", func(t *testing.T) {
		w := do("PUT", "/tasks/"+strconv.Itoa(created.ID), `{"title":"Task","status":"active"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		got := takeCalls()
		if len(got) != 1 {
			t.Fatalf("expected 1 hook call, got %d", len(got))
		}
		if got[0].op != metadata.OpUpdate || got[0].oldStatus != "pending" || got[0].newStatus != "active" || !got[0].visible {
			t.Errorf("unexpected update call: %+v", got[0])
		}
	})

	t.Run("patch fires hook with OpPatch", func(t *testing.T) {
		w := do("PATCH", "/tasks/"+strconv.Itoa(created.ID), `{"status":"done"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		got := takeCalls()
		if len(got) != 1 {
			t.Fatalf("expected 1 hook call, got %d", len(got))
		}
		if got[0].op != metadata.OpPatch || got[0].oldStatus != "active" || got[0].newStatus != "done" || !got[0].visible {
			t.Errorf("unexpected patch call: %+v", got[0])
		}
	})

	t.Run("delete fires hook after the row is gone", func(t *testing.T) {
		w := do("DELETE", "/tasks/"+strconv.Itoa(created.ID), "")
		if w.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
		}

		got := takeCalls()
		if len(got) != 1 {
			t.Fatalf("expected 1 hook call, got %d", len(got))
		}
		if got[0].op != metadata.OpDelete || got[0].oldStatus != "done" || got[0].newStatus != "" {
			t.Errorf("unexpected delete call: %+v", got[0])
		}
		if got[0].visible {
			t.Error("deleted item must not be visible from the hook")
		}
	})

	t.Run("batch create fires hook once per item", func(t *testing.T) {
		w := do("POST", "/tasks/batch", `[{"title":"A","status":"pending"},{"title":"B","status":"pending"}]`)
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}

		got := takeCalls()
		if len(got) != 2 {
			t.Fatalf("expected 2 hook calls, got %d", len(got))
		}
		for i, call := range got {
			if call.op != metadata.OpCreate || !call.visible || call.id == 0 {
				t.Errorf("unexpected batch create call %d: %+v", i, call)
			}
		}
	})

	t.Run("failed write does not fire hook", func(t *testing.T) {
		w := do("PUT", "/tasks/999999", `{"title":"Missing","status":"active"}`)
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
		}
		if got := takeCalls(); len(got) != 0 {
			t.Errorf("hook must not fire for a failed write, got %d calls", len(got))
		}
	})
}
