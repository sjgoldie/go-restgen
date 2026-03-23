//nolint:staticcheck,errcheck,gosec // Test code - string context keys and unchecked test cleanup are acceptable
package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/sjgoldie/go-restgen/handler"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/service"
)

// withBodyLimit creates test middleware that limits request body size.
// This simulates what createMetadataMiddleware does in production
// when MaxBodySize is configured on TypeMetadata.
func withBodyLimit(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}

// oversizedJSON generates a JSON object with a name field exceeding the given byte count.
func oversizedJSON(bytes int) string {
	name := strings.Repeat("x", bytes)
	return `{"name":"` + name + `","email":"test@example.com"}`
}

// oversizedJSONArray generates a JSON array with a name field exceeding the given byte count.
func oversizedJSONArray(bytes int) string {
	name := strings.Repeat("x", bytes)
	return `[{"name":"` + name + `","email":"test@example.com"}]`
}

func TestCreate_BodyTooLarge(t *testing.T) {
	cleanTable(t)

	r := chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Use(withBodyLimit(100))
		r.Post("/", handler.Create[TestUser](handler.StandardCreate[TestUser]))
	})

	req := httptest.NewRequest("POST", "/users", strings.NewReader(oversizedJSON(200)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Expected status 413, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreate_BodyWithinLimit(t *testing.T) {
	cleanTable(t)

	r := chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Use(withBodyLimit(1024))
		r.Post("/", handler.Create[TestUser](handler.StandardCreate[TestUser]))
	})

	body := `{"name":"Alice","email":"alice@example.com"}`
	req := httptest.NewRequest("POST", "/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreate_BodyExactlyAtLimit(t *testing.T) {
	cleanTable(t)

	body := `{"name":"Al","email":"a@b.com"}`
	bodyLen := int64(len(body))

	r := chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Use(withBodyLimit(bodyLen))
		r.Post("/", handler.Create[TestUser](handler.StandardCreate[TestUser]))
	})

	req := httptest.NewRequest("POST", "/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdate_BodyTooLarge(t *testing.T) {
	cleanTable(t)

	db := testDB.GetDB()
	user := &TestUser{Name: "Test User", Email: "test@example.com"}
	_, err := db.NewInsert().Model(user).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test user:", err)
	}

	r := chi.NewRouter()
	r.Route("/users/{id}", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Use(withBodyLimit(100))
		r.Put("/", handler.Update[TestUser](handler.StandardUpdate[TestUser]))
	})

	req := httptest.NewRequest("PUT", "/users/1", strings.NewReader(oversizedJSON(200)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Expected status 413, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdate_BodyWithinLimit(t *testing.T) {
	cleanTable(t)

	db := testDB.GetDB()
	user := &TestUser{Name: "Test User", Email: "test@example.com"}
	_, err := db.NewInsert().Model(user).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test user:", err)
	}

	r := chi.NewRouter()
	r.Route("/users/{id}", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Use(withBodyLimit(1024))
		r.Put("/", handler.Update[TestUser](handler.StandardUpdate[TestUser]))
	})

	body := `{"name":"Updated User","email":"updated@example.com"}`
	req := httptest.NewRequest("PUT", "/users/1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBatchCreate_BodyTooLarge(t *testing.T) {
	cleanTable(t)

	r := chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Use(withBodyLimit(100))
		r.Post("/batch", handler.BatchCreate[TestUser](handler.StandardBatchCreate[TestUser]))
	})

	req := httptest.NewRequest("POST", "/users/batch", strings.NewReader(oversizedJSONArray(200)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Expected status 413, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBatchCreate_BodyWithinLimit(t *testing.T) {
	cleanTable(t)

	r := chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Use(withBodyLimit(1024))
		r.Post("/batch", handler.BatchCreate[TestUser](handler.StandardBatchCreate[TestUser]))
	})

	body := `[{"name":"User 1","email":"user1@example.com"}]`
	req := httptest.NewRequest("POST", "/users/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAction_BodyTooLarge(t *testing.T) {
	cleanTable(t)

	db := testDB.GetDB()
	user := &TestUser{Name: "Test User", Email: "test@example.com"}
	_, err := db.NewInsert().Model(user).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test user:", err)
	}

	actionFn := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *TestUser, payload []byte) (*TestUser, error) {
		return item, nil
	}

	r := chi.NewRouter()
	r.Route("/users/{id}", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Use(withBodyLimit(100))
		r.Post("/action", handler.Action[TestUser](actionFn))
	})

	req := httptest.NewRequest("POST", "/users/1/action", strings.NewReader(oversizedJSON(200)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Expected status 413, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAction_BodyWithinLimit(t *testing.T) {
	cleanTable(t)

	db := testDB.GetDB()
	user := &TestUser{Name: "Test User", Email: "test@example.com"}
	_, err := db.NewInsert().Model(user).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test user:", err)
	}

	//nolint:unparam
	actionFn := func(_ context.Context, _ *service.Common[TestUser], _ *metadata.TypeMetadata, _ *metadata.AuthInfo, _ string, item *TestUser, _ []byte) (*TestUser, error) {
		return item, nil
	}

	r := chi.NewRouter()
	r.Route("/users/{id}", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Use(withBodyLimit(1024))
		r.Post("/action", handler.Action[TestUser](actionFn))
	})

	body := `{"reason":"test"}`
	req := httptest.NewRequest("POST", "/users/1/action", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestEndpoint_BodyTooLarge(t *testing.T) {
	cleanTable(t)

	db := testDB.GetDB()
	user := &TestUser{Name: "Test User", Email: "test@example.com"}
	_, err := db.NewInsert().Model(user).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test user:", err)
	}

	funcHandler := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *TestUser, payload []byte) (any, int, error) {
		return item, http.StatusOK, nil
	}

	r := chi.NewRouter()
	r.Route("/users/{id}", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Use(withBodyLimit(100))
		r.Post("/ep", handler.Endpoint[TestUser](funcHandler))
	})

	req := httptest.NewRequest("POST", "/users/1/ep", strings.NewReader(oversizedJSON(200)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Expected status 413, got %d: %s", w.Code, w.Body.String())
	}
}

