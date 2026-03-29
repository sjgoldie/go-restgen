//nolint:staticcheck,errcheck,gosec // Test code - string context keys and unchecked test cleanup are acceptable
package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/handler"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/service"
)

func TestGetAll_ReturnsEnvelope(t *testing.T) {
	cleanTable(t)

	db, _ := datastore.Get()
	users := []TestUser{
		{Name: "Alice", Email: "alice@test.com"},
		{Name: "Bob", Email: "bob@test.com"},
	}
	for _, u := range users {
		_, err := db.GetDB().NewInsert().Model(&u).Exec(context.Background())
		if err != nil {
			t.Fatal("Failed to insert test user:", err)
		}
	}

	r := chi.NewRouter()
	r.Use(withMeta(userMeta))
	r.Get("/users", handler.GetAll[TestUser](handler.StandardGetAll[TestUser]))

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var envelope handler.ListResponse
	if err := json.NewDecoder(w.Body).Decode(&envelope); err != nil {
		t.Fatalf("Failed to decode envelope: %v", err)
	}

	items, ok := envelope.Data.([]interface{})
	if !ok {
		t.Fatalf("Expected data to be an array, got %T", envelope.Data)
	}
	if len(items) != 2 {
		t.Errorf("Expected 2 items, got %d", len(items))
	}
}

