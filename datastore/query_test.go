package datastore_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/uptrace/bun"
)

// Test product names as constants to avoid duplication
const (
	productApple    = "Apple"
	productBanana   = "Banana"
	productCarrot   = "Carrot"
	productDonut    = "Donut"
	productEggplant = "Eggplant"
)

// TestQueryProduct is a test model for query tests
type TestQueryProduct struct {
	bun.BaseModel `bun:"table:query_products"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	Name          string    `bun:"name,notnull" json:"name"`
	Category      string    `bun:"category,notnull" json:"category"`
	Price         int       `bun:"price,notnull" json:"price"`
	InStock       bool      `bun:"in_stock,notnull" json:"in_stock"`
	CreatedAt     time.Time `bun:"created_at,notnull,default:current_timestamp" json:"created_at"`
}

var testQueryProductMeta = &metadata.TypeMetadata{
	TypeID:           "query_product_id",
	TypeName:         "TestQueryProduct",
	TableName:        "query_products",
	URLParamUUID:     "productId",
	ModelType:        reflect.TypeOf(TestQueryProduct{}),
	FilterableFields: []string{"Name", "Category", "Price", "InStock"},
	SortableFields:   []string{"Name", "Price", "CreatedAt"},
	DefaultLimit:     10,
	MaxLimit:         100,
}

func setupQueryTestDB(t *testing.T) (*datastore.SQLite, func()) {
	t.Helper()

	db, err := datastore.NewSQLite(":memory:")
	if err != nil {
		t.Fatal("Failed to create database:", err)
	}

	// Initialize global singleton for the wrapper
	if err := datastore.Initialize(db); err != nil {
		db.Cleanup()
		t.Fatal("Failed to initialize datastore:", err)
	}

	// Create table
	_, err = db.GetDB().NewCreateTable().Model((*TestQueryProduct)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		datastore.Cleanup()
		db.Cleanup()
		t.Fatal("Failed to create table:", err)
	}

	return db, func() {
		_, _ = db.GetDB().NewDropTable().Model((*TestQueryProduct)(nil)).IfExists().Exec(context.Background())
		datastore.Cleanup()
		db.Cleanup()
	}
}

func ctxWithQueryMeta(meta *metadata.TypeMetadata) context.Context {
	return context.WithValue(context.Background(), metadata.MetadataKey, meta)
}

func seedQueryProducts(t *testing.T, wrapper *datastore.Wrapper[TestQueryProduct], ctx context.Context) {
	t.Helper()

	products := []TestQueryProduct{
		{Name: productApple, Category: "Fruit", Price: 100, InStock: true},
		{Name: productBanana, Category: "Fruit", Price: 50, InStock: true},
		{Name: productCarrot, Category: "Vegetable", Price: 30, InStock: true},
		{Name: productDonut, Category: "Bakery", Price: 150, InStock: false},
		{Name: productEggplant, Category: "Vegetable", Price: 80, InStock: true},
	}

	for _, p := range products {
		_, err := wrapper.Create(ctx, p)
		if err != nil {
			t.Fatal("Failed to seed product:", err)
		}
	}
}

func TestQuery_Filter_Eq(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)
	seedQueryProducts(t, wrapper, ctx)

	// Filter by category = Fruit
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Category": {Value: "Fruit", Operator: "eq"},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, err := wrapper.GetAll(ctx, []string{})
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 Fruit products, got %d", len(results))
	}

	for _, p := range results {
		if p.Category != "Fruit" {
			t.Errorf("Expected category Fruit, got %s", p.Category)
		}
	}
}

func TestQuery_Filter_Gt(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)
	seedQueryProducts(t, wrapper, ctx)

	// Filter by price > 75
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Price": {Value: "75", Operator: "gt"},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, err := wrapper.GetAll(ctx, []string{})
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	if len(results) != 3 {
		t.Errorf("Expected 3 products with price > 75, got %d", len(results))
	}

	for _, p := range results {
		if p.Price <= 75 {
			t.Errorf("Expected price > 75, got %d", p.Price)
		}
	}
}

func TestQuery_Filter_Like(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)
	seedQueryProducts(t, wrapper, ctx)

	// Filter by name LIKE %an%
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Name": {Value: "%an%", Operator: "like"},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, err := wrapper.GetAll(ctx, []string{})
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 products with 'an' in name (Banana, Eggplant), got %d", len(results))
	}
}

func TestQuery_Filter_NotAllowed(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)
	seedQueryProducts(t, wrapper, ctx)

	// Filter by ID (not in FilterableFields) - should be silently ignored
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"ID": {Value: "1", Operator: "eq"},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, err := wrapper.GetAll(ctx, []string{})
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Should return all 5 products (filter ignored)
	if len(results) != 5 {
		t.Errorf("Expected 5 products (ID filter ignored), got %d", len(results))
	}
}

func TestQuery_Sort_Direction(t *testing.T) {
	tests := []struct {
		name string
		desc bool
	}{
		{"ascending", false},
		{"descending", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, cleanup := setupQueryTestDB(t)
			defer cleanup()

			wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
			ctx := ctxWithQueryMeta(testQueryProductMeta)
			seedQueryProducts(t, wrapper, ctx)

			opts := &metadata.QueryOptions{
				Sort: []metadata.SortField{
					{Field: "Price", Desc: tt.desc},
				},
			}
			ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

			results, _, err := wrapper.GetAll(ctx, []string{})
			if err != nil {
				t.Fatal("GetAll failed:", err)
			}

			if len(results) < 2 {
				t.Fatal("Need at least 2 results to test sorting")
			}

			// Check sort order
			for i := 1; i < len(results); i++ {
				if tt.desc {
					if results[i].Price > results[i-1].Price {
						t.Errorf("Results not sorted descending: %d came after %d", results[i].Price, results[i-1].Price)
					}
				} else {
					if results[i].Price < results[i-1].Price {
						t.Errorf("Results not sorted ascending: %d came after %d", results[i].Price, results[i-1].Price)
					}
				}
			}
		})
	}
}

func TestQuery_Pagination_Limit(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)
	seedQueryProducts(t, wrapper, ctx)

	// Limit to 2 results
	opts := &metadata.QueryOptions{
		Limit: 2,
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, err := wrapper.GetAll(ctx, []string{})
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results with limit=2, got %d", len(results))
	}
}

func TestQuery_Pagination_Offset(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)
	seedQueryProducts(t, wrapper, ctx)

	// Sort by name for consistent ordering, then offset by 2
	opts := &metadata.QueryOptions{
		Sort: []metadata.SortField{
			{Field: "Name", Desc: false},
		},
		Offset: 2,
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, err := wrapper.GetAll(ctx, []string{})
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Should get 3 results (5 total - 2 offset)
	if len(results) != 3 {
		t.Errorf("Expected 3 results with offset=2, got %d", len(results))
	}

	// First result should be "Carrot" (3rd alphabetically after Apple, Banana)
	if results[0].Name != productCarrot {
		t.Errorf("Expected first result to be %s, got %s", productCarrot, results[0].Name)
	}
}

func TestQuery_Pagination_LimitAndOffset(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)
	seedQueryProducts(t, wrapper, ctx)

	// Sort by name, skip 1, take 2
	opts := &metadata.QueryOptions{
		Sort: []metadata.SortField{
			{Field: "Name", Desc: false},
		},
		Limit:  2,
		Offset: 1,
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, err := wrapper.GetAll(ctx, []string{})
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}

	// Should get Banana and Carrot (skip Apple, take 2)
	if results[0].Name != productBanana {
		t.Errorf("Expected first result to be %s, got %s", productBanana, results[0].Name)
	}
	if results[1].Name != productCarrot {
		t.Errorf("Expected second result to be %s, got %s", productCarrot, results[1].Name)
	}
}

func TestQuery_Count(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)
	seedQueryProducts(t, wrapper, ctx)

	// Request count with pagination
	opts := &metadata.QueryOptions{
		Limit:      2,
		CountTotal: true,
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, count, err := wrapper.GetAll(ctx, []string{})
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}

	if count != 5 {
		t.Errorf("Expected total count of 5, got %d", count)
	}
}

func TestQuery_CountWithFilter(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)
	seedQueryProducts(t, wrapper, ctx)

	// Filter by category = Fruit with count
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Category": {Value: "Fruit", Operator: "eq"},
		},
		Limit:      1,
		CountTotal: true,
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, count, err := wrapper.GetAll(ctx, []string{})
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
	}

	// Count should reflect filter (2 Fruit products)
	if count != 2 {
		t.Errorf("Expected filtered count of 2, got %d", count)
	}
}

func TestQuery_MaxLimitEnforced(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)
	seedQueryProducts(t, wrapper, ctx)

	// Request more than MaxLimit (100)
	opts := &metadata.QueryOptions{
		Limit: 1000,
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, err := wrapper.GetAll(ctx, []string{})
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Should return all 5 products (max limit is 100, but we only have 5)
	if len(results) != 5 {
		t.Errorf("Expected 5 results (max limit enforced but only 5 items), got %d", len(results))
	}
}

func TestQuery_DefaultLimit(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)

	// Seed more products than default limit (10)
	for i := 0; i < 15; i++ {
		p := TestQueryProduct{
			Name:     "Product" + string(rune('A'+i)),
			Category: "Test",
			Price:    i * 10,
			InStock:  true,
		}
		_, err := wrapper.Create(ctx, p)
		if err != nil {
			t.Fatal("Failed to seed product:", err)
		}
	}

	// No explicit limit - should use DefaultLimit (10)
	results, _, err := wrapper.GetAll(ctx, []string{})
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	if len(results) != 10 {
		t.Errorf("Expected 10 results (default limit), got %d", len(results))
	}
}

func TestQuery_CombinedFilterSortPagination(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)
	seedQueryProducts(t, wrapper, ctx)

	// Filter by Category=Fruit, sort by price desc, limit 1, offset 1, with count
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Category": {Value: "Fruit", Operator: "eq"},
		},
		Sort: []metadata.SortField{
			{Field: "Price", Desc: true},
		},
		Limit:      1,
		Offset:     1,
		CountTotal: true,
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, count, err := wrapper.GetAll(ctx, []string{})
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// 2 Fruit items: Apple(100), Banana(50)
	// Sorted by price desc: Apple(100), Banana(50)
	// Offset 1, limit 1: Banana
	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
	}

	if count != 2 {
		t.Errorf("Expected count of 2 Fruit items, got %d", count)
	}

	if len(results) > 0 && results[0].Name != productBanana {
		t.Errorf("Expected first result to be %s, got %s", productBanana, results[0].Name)
	}
}

func TestQuery_Sort_InvalidField(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)
	seedQueryProducts(t, wrapper, ctx)

	// Sort by ID (not in SortableFields) - should be ignored
	opts := &metadata.QueryOptions{
		Sort: []metadata.SortField{
			{Field: "ID", Desc: false},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, err := wrapper.GetAll(ctx, []string{})
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Should return all 5 products (invalid sort ignored)
	if len(results) != 5 {
		t.Errorf("Expected 5 products (invalid sort ignored), got %d", len(results))
	}
}

func TestQuery_DefaultSort_Direction(t *testing.T) {
	tests := []struct {
		name           string
		defaultSort    string
		expectedFirst  string
		expectedSecond string
	}{
		{
			name:           "ascending",
			defaultSort:    "Name",
			expectedFirst:  productApple,
			expectedSecond: productBanana,
		},
		{
			name:           "descending",
			defaultSort:    "-Name",
			expectedFirst:  productEggplant,
			expectedSecond: productDonut,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, cleanup := setupQueryTestDB(t)
			defer cleanup()

			meta := &metadata.TypeMetadata{
				TypeID:           "query_product_default_sort_" + tt.name,
				TypeName:         "TestQueryProduct",
				TableName:        "query_products",
				URLParamUUID:     "productId",
				ModelType:        reflect.TypeOf(TestQueryProduct{}),
				FilterableFields: []string{"Name", "Category", "Price"},
				SortableFields:   []string{"Name", "Price", "CreatedAt"},
				DefaultSort:      tt.defaultSort,
				DefaultLimit:     10,
				MaxLimit:         100,
			}

			wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
			ctx := ctxWithQueryMeta(meta)
			seedQueryProducts(t, wrapper, ctx)

			// No explicit sort - should use DefaultSort
			results, _, err := wrapper.GetAll(ctx, []string{})
			if err != nil {
				t.Fatal("GetAll failed:", err)
			}

			if len(results) < 2 {
				t.Fatal("Need at least 2 results to test default sorting")
			}

			if results[0].Name != tt.expectedFirst {
				t.Errorf("Expected first result to be %s, got %s", tt.expectedFirst, results[0].Name)
			}
			if results[1].Name != tt.expectedSecond {
				t.Errorf("Expected second result to be %s, got %s", tt.expectedSecond, results[1].Name)
			}
		})
	}
}

func TestQuery_MultipleSort(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)
	seedQueryProducts(t, wrapper, ctx)

	// Sort by multiple fields: Category asc, then Price desc
	opts := &metadata.QueryOptions{
		Sort: []metadata.SortField{
			{Field: "Name", Desc: false},
			{Field: "Price", Desc: true},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, err := wrapper.GetAll(ctx, []string{})
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	if len(results) != 5 {
		t.Errorf("Expected 5 results, got %d", len(results))
	}

	// First sort by name should put Apple first
	if results[0].Name != productApple {
		t.Errorf("Expected first result to be %s, got %s", productApple, results[0].Name)
	}
}