func TestEndpoint_BodyWithinLimit(t *testing.T) {
	cleanTable(t)

	db := testDB.GetDB()
	user := &TestUser{Name: "Test User", Email: "test@example.com"}
	_, err := db.NewInsert().Model(user).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test user:", err)
	}

	//nolint:unparam
	funcHandler := func(_ context.Context, _ *service.Common[TestUser], _ *metadata.TypeMetadata, _ *metadata.AuthInfo, _ string, item *TestUser, _ []byte) (any, int, error) {
		return item, http.StatusOK, nil
	}

	r := chi.NewRouter()
	r.Route("/users/{id}", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Use(withBodyLimit(1024))
		r.Post("/ep", handler.Endpoint[TestUser](funcHandler))
	})

	body := `{"data":"test"}`
	req := httptest.NewRequest("POST", "/users/1/ep", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreate_CustomLimit(t *testing.T) {
	cleanTable(t)

	const customLimit int64 = 200

	r := chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Use(withBodyLimit(customLimit))
		r.Post("/", handler.Create[TestUser](handler.StandardCreate[TestUser]))
	})

	t.Run("within custom limit", func(t *testing.T) {
		cleanTable(t)

		body := `{"name":"Alice","email":"alice@example.com"}`
		req := httptest.NewRequest("POST", "/users", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("exceeds custom limit", func(t *testing.T) {
		cleanTable(t)

		req := httptest.NewRequest("POST", "/users", strings.NewReader(oversizedJSON(300)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("Expected status 413, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestBatchUpdate_BodyTooLarge(t *testing.T) {
	cleanTable(t)

	r := chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Use(withBodyLimit(100))
		r.Put("/batch", handler.BatchUpdate[TestUser](handler.StandardBatchUpdate[TestUser]))
	})

	req := httptest.NewRequest("PUT", "/users/batch", strings.NewReader(oversizedJSONArray(200)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Expected status 413, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBatchDelete_BodyTooLarge(t *testing.T) {
	cleanTable(t)

	r := chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		r.Use(withMeta(userMeta))
		r.Use(withBodyLimit(100))
		r.Delete("/batch", handler.BatchDelete[TestUser](handler.StandardBatchDelete[TestUser]))
	})

	req := httptest.NewRequest("DELETE", "/users/batch", strings.NewReader(oversizedJSONArray(200)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Expected status 413, got %d: %s", w.Code, w.Body.String())
	}
}