func TestGetAll_EnvelopeWithOffsetPagination(t *testing.T) {
	cleanTable(t)

	db, _ := datastore.Get()
	users := []TestUser{
		{Name: "Alice", Email: "alice@test.com"},
		{Name: "Bob", Email: "bob@test.com"},
		{Name: "Charlie", Email: "charlie@test.com"},
	}
	for _, u := range users {
		_, err := db.GetDB().NewInsert().Model(&u).Exec(context.Background())
		if err != nil {
			t.Fatal("Failed to insert test user:", err)
		}
	}

	paginatedMeta := &metadata.TypeMetadata{
		TypeID:       "test_paginated_id",
		TypeName:     "TestUser",
		TableName:    "users",
		URLParamUUID: "id",
		PKField:      "ID",
		ModelType:    reflect.TypeOf(TestUser{}),
		DefaultLimit: 10,
		MaxLimit:     100,
	}

	r := chi.NewRouter()
	r.Use(withMeta(paginatedMeta))
	r.Get("/users", handler.GetAll[TestUser](handler.StandardGetAll[TestUser]))

	req := httptest.NewRequest(http.MethodGet, "/users?limit=2&offset=1&count=true", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var envelope handler.ListResponse
	if err := json.NewDecoder(w.Body).Decode(&envelope); err != nil {
		t.Fatalf("Failed to decode envelope: %v", err)
	}

	items, ok := envelope.Data.([]interface{})
	if !ok {
		t.Fatalf("Expected data to be an array, got %T", envelope.Data)
	}
	if len(items) != 2 {
		t.Errorf("Expected 2 items, got %d", len(items))
	}

	if envelope.Pagination == nil {
		t.Fatal("Expected pagination to be present")
	}
	if envelope.Pagination.Limit == nil || *envelope.Pagination.Limit != 2 {
		t.Errorf("Expected limit=2, got %v", envelope.Pagination.Limit)
	}
	if envelope.Pagination.Offset == nil || *envelope.Pagination.Offset != 1 {
		t.Errorf("Expected offset=1, got %v", envelope.Pagination.Offset)
	}
	if envelope.Pagination.TotalCount == nil || *envelope.Pagination.TotalCount != 3 {
		t.Errorf("Expected total_count=3, got %v", envelope.Pagination.TotalCount)
	}

	// Headers should NOT be set — all metadata is in the envelope
	if h := w.Header().Get("X-Total-Count"); h != "" {
		t.Errorf("X-Total-Count header should not be set, got %s", h)
	}
	if h := w.Header().Get("X-Limit"); h != "" {
		t.Errorf("X-Limit header should not be set, got %s", h)
	}
	if h := w.Header().Get("X-Offset"); h != "" {
		t.Errorf("X-Offset header should not be set, got %s", h)
	}
}

func TestGetAll_EnvelopeWithSums(t *testing.T) {
	db, _ := datastore.Get()
	_, err := db.GetDB().NewCreateTable().Model((*TestSumProduct)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create sum_products table:", err)
	}
	defer db.GetDB().NewDropTable().Model((*TestSumProduct)(nil)).IfExists().Exec(context.Background())

	_, _ = db.GetDB().NewDelete().Model((*TestSumProduct)(nil)).Where("1=1").Exec(context.Background())
	_, _ = db.GetDB().Exec("DELETE FROM sqlite_sequence WHERE name = 'sum_products'")

	products := []TestSumProduct{
		{Name: "Apple", Price: 100, Rating: 4.5, InStock: true},
		{Name: "Banana", Price: 50, Rating: 3.8, InStock: true},
	}
	for _, p := range products {
		_, err := db.GetDB().NewInsert().Model(&p).Exec(context.Background())
		if err != nil {
			t.Fatal("Failed to insert test product:", err)
		}
	}

	sumMeta := &metadata.TypeMetadata{
		TypeID:         "test_sum_envelope_id",
		TypeName:       "TestSumProduct",
		TableName:      "sum_products",
		URLParamUUID:   "id",
		PKField:        "ID",
		ModelType:      reflect.TypeOf(TestSumProduct{}),
		SummableFields: []string{"Price", "Rating"},
		DefaultLimit:   10,
		MaxLimit:       100,
	}

	r := chi.NewRouter()
	r.Use(withMeta(sumMeta))
	r.Get("/products", handler.GetAll[TestSumProduct](handler.StandardGetAll[TestSumProduct]))

	req := httptest.NewRequest(http.MethodGet, "/products?sum=Price,Rating&count=true", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var envelope handler.ListResponse
	if err := json.NewDecoder(w.Body).Decode(&envelope); err != nil {
		t.Fatalf("Failed to decode envelope: %v", err)
	}

	if envelope.Sums == nil {
		t.Fatal("Expected sums to be present")
	}
	if envelope.Sums["Price"] != 150 {
		t.Errorf("Expected Price sum=150, got %v", envelope.Sums["Price"])
	}
	if envelope.Sums["Rating"] != 8.3 {
		t.Errorf("Expected Rating sum=8.3, got %v", envelope.Sums["Rating"])
	}

	if envelope.Pagination == nil || envelope.Pagination.TotalCount == nil || *envelope.Pagination.TotalCount != 2 {
		t.Errorf("Expected total_count=2 in pagination")
	}

	// Sum headers should NOT be set
	if h := w.Header().Get("X-Sum-Price"); h != "" {
		t.Errorf("X-Sum-Price header should not be set, got %s", h)
	}
	if h := w.Header().Get("X-Sum-Rating"); h != "" {
		t.Errorf("X-Sum-Rating header should not be set, got %s", h)
	}
}

func TestGetAll_EnvelopeEmptyResult(t *testing.T) {
	cleanTable(t)

	r := chi.NewRouter()
	r.Use(withMeta(userMeta))
	r.Get("/users", handler.GetAll[TestUser](handler.StandardGetAll[TestUser]))

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var envelope handler.ListResponse
	if err := json.NewDecoder(w.Body).Decode(&envelope); err != nil {
		t.Fatalf("Failed to decode envelope: %v", err)
	}

	items, ok := envelope.Data.([]interface{})
	if !ok {
		t.Fatalf("Expected data to be an array, got %T", envelope.Data)
	}
	if len(items) != 0 {
		t.Errorf("Expected 0 items, got %d", len(items))
	}
}

func TestGetAll_EnvelopeNoPaginationWhenNotRequested(t *testing.T) {
	cleanTable(t)

	db, _ := datastore.Get()
	_, err := db.GetDB().NewInsert().Model(&TestUser{Name: "Alice", Email: "alice@test.com"}).Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to insert test user:", err)
	}

	r := chi.NewRouter()
	r.Use(withMeta(userMeta))
	r.Get("/users", handler.GetAll[TestUser](handler.StandardGetAll[TestUser]))

	// No pagination params
	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var envelope handler.ListResponse
	if err := json.NewDecoder(w.Body).Decode(&envelope); err != nil {
		t.Fatalf("Failed to decode envelope: %v", err)
	}

	if envelope.Pagination != nil {
		t.Errorf("Expected no pagination when no pagination params requested, got %+v", envelope.Pagination)
	}
	if envelope.Sums != nil {
		t.Errorf("Expected no sums when not requested, got %+v", envelope.Sums)
	}
}

func TestGetAll_CustomGetAllReturnsEnvelope(t *testing.T) {
	cleanTable(t)

	db, _ := datastore.Get()
	users := []TestUser{
		{Name: "Alice", Email: "alice@test.com"},
		{Name: "Bob", Email: "bob@test.com"},
	}
	for _, u := range users {
		db.GetDB().NewInsert().Model(&u).Exec(context.Background())
	}

	customGetAll := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo) ([]*TestUser, int, map[string]float64, *metadata.CursorInfo, error) {
		return []*TestUser{{ID: 1, Name: "Custom"}}, 1, nil, nil, nil
	}

	r := chi.NewRouter()
	r.Use(withMeta(userMeta))
	r.Get("/users", handler.GetAll[TestUser](customGetAll))

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var envelope handler.ListResponse
	if err := json.NewDecoder(w.Body).Decode(&envelope); err != nil {
		t.Fatalf("Failed to decode envelope: %v", err)
	}

	items, ok := envelope.Data.([]interface{})
	if !ok {
		t.Fatalf("Expected data to be an array, got %T", envelope.Data)
	}
	if len(items) != 1 {
		t.Errorf("Expected 1 item, got %d", len(items))
	}
}

