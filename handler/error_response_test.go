//nolint:staticcheck,errcheck,gosec // Test code - string context keys and unchecked test cleanup are acceptable
package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/sjgoldie/go-restgen/datastore"
	apperrors "github.com/sjgoldie/go-restgen/errors"
	"github.com/sjgoldie/go-restgen/handler"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/service"
)

type errorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func assertJSONError(t *testing.T, w *httptest.ResponseRecorder, expectedStatus int, expectedError, expectedMessage string) {
	t.Helper()

	if w.Code != expectedStatus {
		t.Errorf("Expected status %d, got %d: %s", expectedStatus, w.Code, w.Body.String())
		return
	}

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got %q", ct)
	}

	var body errorBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Errorf("Expected JSON error body, got %q (unmarshal error: %v)", w.Body.String(), err)
		return
	}

	if body.Error != expectedError {
		t.Errorf("Expected error code %q, got %q", expectedError, body.Error)
	}

	if body.Message != expectedMessage {
		t.Errorf("Expected message %q, got %q", expectedMessage, body.Message)
	}
}

type failWriter struct {
	header http.Header
	code   int
}

func (f *failWriter) Header() http.Header       { return f.header }
func (f *failWriter) WriteHeader(code int)      { f.code = code }
func (f *failWriter) Write([]byte) (int, error) { return 0, fmt.Errorf("write failed") }

func TestErrorResponse_WriteError_EncodeFail(t *testing.T) {
	w := &failWriter{header: http.Header{}}
	handler.WriteError(w, http.StatusInternalServerError, handler.ErrCodeInternalError, http.StatusText(http.StatusInternalServerError))

	if w.code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, w.code)
	}
	if w.header.Get("Content-Type") != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %q", w.header.Get("Content-Type"))
	}
}

func TestErrorResponse_GetNotFound(t *testing.T) {
	cleanTable(t)

	r := chi.NewRouter()
	r.Use(withMeta(userMeta))
	r.Get("/users/{id}", handler.Get[TestUser](handler.StandardGet[TestUser]))

	req := httptest.NewRequest(http.MethodGet, "/users/999", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assertJSONError(t, w, http.StatusNotFound, "not_found", http.StatusText(http.StatusNotFound))
}

func TestErrorResponse_CreateDuplicate(t *testing.T) {
	cleanTable(t)

	db, _ := datastore.Get()
	user := &TestUser{Name: "Existing", Email: "dup@example.com"}
	_, err := db.GetDB().NewInsert().Model(user).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to insert test user:", err)
	}

	r := chi.NewRouter()
	r.Use(withMeta(userMeta))
	r.Post("/users", handler.Create[TestUser](handler.StandardCreate[TestUser]))

	body, _ := json.Marshal(TestUser{Name: "Another", Email: "dup@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assertJSONError(t, w, http.StatusBadRequest, "duplicate", http.StatusText(http.StatusBadRequest))
}

func TestErrorResponse_CreateInvalidJSON(t *testing.T) {
	cleanTable(t)

	r := chi.NewRouter()
	r.Use(withMeta(userMeta))
	r.Post("/users", handler.Create[TestUser](handler.StandardCreate[TestUser]))

	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assertJSONError(t, w, http.StatusBadRequest, "bad_request", http.StatusText(http.StatusBadRequest))
}

func TestErrorResponse_CreateBodyTooLarge(t *testing.T) {
	cleanTable(t)

	r := chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Use(withBodyLimit(100))
		r.Post("/", handler.Create[TestUser](handler.StandardCreate[TestUser]))
	})

	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(oversizedJSON(200)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assertJSONError(t, w, http.StatusRequestEntityTooLarge, "request_too_large", http.StatusText(http.StatusRequestEntityTooLarge))
}

func TestErrorResponse_ValidationError(t *testing.T) {
	cleanTable(t)

	customCreate := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, items []TestUser) ([]*TestUser, error) {
		return nil, apperrors.NewValidationError("name is required")
	}

	r := chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Post("/batch", handler.BatchCreate[TestUser](customCreate))
	})

	req := httptest.NewRequest(http.MethodPost, "/users/batch", strings.NewReader(`[{"email":"a@b.com"}]`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assertJSONError(t, w, http.StatusBadRequest, "validation_error", "name is required")
}

func TestErrorResponse_DeadlineExceeded(t *testing.T) {
	getAllFunc := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo) ([]*TestUser, int, map[string]float64, *metadata.CursorInfo, error) {
		return nil, 0, nil, nil, context.DeadlineExceeded
	}

	r := chi.NewRouter()
	r.Use(withMeta(userMeta))
	r.Get("/users", handler.GetAll[TestUser](getAllFunc))

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assertJSONError(t, w, http.StatusGatewayTimeout, "request_timeout", http.StatusText(http.StatusGatewayTimeout))
}

func TestErrorResponse_ServiceUnavailable(t *testing.T) {
	getAllFunc := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo) ([]*TestUser, int, map[string]float64, *metadata.CursorInfo, error) {
		return nil, 0, nil, nil, apperrors.ErrUnavailable
	}

	r := chi.NewRouter()
	r.Use(withMeta(userMeta))
	r.Get("/users", handler.GetAll[TestUser](getAllFunc))

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assertJSONError(t, w, http.StatusServiceUnavailable, "service_unavailable", http.StatusText(http.StatusServiceUnavailable))
}

