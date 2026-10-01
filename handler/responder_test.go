package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/sjgoldie/go-restgen/handler"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/service"
)

// csvReport writes a CSV body with its own content type, status and headers.
type csvReport struct {
	name string
	rows []string
}

func (c csvReport) WriteResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="`+c.name+`"`)
	w.WriteHeader(http.StatusAccepted)
	_, err := w.Write([]byte(strings.Join(c.rows, "\n")))
	return err
}

// failingResponder writes part of a response, then fails.
type failingResponder struct{}

func (failingResponder) WriteResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte("partial"))
	return errors.New("generator failed")
}

// pointerResponder implements Responder on a pointer receiver.
type pointerResponder struct{ body string }

func (p *pointerResponder) WriteResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "text/plain")
	_, err := w.Write([]byte(p.body))
	return err
}

// serveUserEndpoint serves fn as GET /users/{id}/report and returns the response for user 1.
func serveUserEndpoint(t *testing.T, fn handler.EndpointHandler[TestUser]) *httptest.ResponseRecorder {
	t.Helper()
	cleanTable(t)
	if _, err := testDB.GetDB().NewInsert().Model(&TestUser{Name: "Test User", Email: "test@example.com"}).Exec(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Route("/users/{id}", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Get("/report", handler.Endpoint[TestUser](fn))
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/users/1/report", nil))
	return w
}

func TestEndpoint_Responder(t *testing.T) {
	w := serveUserEndpoint(t, func(_ context.Context, _ *service.Common[TestUser], _ *metadata.TypeMetadata, _ *metadata.AuthInfo, id string, item *TestUser, _ []byte) (any, int, error) {
		return csvReport{name: "user-" + id + ".csv", rows: []string{"name", item.Name}}, http.StatusTeapot, nil
	})

	if w.Code != http.StatusAccepted {
		t.Errorf("the Responder's status is used, not the returned one: got %d", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "text/csv" {
		t.Errorf("content type: got %q", got)
	}
	if got := w.Header().Get("Content-Disposition"); got != `attachment; filename="user-1.csv"` {
		t.Errorf("content disposition: got %q", got)
	}
	if got := w.Body.String(); got != "name\nTest User" {
		t.Errorf("body: got %q", got)
	}
}

func TestEndpoint_ResponderPointer(t *testing.T) {
	w := serveUserEndpoint(t, func(context.Context, *service.Common[TestUser], *metadata.TypeMetadata, *metadata.AuthInfo, string, *TestUser, []byte) (any, int, error) {
		return &pointerResponder{body: "pointer"}, 0, nil
	})
	if w.Code != http.StatusOK || w.Body.String() != "pointer" {
		t.Errorf("got %d %q", w.Code, w.Body.String())
	}

	w = serveUserEndpoint(t, func(context.Context, *service.Common[TestUser], *metadata.TypeMetadata, *metadata.AuthInfo, string, *TestUser, []byte) (any, int, error) {
		var nilResponder *pointerResponder
		return nilResponder, 0, nil
	})
	if w.Code != http.StatusInternalServerError {
		t.Errorf("nil Responder: expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestEndpoint_ResponderWriteError(t *testing.T) {
	w := serveUserEndpoint(t, func(context.Context, *service.Common[TestUser], *metadata.TypeMetadata, *metadata.AuthInfo, string, *TestUser, []byte) (any, int, error) {
		return failingResponder{}, 0, nil
	})
	if w.Code != http.StatusOK || w.Body.String() != "partial" {
		t.Errorf("the response written before the error stands: got %d %q", w.Code, w.Body.String())
	}
}

func TestEndpoint_ErrorBeforeResponder(t *testing.T) {
	w := serveUserEndpoint(t, func(context.Context, *service.Common[TestUser], *metadata.TypeMetadata, *metadata.AuthInfo, string, *TestUser, []byte) (any, int, error) {
		return csvReport{}, 0, errors.New("render failed")
	})
	if w.Code != http.StatusInternalServerError || w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("a handler error is the normal error response: got %d %q", w.Code, w.Header().Get("Content-Type"))
	}
}

func TestEndpoint_ResponderNotForMissingItem(t *testing.T) {
	cleanTable(t)
	called := false
	r := chi.NewRouter()
	r.Route("/users/{id}", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Get("/report", handler.Endpoint[TestUser](func(context.Context, *service.Common[TestUser], *metadata.TypeMetadata, *metadata.AuthInfo, string, *TestUser, []byte) (any, int, error) {
			called = true
			return csvReport{}, 0, nil
		}))
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/users/999/report", nil))
	if w.Code != http.StatusNotFound || called {
		t.Errorf("missing item: expected 404 without calling the handler, got %d (called %v)", w.Code, called)
	}
}

func TestEndpoint_NonResponderStillJSON(t *testing.T) {
	w := serveUserEndpoint(t, func(context.Context, *service.Common[TestUser], *metadata.TypeMetadata, *metadata.AuthInfo, string, *TestUser, []byte) (any, int, error) {
		var none *WorkflowStatus
		return none, 0, nil
	})
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != "null" || w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("a typed nil that is not a Responder is still encoded as JSON: got %d %q", w.Code, w.Body.String())
	}
}

func TestRootEndpoint_Responder(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/export", handler.RootEndpoint(func(context.Context, *metadata.AuthInfo, *http.Request) (any, int, error) {
		return csvReport{name: "export.csv", rows: []string{"a", "b"}}, 0, nil
	}))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/export", nil))
	if w.Code != http.StatusAccepted || w.Header().Get("Content-Type") != "text/csv" || w.Body.String() != "a\nb" {
		t.Errorf("got %d %q %q", w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}
	var probe any
	if json.Unmarshal(w.Body.Bytes(), &probe) == nil {
		t.Errorf("the body must not be JSON-encoded: %q", w.Body.String())
	}
}
