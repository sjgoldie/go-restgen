//nolint:dupl,errcheck,gosec,goconst,staticcheck // Test code - duplicate test patterns, unchecked test cleanup, repeated test strings, and string context keys are acceptable
package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
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

// ActionTestOrder is a test model for action tests
type ActionTestOrder struct {
	bun.BaseModel `bun:"table:action_test_orders"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	Status        string    `bun:"status,notnull" json:"status"`
	OwnerID       string    `bun:"owner_id,notnull" json:"owner_id"`
	CreatedAt     time.Time `bun:"created_at,notnull,default:current_timestamp" json:"created_at,omitempty"`
}

// CancelRequest represents the action payload
type CancelRequest struct {
	Reason string `json:"reason"`
}

// cancelOrderAction is a test action handler
func cancelOrderAction(ctx context.Context, svc *service.Common[ActionTestOrder], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *ActionTestOrder, payload []byte) (*ActionTestOrder, error) {
	var req CancelRequest
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, err
		}
	}
	item.Status = "cancelled"
	return svc.Update(ctx, id, *item)
}

// refundOrderAction is a test action handler that returns nil
func refundOrderAction(ctx context.Context, svc *service.Common[ActionTestOrder], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item *ActionTestOrder, payload []byte) (*ActionTestOrder, error) {
	// Process refund but return no content
	return nil, nil
}

func setupActionTestTable(t *testing.T) {
	t.Helper()

	ds, err := datastore.Get()
	if err != nil {
		t.Fatal("Failed to get datastore:", err)
	}

	db := ds.GetDB()
	ctx := context.Background()

	// Create table
	_, err = db.NewCreateTable().Model((*ActionTestOrder)(nil)).IfNotExists().Exec(ctx)
	if err != nil {
		t.Fatal("Failed to create table:", err)
	}
}

func cleanActionTestTable(t *testing.T) {
	t.Helper()

	ds, err := datastore.Get()
	if err != nil {
		t.Fatal("Failed to get datastore:", err)
	}

	db := ds.GetDB()
	ctx := context.Background()

	// Delete all rows
	_, _ = db.NewDelete().Model((*ActionTestOrder)(nil)).Where("1=1").Exec(ctx)
	// Reset auto-increment
	_, _ = db.Exec("DELETE FROM sqlite_sequence WHERE name = 'action_test_orders'")
}

func TestAction_Registration(t *testing.T) {
	setupActionTestTable(t)
	cleanActionTestTable(t)

	// Create a test order
	ds, _ := datastore.Get()
	order := &ActionTestOrder{Status: "pending", OwnerID: "user1"}
	_, err := ds.GetDB().NewInsert().Model(order).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test order:", err)
	}

	r := chi.NewRouter()
	b := router.NewBuilder(r, testDB(t))

	router.RegisterRoutes[ActionTestOrder](b, "/orders",
		router.AllPublic(),
		router.WithAction("cancel", cancelOrderAction, router.AuthConfig{
			Scopes: []string{router.ScopePublic},
		}),
	)

	// Test action endpoint
	body := `{"reason": "customer request"}`
	req := httptest.NewRequest("POST", "/orders/1/cancel", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify the order was updated
	var result ActionTestOrder
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal("Failed to unmarshal response:", err)
	}

	if result.Status != "cancelled" {
		t.Errorf("Expected status 'cancelled', got %q", result.Status)
	}
}

func TestAction_MultipleActions(t *testing.T) {
	setupActionTestTable(t)
	cleanActionTestTable(t)

	// Create a test order
	ds, _ := datastore.Get()
	order := &ActionTestOrder{Status: "pending", OwnerID: "user1"}
	_, err := ds.GetDB().NewInsert().Model(order).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test order:", err)
	}

	r := chi.NewRouter()
	b := router.NewBuilder(r, testDB(t))

	router.RegisterRoutes[ActionTestOrder](b, "/orders",
		router.AllPublic(),
		router.WithAction("cancel", cancelOrderAction, router.AuthConfig{
			Scopes: []string{router.ScopePublic},
		}),
		router.WithAction("refund", refundOrderAction, router.AuthConfig{
			Scopes: []string{router.ScopePublic},
		}),
	)

	// Test cancel action
	req1 := httptest.NewRequest("POST", "/orders/1/cancel", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Errorf("Cancel: Expected status 200, got %d: %s", w1.Code, w1.Body.String())
	}

	// Test refund action (returns 204)
	req2 := httptest.NewRequest("POST", "/orders/1/refund", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusNoContent {
		t.Errorf("Refund: Expected status 204, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestAction_WithAuth(t *testing.T) {
	setupActionTestTable(t)
	cleanActionTestTable(t)

	// Create a test order
	ds, _ := datastore.Get()
	order := &ActionTestOrder{Status: "pending", OwnerID: "user1"}
	_, err := ds.GetDB().NewInsert().Model(order).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test order:", err)
	}

	r := chi.NewRouter()

	// Auth middleware
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check for admin header
			if r.Header.Get("X-Admin") == "true" {
				auth := &router.AuthInfo{UserID: "admin1", Scopes: []string{"admin"}}
				ctx := context.WithValue(r.Context(), router.AuthInfoKey, auth)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	b := router.NewBuilder(r, testDB(t))

	router.RegisterRoutes[ActionTestOrder](b, "/orders",
		router.AllPublic(),
		router.WithAction("cancel", cancelOrderAction, router.AuthConfig{
			Scopes: []string{"admin"}, // Only admin can cancel
		}),
	)

	// Test without auth - should fail (401 because no auth at all)
	req1 := httptest.NewRequest("POST", "/orders/1/cancel", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	if w1.Code != http.StatusUnauthorized {
		t.Errorf("No auth: Expected status 401, got %d: %s", w1.Code, w1.Body.String())
	}

	// Test with admin auth - should succeed
	req2 := httptest.NewRequest("POST", "/orders/1/cancel", nil)
	req2.Header.Set("X-Admin", "true")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("With auth: Expected status 200, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestAction_WithOwnership(t *testing.T) {
	setupActionTestTable(t)
	cleanActionTestTable(t)

	// Create test orders
	ds, _ := datastore.Get()
	order1 := &ActionTestOrder{Status: "pending", OwnerID: "user1"}
	order2 := &ActionTestOrder{Status: "pending", OwnerID: "user2"}
	_, err := ds.GetDB().NewInsert().Model(order1).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test order 1:", err)
	}
	_, err = ds.GetDB().NewInsert().Model(order2).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test order 2:", err)
	}

	r := chi.NewRouter()

	// Auth middleware
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := &router.AuthInfo{UserID: "user1", Scopes: []string{"user"}}
			ctx := context.WithValue(r.Context(), router.AuthInfoKey, auth)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})

	b := router.NewBuilder(r, testDB(t))

	// Ownership needs to be on the resource auth config to be stored in metadata
	// The action inherits this from the context set by wrapWithAuth
	router.RegisterRoutes[ActionTestOrder](b, "/orders",
		router.AuthConfig{
			Methods: []string{router.MethodAll},
			Scopes:  []string{"user"},
			Ownership: &router.OwnershipConfig{
				Fields: []string{"OwnerID"},
			},
		},
		router.WithAction("cancel", cancelOrderAction, router.AuthConfig{
			Scopes: []string{"user"},
			Ownership: &router.OwnershipConfig{
				Fields: []string{"OwnerID"},
			},
		}),
	)

	// Test cancelling own order - should succeed
	req1 := httptest.NewRequest("POST", "/orders/1/cancel", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Errorf("Own order: Expected status 200, got %d: %s", w1.Code, w1.Body.String())
	}

	// Test cancelling other user's order - should fail with 404 (ownership hides existence)
	req2 := httptest.NewRequest("POST", "/orders/2/cancel", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusNotFound {
		t.Errorf("Other's order: Expected status 404, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestAction_BlockedByDefault(t *testing.T) {
	setupActionTestTable(t)
	cleanActionTestTable(t)

	// Create a test order
	ds, _ := datastore.Get()
	order := &ActionTestOrder{Status: "pending", OwnerID: "user1"}
	_, err := ds.GetDB().NewInsert().Model(order).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create test order:", err)
	}

	r := chi.NewRouter()
	b := router.NewBuilder(r, testDB(t))

	// Action with empty auth config (no scopes) - should be blocked
	router.RegisterRoutes[ActionTestOrder](b, "/orders",
		router.AllPublic(),
		router.WithAction("cancel", cancelOrderAction, router.AuthConfig{}),
	)

	req := httptest.NewRequest("POST", "/orders/1/cancel", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Empty auth config (no scopes) = 401 Unauthorized
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401 (blocked), got %d: %s", w.Code, w.Body.String())
	}
}

func TestAction_NotFound(t *testing.T) {
	setupActionTestTable(t)
	cleanActionTestTable(t)

	r := chi.NewRouter()
	b := router.NewBuilder(r, testDB(t))

	router.RegisterRoutes[ActionTestOrder](b, "/orders",
		router.AllPublic(),
		router.WithAction("cancel", cancelOrderAction, router.AuthConfig{
			Scopes: []string{router.ScopePublic},
		}),
	)

	// Test action on non-existent order
	req := httptest.NewRequest("POST", "/orders/999/cancel", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWithAction_TypeAndName(t *testing.T) {
	// Test that WithAction returns correct config
	config := router.WithAction("cancel", cancelOrderAction, router.AuthConfig{
		Scopes: []string{"user"},
	})

	if config.Name != "cancel" {
		t.Errorf("Expected name 'cancel', got %q", config.Name)
	}

	if config.Fn == nil {
		t.Error("Expected function to be set")
	}

	if len(config.Auth.Scopes) != 1 || config.Auth.Scopes[0] != "user" {
		t.Errorf("Expected scopes ['user'], got %v", config.Auth.Scopes)
	}
}

func TestAction_CRUDStillWorks(t *testing.T) {
	setupActionTestTable(t)
	cleanActionTestTable(t)

	r := chi.NewRouter()
	b := router.NewBuilder(r, testDB(t))

	router.RegisterRoutes[ActionTestOrder](b, "/orders",
		router.AllPublic(),
		router.WithAction("cancel", cancelOrderAction, router.AuthConfig{
			Scopes: []string{router.ScopePublic},
		}),
	)

	// Test that regular CRUD still works

	// Create
	createBody := `{"status": "pending", "owner_id": "user1"}`
	createReq := httptest.NewRequest("POST", "/orders", strings.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	r.ServeHTTP(createW, createReq)

	if createW.Code != http.StatusCreated {
		t.Errorf("Create: Expected status 201, got %d: %s", createW.Code, createW.Body.String())
	}

	// Get
	getReq := httptest.NewRequest("GET", "/orders/1", nil)
	getW := httptest.NewRecorder()
	r.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Errorf("Get: Expected status 200, got %d: %s", getW.Code, getW.Body.String())
	}

	// List
	listReq := httptest.NewRequest("GET", "/orders", nil)
	listW := httptest.NewRecorder()
	r.ServeHTTP(listW, listReq)

	if listW.Code != http.StatusOK {
		t.Errorf("List: Expected status 200, got %d: %s", listW.Code, listW.Body.String())
	}

	// Action
	actionReq := httptest.NewRequest("POST", "/orders/1/cancel", nil)
	actionW := httptest.NewRecorder()
	r.ServeHTTP(actionW, actionReq)

	if actionW.Code != http.StatusOK {
		t.Errorf("Action: Expected status 200, got %d: %s", actionW.Code, actionW.Body.String())
	}

	// Delete
	deleteReq := httptest.NewRequest("DELETE", "/orders/1", nil)
	deleteW := httptest.NewRecorder()
	r.ServeHTTP(deleteW, deleteReq)

	if deleteW.Code != http.StatusNoContent {
		t.Errorf("Delete: Expected status 204, got %d: %s", deleteW.Code, deleteW.Body.String())
	}
}

func TestActionConfig_TypeAssertion(t *testing.T) {
	// Test that ActionConfig can be type asserted from interface{}
	config := router.WithAction("test", cancelOrderAction, router.AuthConfig{})

	var i interface{} = config
	_, ok := i.(router.ActionConfig[ActionTestOrder])

	if !ok {
		t.Error("Expected ActionConfig to be type assertable")
	}
}

func TestActionConfig_ReflectType(t *testing.T) {
	config := router.WithAction("test", cancelOrderAction, router.AuthConfig{})

	configType := reflect.TypeOf(config)
	if configType.Name() != "ActionConfig[github.com/sjgoldie/go-restgen/router_test.ActionTestOrder]" {
		// The name includes the full package path for the type parameter
		if !strings.Contains(configType.String(), "ActionConfig") {
			t.Errorf("Unexpected type name: %s", configType.String())
		}
	}
}