func TestErrorResponse_InternalServerError(t *testing.T) {
	getAllFunc := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo) ([]*TestUser, int, map[string]float64, *metadata.CursorInfo, error) {
		return nil, 0, nil, nil, fmt.Errorf("unexpected database error")
	}

	r := chi.NewRouter()
	r.Use(withMeta(userMeta))
	r.Get("/users", handler.GetAll[TestUser](getAllFunc))

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assertJSONError(t, w, http.StatusInternalServerError, "internal_error", http.StatusText(http.StatusInternalServerError))
}

func TestErrorResponse_InvalidReference(t *testing.T) {
	cleanTable(t)

	customCreate := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, items []TestUser) ([]*TestUser, error) {
		return nil, apperrors.ErrInvalidReference
	}

	r := chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Post("/batch", handler.BatchCreate[TestUser](customCreate))
	})

	req := httptest.NewRequest(http.MethodPost, "/users/batch", strings.NewReader(`[{"name":"a","email":"a@b.com"}]`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assertJSONError(t, w, http.StatusBadRequest, "invalid_reference", http.StatusText(http.StatusBadRequest))
}

func TestErrorResponse_BatchNotImplemented(t *testing.T) {
	cleanTable(t)

	fileMeta := &metadata.TypeMetadata{
		TypeID:         userMeta.TypeID,
		TypeName:       userMeta.TypeName,
		TableName:      userMeta.TableName,
		URLParamUUID:   userMeta.URLParamUUID,
		ModelType:      userMeta.ModelType,
		PKField:        "ID",
		IsFileResource: true,
	}

	r := chi.NewRouter()
	r.Route("/files", func(r chi.Router) {
		r.Use(withMeta(fileMeta))
		r.Post("/batch", handler.BatchCreate[TestUser](handler.StandardBatchCreate[TestUser]))
	})

	req := httptest.NewRequest(http.MethodPost, "/files/batch", strings.NewReader(`[{"name":"f","email":"f@b.com"}]`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assertJSONError(t, w, http.StatusNotImplemented, "not_implemented", http.StatusText(http.StatusNotImplemented))
}

func TestErrorResponse_MissingID(t *testing.T) {
	cleanTable(t)

	req := httptest.NewRequest(http.MethodGet, "/users/", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = ctxWithMeta(ctx, userMeta)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.Get[TestUser](handler.StandardGet[TestUser])(w, req)

	assertJSONError(t, w, http.StatusBadRequest, "bad_request", http.StatusText(http.StatusBadRequest))
}

func TestErrorResponse_BatchEmptyArray(t *testing.T) {
	cleanTable(t)

	r := chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Post("/batch", handler.BatchCreate[TestUser](handler.StandardBatchCreate[TestUser]))
	})

	req := httptest.NewRequest(http.MethodPost, "/users/batch", strings.NewReader(`[]`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assertJSONError(t, w, http.StatusBadRequest, "bad_request", http.StatusText(http.StatusBadRequest))
}

func TestErrorResponse_BatchExceedsLimit(t *testing.T) {
	cleanTable(t)

	limitedMeta := &metadata.TypeMetadata{
		TypeID:       userMeta.TypeID,
		TypeName:     userMeta.TypeName,
		TableName:    userMeta.TableName,
		URLParamUUID: userMeta.URLParamUUID,
		ModelType:    userMeta.ModelType,
		BatchLimit:   2,
	}

	r := chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		r.Use(withMeta(limitedMeta))
		r.Post("/batch", handler.BatchCreate[TestUser](handler.StandardBatchCreate[TestUser]))
	})

	body := `[{"name":"A","email":"a@b.com"},{"name":"B","email":"b@b.com"},{"name":"C","email":"c@b.com"}]`
	req := httptest.NewRequest(http.MethodPost, "/users/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assertJSONError(t, w, http.StatusBadRequest, "bad_request", http.StatusText(http.StatusBadRequest))
}

func TestErrorResponse_BatchInvalidJSON(t *testing.T) {
	cleanTable(t)

	r := chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Post("/batch", handler.BatchCreate[TestUser](handler.StandardBatchCreate[TestUser]))
	})

	req := httptest.NewRequest(http.MethodPost, "/users/batch", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assertJSONError(t, w, http.StatusBadRequest, "bad_request", http.StatusText(http.StatusBadRequest))
}

func TestErrorResponse_UpdateInvalidID(t *testing.T) {
	cleanTable(t)

	body := []byte(`{"name":"Updated","email":"updated@example.com"}`)
	req := httptest.NewRequest(http.MethodPut, "/users/invalid", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "invalid")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, metadata.MetadataKey, userMeta)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.Update[TestUser](handler.StandardUpdate[TestUser])(w, req)

	assertJSONError(t, w, http.StatusBadRequest, "bad_request", http.StatusText(http.StatusBadRequest))
}

func TestErrorResponse_DeleteNotFound(t *testing.T) {
	cleanTable(t)

	r := chi.NewRouter()
	r.Use(withMeta(userMeta))
	r.Delete("/users/{id}", handler.Delete[TestUser](handler.StandardDelete[TestUser]))

	req := httptest.NewRequest(http.MethodDelete, "/users/999", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assertJSONError(t, w, http.StatusNotFound, "not_found", http.StatusText(http.StatusNotFound))
}
