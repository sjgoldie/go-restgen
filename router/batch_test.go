//nolint:dupl,errcheck,gosec,goconst,staticcheck // Test code - duplicate test patterns, unchecked test cleanup, repeated test strings, and string context keys are acceptable
package router_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/router"
	"github.com/sjgoldie/go-restgen/service"
)

// BatchTestItem is a test model for batch operations
type BatchTestItem struct {
	bun.BaseModel `bun:"table:batch_test_items"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	Name          string    `bun:"name,notnull" json:"name"`
	Value         int       `bun:"value" json:"value"`
	CreatedAt     time.Time `bun:"created_at" json:"created_at"`
}

func setupBatchTestTable(t *testing.T) {
	db, err := datastore.Get()
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.GetDB().NewCreateTable().Model((*BatchTestItem)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Clean up any existing data
	_, _ = db.GetDB().NewTruncateTable().Model((*BatchTestItem)(nil)).Exec(context.Background())
}

func TestBatch_AllPublicWithBatch(t *testing.T) {
	setupBatchTestTable(t)

	r := chi.NewRouter()
	b := router.NewBuilder(r, testDB(t))
	router.RegisterRoutes[BatchTestItem](b, "/items",
		router.AllPublicWithBatch(),
	)

	// Test batch create endpoint exists
	req := httptest.NewRequest("POST", "/items/batch", strings.NewReader(`[{"name": "Test", "value": 1}]`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBatch_AllScopedWithBatch(t *testing.T) {
	setupBatchTestTable(t)

	r := chi.NewRouter()
	b := router.NewBuilder(r, testDB(t))
	router.RegisterRoutes[BatchTestItem](b, "/items",
		router.AllScopedWithBatch("admin"),
	)

	// Without auth - should be 401
	req := httptest.NewRequest("POST", "/items/batch", strings.NewReader(`[{"name": "Test", "value": 1}]`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d: %s", w.Code, w.Body.String())
	}

	// With admin auth - should work
	req = httptest.NewRequest("POST", "/items/batch", strings.NewReader(`[{"name": "Test", "value": 1}]`))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), metadata.AuthInfoKey, &metadata.AuthInfo{
		UserID: "admin1",
		Scopes: []string{"admin"},
	})
	req = req.WithContext(ctx)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBatch_WithBatchLimit(t *testing.T) {
	setupBatchTestTable(t)

	r := chi.NewRouter()
	b := router.NewBuilder(r, testDB(t))
	router.RegisterRoutes[BatchTestItem](b, "/items",
		router.AllPublicWithBatch(),
		router.WithBatchLimit(2),
	)

	// Within limit - should work
	req := httptest.NewRequest("POST", "/items/batch", strings.NewReader(`[{"name": "Test1", "value": 1}, {"name": "Test2", "value": 2}]`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Exceeds limit - should fail
	req = httptest.NewRequest("POST", "/items/batch", strings.NewReader(`[{"name": "Test1", "value": 1}, {"name": "Test2", "value": 2}, {"name": "Test3", "value": 3}]`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBatch_CustomBatchCreate(t *testing.T) {
	setupBatchTestTable(t)

	customCalled := false
	customFn := func(ctx context.Context, svc *service.Common[BatchTestItem], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, items []BatchTestItem) ([]*BatchTestItem, error) {
		customCalled = true
		// Modify items
		for i := range items {
			items[i].Value *= 10
		}
		return svc.BatchCreate(ctx, items)
	}

	r := chi.NewRouter()
	b := router.NewBuilder(r, testDB(t))
	router.RegisterRoutes[BatchTestItem](b, "/items",
		router.AllPublicWithBatch(),
		router.WithCustomBatchCreate(customFn),
	)

	req := httptest.NewRequest("POST", "/items/batch", strings.NewReader(`[{"name": "Test", "value": 5}]`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	if !customCalled {
		t.Error("Custom batch create function was not called")
	}

	// Verify value was multiplied by 10
	if !strings.Contains(w.Body.String(), `"value":50`) {
		t.Errorf("Expected value to be 50, got: %s", w.Body.String())
	}
}

func TestBatch_CustomBatchUpdate(t *testing.T) {
	setupBatchTestTable(t)

	// First create an item via batch create to get the ID
	customCalled := false
	customFn := func(ctx context.Context, svc *service.Common[BatchTestItem], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, items []BatchTestItem) ([]*BatchTestItem, error) {
		customCalled = true
		return svc.BatchUpdate(ctx, items)
	}

	r := chi.NewRouter()
	b := router.NewBuilder(r, testDB(t))
	router.RegisterRoutes[BatchTestItem](b, "/items",
		router.AllPublicWithBatch(),
		router.WithCustomBatchUpdate(customFn),
	)

	// Create item first
	req := httptest.NewRequest("POST", "/items/batch", strings.NewReader(`[{"name": "Original", "value": 1}]`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Failed to create item: %d: %s", w.Code, w.Body.String())
	}

	// Get the created ID from response
	var created []BatchTestItem
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	itemID := created[0].ID

	// Now update
	updateBody := fmt.Sprintf(`[{"id": %d, "name": "Updated", "value": 2}]`, itemID)
	req = httptest.NewRequest("PUT", "/items/batch", strings.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if !customCalled {
		t.Error("Custom batch update function was not called")
	}
}

func TestBatch_CustomBatchDelete(t *testing.T) {
	setupBatchTestTable(t)

	customCalled := false
	customFn := func(ctx context.Context, svc *service.Common[BatchTestItem], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, items []BatchTestItem) error {
		customCalled = true
		return svc.BatchDelete(ctx, items)
	}

	r := chi.NewRouter()
	b := router.NewBuilder(r, testDB(t))
	router.RegisterRoutes[BatchTestItem](b, "/items",
		router.AllPublicWithBatch(),
		router.WithCustomBatchDelete(customFn),
	)

	// Create item first
	req := httptest.NewRequest("POST", "/items/batch", strings.NewReader(`[{"name": "ToDelete", "value": 1}]`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Failed to create item: %d: %s", w.Code, w.Body.String())
	}

	// Get the created ID from response
	var created []BatchTestItem
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	itemID := created[0].ID

	// Now delete
	deleteBody := fmt.Sprintf(`[{"id": %d}]`, itemID)
	req = httptest.NewRequest("DELETE", "/items/batch", strings.NewReader(deleteBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("Expected 204, got %d: %s", w.Code, w.Body.String())
	}

	if !customCalled {
		t.Error("Custom batch delete function was not called")
	}
}

func TestBatch_NoBatchMethodsNoRoutes(t *testing.T) {
	setupBatchTestTable(t)

	r := chi.NewRouter()
	b := router.NewBuilder(r, testDB(t))
	// Only AllPublic, no batch methods
	router.RegisterRoutes[BatchTestItem](b, "/items",
		router.AllPublic(),
	)

	// Batch endpoints should not work when not configured
	// Chi returns 405 because /items/batch matches /{id} pattern with id="batch"
	// and POST is not a valid method on /{id}
	req := httptest.NewRequest("POST", "/items/batch", strings.NewReader(`[{"name": "Test", "value": 1}]`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBatch_PartialBatchMethods(t *testing.T) {
	setupBatchTestTable(t)

	r := chi.NewRouter()
	b := router.NewBuilder(r, testDB(t))
	// Only batch create, not update or delete
	router.RegisterRoutes[BatchTestItem](b, "/items",
		router.AllPublic(),
		router.AuthConfig{
			Methods: []string{router.MethodBatchCreate},
			Scopes:  []string{router.ScopePublic},
		},
	)

	// Batch create should work
	req := httptest.NewRequest("POST", "/items/batch", strings.NewReader(`[{"name": "Test", "value": 1}]`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected 201 for batch create, got %d: %s", w.Code, w.Body.String())
	}

	// Batch update should not work (method not allowed or not found)
	req = httptest.NewRequest("PUT", "/items/batch", strings.NewReader(`[{"id": 1, "name": "Test", "value": 1}]`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Should be 405 (method not allowed) because /batch exists but PUT is not registered
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405 for batch update, got %d: %s", w.Code, w.Body.String())
	}
}