func TestGetAll_EnvelopeWithCursorPagination(t *testing.T) {
	cleanTable(t)

	db, _ := datastore.Get()
	users := []TestUser{
		{Name: "Alice", Email: "alice@test.com"},
		{Name: "Bob", Email: "bob@test.com"},
	}
	for _, u := range users {
		db.GetDB().NewInsert().Model(&u).Exec(context.Background())
	}

	nextCursor := "abc123"
	prevCursor := "xyz789"
	customGetAll := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo) ([]*TestUser, int, map[string]float64, *metadata.CursorInfo, error) {
		items, _, _, _, err := svc.GetAll(ctx)
		if err != nil {
			return nil, 0, nil, nil, err
		}
		cursor := &metadata.CursorInfo{
			NextCursor: nextCursor,
			PrevCursor: prevCursor,
			HasMore:    true,
		}
		return items, 0, nil, cursor, nil
	}

	r := chi.NewRouter()
	r.Use(withMeta(userMeta))
	r.Get("/users", handler.GetAll[TestUser](customGetAll))

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var envelope handler.ListResponse
	if err := json.NewDecoder(w.Body).Decode(&envelope); err != nil {
		t.Fatalf("Failed to decode envelope: %v", err)
	}

	items, ok := envelope.Data.([]interface{})
	if !ok {
		t.Fatalf("Expected data to be an array, got %T", envelope.Data)
	}
	if len(items) != 2 {
		t.Errorf("Expected 2 items, got %d", len(items))
	}

	if envelope.Pagination == nil {
		t.Fatal("Expected pagination to be present")
	}
	if envelope.Pagination.HasMore == nil || !*envelope.Pagination.HasMore {
		t.Error("Expected has_more=true")
	}
	if envelope.Pagination.NextCursor == nil || *envelope.Pagination.NextCursor != nextCursor {
		t.Errorf("Expected next_cursor=%q, got %v", nextCursor, envelope.Pagination.NextCursor)
	}
	if envelope.Pagination.PrevCursor == nil || *envelope.Pagination.PrevCursor != prevCursor {
		t.Errorf("Expected prev_cursor=%q, got %v", prevCursor, envelope.Pagination.PrevCursor)
	}
	if envelope.Pagination.Limit != nil {
		t.Error("Expected limit to be nil for cursor pagination")
	}
	if envelope.Pagination.Offset != nil {
		t.Error("Expected offset to be nil for cursor pagination")
	}
}

func TestGetAll_EnvelopeWithCursorPaginationAndCount(t *testing.T) {
	cleanTable(t)

	db, _ := datastore.Get()
	users := []TestUser{
		{Name: "Alice", Email: "alice@test.com"},
		{Name: "Bob", Email: "bob@test.com"},
		{Name: "Charlie", Email: "charlie@test.com"},
	}
	for _, u := range users {
		db.GetDB().NewInsert().Model(&u).Exec(context.Background())
	}

	customGetAll := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo) ([]*TestUser, int, map[string]float64, *metadata.CursorInfo, error) {
		cursor := &metadata.CursorInfo{
			NextCursor: "next123",
			HasMore:    true,
		}
		return []*TestUser{{ID: 1, Name: "Alice"}}, 3, nil, cursor, nil
	}

	paginatedMeta := &metadata.TypeMetadata{
		TypeID:       "test_cursor_count_id",
		TypeName:     "TestUser",
		TableName:    "users",
		URLParamUUID: "id",
		PKField:      "ID",
		ModelType:    reflect.TypeOf(TestUser{}),
		DefaultLimit: 10,
		MaxLimit:     100,
	}

	r := chi.NewRouter()
	r.Use(withMeta(paginatedMeta))
	r.Get("/users", handler.GetAll[TestUser](customGetAll))

	req := httptest.NewRequest(http.MethodGet, "/users?count=true", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var envelope handler.ListResponse
	if err := json.NewDecoder(w.Body).Decode(&envelope); err != nil {
		t.Fatalf("Failed to decode envelope: %v", err)
	}

	if envelope.Pagination == nil {
		t.Fatal("Expected pagination to be present")
	}
	if envelope.Pagination.HasMore == nil || !*envelope.Pagination.HasMore {
		t.Error("Expected has_more=true")
	}
	if envelope.Pagination.NextCursor == nil || *envelope.Pagination.NextCursor != "next123" {
		t.Errorf("Expected next_cursor=next123, got %v", envelope.Pagination.NextCursor)
	}
	if envelope.Pagination.TotalCount == nil || *envelope.Pagination.TotalCount != 3 {
		t.Errorf("Expected total_count=3, got %v", envelope.Pagination.TotalCount)
	}
}

func TestGetAll_EnvelopeCursorNoPrevOnFirstPage(t *testing.T) {
	customGetAll := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo) ([]*TestUser, int, map[string]float64, *metadata.CursorInfo, error) {
		cursor := &metadata.CursorInfo{
			NextCursor: "next_page",
			PrevCursor: "",
			HasMore:    true,
		}
		return []*TestUser{{ID: 1, Name: "Alice"}}, 0, nil, cursor, nil
	}

	r := chi.NewRouter()
	r.Use(withMeta(userMeta))
	r.Get("/users", handler.GetAll[TestUser](customGetAll))

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var envelope handler.ListResponse
	if err := json.NewDecoder(w.Body).Decode(&envelope); err != nil {
		t.Fatalf("Failed to decode envelope: %v", err)
	}

	if envelope.Pagination == nil {
		t.Fatal("Expected pagination to be present")
	}
	if envelope.Pagination.PrevCursor != nil {
		t.Error("Expected prev_cursor to be nil on first page")
	}
	if envelope.Pagination.NextCursor == nil || *envelope.Pagination.NextCursor != "next_page" {
		t.Errorf("Expected next_cursor=next_page, got %v", envelope.Pagination.NextCursor)
	}
}

func TestGetAll_EnvelopeCursorLastPage(t *testing.T) {
	customGetAll := func(ctx context.Context, svc *service.Common[TestUser], meta *metadata.TypeMetadata, auth *metadata.AuthInfo) ([]*TestUser, int, map[string]float64, *metadata.CursorInfo, error) {
		cursor := &metadata.CursorInfo{
			NextCursor: "",
			PrevCursor: "prev_page",
			HasMore:    false,
		}
		return []*TestUser{{ID: 3, Name: "Charlie"}}, 0, nil, cursor, nil
	}

	r := chi.NewRouter()
	r.Use(withMeta(userMeta))
	r.Get("/users", handler.GetAll[TestUser](customGetAll))

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var envelope handler.ListResponse
	if err := json.NewDecoder(w.Body).Decode(&envelope); err != nil {
		t.Fatalf("Failed to decode envelope: %v", err)
	}

	if envelope.Pagination == nil {
		t.Fatal("Expected pagination to be present")
	}
	if envelope.Pagination.HasMore == nil || *envelope.Pagination.HasMore {
		t.Error("Expected has_more=false on last page")
	}
	if envelope.Pagination.NextCursor != nil {
		t.Error("Expected next_cursor to be nil on last page")
	}
	if envelope.Pagination.PrevCursor == nil || *envelope.Pagination.PrevCursor != "prev_page" {
		t.Errorf("Expected prev_cursor=prev_page, got %v", envelope.Pagination.PrevCursor)
	}
}
