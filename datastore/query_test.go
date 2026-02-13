package datastore_test

import (
	"context"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/metadata"
)

// Test constants to avoid duplication
const (
	productApple    = "Apple"
	productBanana   = "Banana"
	productCarrot   = "Carrot"
	productDonut    = "Donut"
	productEggplant = "Eggplant"

	categoryFruit     = "Fruit"
	categoryVegetable = "Vegetable"
	categoryBakery    = "Bakery"
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

func setupTestDBWithModel(t *testing.T, model any, dropModel any) (*datastore.SQLite, func()) {
	t.Helper()

	db, err := datastore.NewSQLite(":memory:")
	if err != nil {
		t.Fatal("Failed to create database:", err)
	}

	if err := datastore.Initialize(db); err != nil {
		db.Cleanup()
		t.Fatal("Failed to initialize datastore:", err)
	}

	_, err = db.GetDB().NewCreateTable().Model(model).IfNotExists().Exec(context.Background())
	if err != nil {
		datastore.Cleanup()
		db.Cleanup()
		t.Fatal("Failed to create table:", err)
	}

	return db, func() {
		_, _ = db.GetDB().NewDropTable().Model(dropModel).IfExists().Exec(context.Background())
		datastore.Cleanup()
		db.Cleanup()
	}
}

func setupQueryTestDB(t *testing.T) (*datastore.SQLite, func()) {
	return setupTestDBWithModel(t, (*TestQueryProduct)(nil), (*TestQueryProduct)(nil))
}

func ctxWithQueryMeta(meta *metadata.TypeMetadata) context.Context {
	return context.WithValue(context.Background(), metadata.MetadataKey, meta)
}

func seedQueryProducts(t *testing.T, wrapper *datastore.Wrapper[TestQueryProduct], ctx context.Context) {
	t.Helper()

	products := []TestQueryProduct{
		{Name: productApple, Category: categoryFruit, Price: 100, InStock: true},
		{Name: productBanana, Category: categoryFruit, Price: 50, InStock: true},
		{Name: productCarrot, Category: categoryVegetable, Price: 30, InStock: true},
		{Name: productDonut, Category: categoryBakery, Price: 150, InStock: false},
		{Name: productEggplant, Category: categoryVegetable, Price: 80, InStock: true},
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

	// Filter by category = categoryFruit
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Category": {Value: categoryFruit, Operator: metadata.OpEq},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 Fruit products, got %d", len(results))
	}

	for _, p := range results {
		if p.Category != categoryFruit {
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
			"Price": {Value: "75", Operator: metadata.OpGt},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, _, err := wrapper.GetAll(ctx)
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
			"Name": {Value: "%an%", Operator: metadata.OpLike},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, _, err := wrapper.GetAll(ctx)
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
			"ID": {Value: "1", Operator: metadata.OpEq},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, _, err := wrapper.GetAll(ctx)
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

			results, _, _, err := wrapper.GetAll(ctx)
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

	results, _, _, err := wrapper.GetAll(ctx)
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

	results, _, _, err := wrapper.GetAll(ctx)
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

	results, _, _, err := wrapper.GetAll(ctx)
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

	results, count, _, err := wrapper.GetAll(ctx)
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

	// Filter by category = categoryFruit with count
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Category": {Value: categoryFruit, Operator: metadata.OpEq},
		},
		Limit:      1,
		CountTotal: true,
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, count, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
	}

	// Count should reflect filter (2 categoryFruit products)
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

	results, _, _, err := wrapper.GetAll(ctx)
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
	results, _, _, err := wrapper.GetAll(ctx)
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

	// Filter by Category=categoryFruit, sort by price desc, limit 1, offset 1, with count
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Category": {Value: categoryFruit, Operator: metadata.OpEq},
		},
		Sort: []metadata.SortField{
			{Field: "Price", Desc: true},
		},
		Limit:      1,
		Offset:     1,
		CountTotal: true,
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, count, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// 2 categoryFruit items: Apple(100), Banana(50)
	// Sorted by price desc: Apple(100), Banana(50)
	// Offset 1, limit 1: Banana
	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
	}

	if count != 2 {
		t.Errorf("Expected count of 2 categoryFruit items, got %d", count)
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

	results, _, _, err := wrapper.GetAll(ctx)
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
			results, _, _, err := wrapper.GetAll(ctx)
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

	results, _, _, err := wrapper.GetAll(ctx)
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

func TestQuery_Filter_Neq(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)
	seedQueryProducts(t, wrapper, ctx)

	// Filter by category != categoryFruit
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Category": {Value: categoryFruit, Operator: metadata.OpNeq},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	if len(results) != 3 {
		t.Errorf("Expected 3 non-Fruit products, got %d", len(results))
	}

	for _, p := range results {
		if p.Category == categoryFruit {
			t.Errorf("Expected non-Fruit category, got %s", p.Category)
		}
	}
}

func TestQuery_Filter_Gte(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)
	seedQueryProducts(t, wrapper, ctx)

	// Filter by price >= 80
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Price": {Value: "80", Operator: metadata.OpGte},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	if len(results) != 3 {
		t.Errorf("Expected 3 products with price >= 80 (Apple=100, Eggplant=80, Donut=150), got %d", len(results))
	}

	for _, p := range results {
		if p.Price < 80 {
			t.Errorf("Expected price >= 80, got %d", p.Price)
		}
	}
}

func TestQuery_Filter_Lt(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)
	seedQueryProducts(t, wrapper, ctx)

	// Filter by price < 80
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Price": {Value: "80", Operator: metadata.OpLt},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 products with price < 80 (Banana=50, Carrot=30), got %d", len(results))
	}

	for _, p := range results {
		if p.Price >= 80 {
			t.Errorf("Expected price < 80, got %d", p.Price)
		}
	}
}

func TestQuery_Filter_Lte(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)
	seedQueryProducts(t, wrapper, ctx)

	// Filter by price <= 80
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Price": {Value: "80", Operator: metadata.OpLte},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	if len(results) != 3 {
		t.Errorf("Expected 3 products with price <= 80 (Banana=50, Carrot=30, Eggplant=80), got %d", len(results))
	}

	for _, p := range results {
		if p.Price > 80 {
			t.Errorf("Expected price <= 80, got %d", p.Price)
		}
	}
}

func TestQuery_Filter_In(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)
	seedQueryProducts(t, wrapper, ctx)

	// Filter by category IN (Fruit, Bakery)
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Category": {Value: categoryFruit + "," + categoryBakery, Operator: metadata.OpIn},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	if len(results) != 3 {
		t.Errorf("Expected 3 products with category IN (Fruit, Bakery), got %d", len(results))
	}

	for _, p := range results {
		if p.Category != categoryFruit && p.Category != categoryBakery {
			t.Errorf("Expected category Fruit or Bakery, got %s", p.Category)
		}
	}
}

func TestQuery_Filter_Nin(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)
	seedQueryProducts(t, wrapper, ctx)

	// Filter by category NOT IN (Fruit, Bakery)
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Category": {Value: categoryFruit + "," + categoryBakery, Operator: metadata.OpNin},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 products with category NOT IN (Fruit, Bakery), got %d", len(results))
	}

	for _, p := range results {
		if p.Category == categoryFruit || p.Category == categoryBakery {
			t.Errorf("Expected category NOT Fruit or Bakery, got %s", p.Category)
		}
	}
}

func TestQuery_Filter_Bt(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)
	seedQueryProducts(t, wrapper, ctx)

	// Filter by price BETWEEN 50 AND 100
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Price": {Value: "50,100", Operator: metadata.OpBt},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Apple=100, Banana=50, Eggplant=80 are between 50-100
	if len(results) != 3 {
		t.Errorf("Expected 3 products with price BETWEEN 50 AND 100, got %d", len(results))
	}

	for _, p := range results {
		if p.Price < 50 || p.Price > 100 {
			t.Errorf("Expected price between 50 and 100, got %d", p.Price)
		}
	}
}

func TestQuery_Filter_Nbt(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)
	seedQueryProducts(t, wrapper, ctx)

	// Filter by price NOT BETWEEN 50 AND 100
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Price": {Value: "50,100", Operator: metadata.OpNbt},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Carrot=30, Donut=150 are NOT between 50-100
	if len(results) != 2 {
		t.Errorf("Expected 2 products with price NOT BETWEEN 50 AND 100, got %d", len(results))
	}

	for _, p := range results {
		if p.Price >= 50 && p.Price <= 100 {
			t.Errorf("Expected price outside 50-100 range, got %d", p.Price)
		}
	}
}

func TestQuery_Filter_Bool_True(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)
	seedQueryProducts(t, wrapper, ctx)

	// Filter by InStock = true (string "true" should convert to bool)
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"InStock": {Value: "true", Operator: metadata.OpEq},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Apple, Banana, Carrot, Eggplant are in stock (4 products)
	if len(results) != 4 {
		t.Errorf("Expected 4 products with InStock=true, got %d", len(results))
	}

	for _, p := range results {
		if !p.InStock {
			t.Errorf("Expected InStock=true, got false for %s", p.Name)
		}
	}
}

func TestQuery_Filter_Bool_False(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)
	seedQueryProducts(t, wrapper, ctx)

	// Filter by InStock = false (string "false" should convert to bool)
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"InStock": {Value: "false", Operator: metadata.OpEq},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Only Donut is not in stock (1 product)
	if len(results) != 1 {
		t.Errorf("Expected 1 product with InStock=false, got %d", len(results))
	}

	for _, p := range results {
		if p.InStock {
			t.Errorf("Expected InStock=false, got true for %s", p.Name)
		}
	}
}

// seedTypeConversionProducts creates products with edge-case values for type conversion testing
func seedTypeConversionProducts(t *testing.T, wrapper *datastore.Wrapper[TestQueryProduct], ctx context.Context) {
	t.Helper()

	products := []TestQueryProduct{
		{Name: "123", Category: "true", Price: 100, InStock: true},       // numeric name, "true" category
		{Name: "456", Category: "false", Price: 200, InStock: false},     // numeric name, "false" category
		{Name: "Normal", Category: "Regular", Price: 100, InStock: true}, // normal values, same price as first
	}

	for _, p := range products {
		_, err := wrapper.Create(ctx, p)
		if err != nil {
			t.Fatal("Failed to seed product:", err)
		}
	}
}

func TestQuery_Filter_Int_StringValue(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)
	seedTypeConversionProducts(t, wrapper, ctx)

	// Filter by Price = "100" (string should convert to int 100)
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Price": {Value: "100", Operator: metadata.OpEq},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Products "123" and "Normal" both have Price=100
	if len(results) != 2 {
		t.Errorf("Expected 2 products with Price=100, got %d", len(results))
	}

	for _, p := range results {
		if p.Price != 100 {
			t.Errorf("Expected Price=100, got %d for %s", p.Price, p.Name)
		}
	}
}

func TestQuery_Filter_String_NumericLookingValue(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)
	seedTypeConversionProducts(t, wrapper, ctx)

	// Filter by Name = "123" (should stay as string, not convert to int)
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Name": {Value: "123", Operator: metadata.OpEq},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 product with Name='123', got %d", len(results))
	}

	if len(results) > 0 && results[0].Name != "123" {
		t.Errorf("Expected Name='123', got %s", results[0].Name)
	}
}

func TestQuery_Filter_String_TrueLookingValue(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)
	seedTypeConversionProducts(t, wrapper, ctx)

	// Filter by Category = "true" (should stay as string, not convert to bool)
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Category": {Value: "true", Operator: metadata.OpEq},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 product with Category='true', got %d", len(results))
	}

	if len(results) > 0 && results[0].Category != "true" {
		t.Errorf("Expected Category='true', got %s", results[0].Category)
	}
}

func TestQuery_Filter_String_FalseLookingValue(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)
	seedTypeConversionProducts(t, wrapper, ctx)

	// Filter by Category = "false" (should stay as string, not convert to bool)
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Category": {Value: "false", Operator: metadata.OpEq},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 product with Category='false', got %d", len(results))
	}

	if len(results) > 0 && results[0].Category != "false" {
		t.Errorf("Expected Category='false', got %s", results[0].Category)
	}
}

func TestQuery_Filter_Bool_OneValue(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)
	seedQueryProducts(t, wrapper, ctx)

	// Filter by InStock = "1" (should convert to true)
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"InStock": {Value: "1", Operator: metadata.OpEq},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Should get all in-stock products (Apple, Banana, Carrot, Eggplant)
	if len(results) != 4 {
		t.Errorf("Expected 4 in-stock products with '1' filter, got %d", len(results))
	}

	for _, p := range results {
		if !p.InStock {
			t.Errorf("Expected InStock=true, got false for %s", p.Name)
		}
	}
}

func TestQuery_Filter_Int_InvalidValue(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)
	seedQueryProducts(t, wrapper, ctx)

	// Filter by Price = "notanumber" (should log warning, use string comparison which won't match)
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Price": {Value: "notanumber", Operator: metadata.OpEq},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// No products should match since "notanumber" won't equal any int price
	if len(results) != 0 {
		t.Errorf("Expected 0 products with invalid int filter, got %d", len(results))
	}
}

func TestQuery_Filter_Bt_SingleValue(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMeta)
	seedQueryProducts(t, wrapper, ctx)

	// Filter with bt operator but only one value - should use zero value for second
	// Price bt 50 (with zero as second value) means Price BETWEEN 50 AND 0
	// SQL BETWEEN requires first <= second, so this matches nothing
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Price": {Value: "50", Operator: metadata.OpBt},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// BETWEEN 50 AND 0 matches nothing (first value > second value)
	// This tests the documented edge case behavior
	if len(results) != 0 {
		t.Errorf("Expected 0 products with bt single value (BETWEEN 50 AND 0), got %d", len(results))
	}
}

// TestNumericProduct is a test model with various numeric types
type TestNumericProduct struct {
	bun.BaseModel `bun:"table:numeric_products"`
	ID            int     `bun:"id,pk,autoincrement" json:"id"`
	Name          string  `bun:"name,notnull" json:"name"`
	Rating        float64 `bun:"rating,notnull" json:"rating"`
	Stock         uint    `bun:"stock,notnull" json:"stock"`
}

var testNumericProductMeta = &metadata.TypeMetadata{
	TypeID:           "numeric_product_id",
	TypeName:         "TestNumericProduct",
	TableName:        "numeric_products",
	URLParamUUID:     "productId",
	ModelType:        reflect.TypeOf(TestNumericProduct{}),
	FilterableFields: []string{"Name", "Rating", "Stock"},
	SortableFields:   []string{"Name", "Rating", "Stock"},
	DefaultLimit:     10,
	MaxLimit:         100,
}

func setupNumericTestDB(t *testing.T) (*datastore.SQLite, func()) {
	return setupTestDBWithModel(t, (*TestNumericProduct)(nil), (*TestNumericProduct)(nil))
}

func seedNumericProducts(t *testing.T, wrapper *datastore.Wrapper[TestNumericProduct], ctx context.Context) {
	t.Helper()

	products := []TestNumericProduct{
		{Name: "HighRated", Rating: 4.5, Stock: 100},
		{Name: "LowRated", Rating: 2.0, Stock: 50},
		{Name: "MidRated", Rating: 3.5, Stock: 75},
	}

	for _, p := range products {
		_, err := wrapper.Create(ctx, p)
		if err != nil {
			t.Fatal("Failed to seed numeric product:", err)
		}
	}
}

func TestQuery_Filter_Float_Gt(t *testing.T) {
	db, cleanup := setupNumericTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestNumericProduct]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, testNumericProductMeta)
	seedNumericProducts(t, wrapper, ctx)

	// Filter by Rating > 3.0
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Rating": {Value: "3.0", Operator: metadata.OpGt},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Should get HighRated (4.5) and MidRated (3.5)
	if len(results) != 2 {
		t.Errorf("Expected 2 products with Rating > 3.0, got %d", len(results))
	}
}

func TestQuery_Filter_Float_InvalidValue(t *testing.T) {
	db, cleanup := setupNumericTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestNumericProduct]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, testNumericProductMeta)
	seedNumericProducts(t, wrapper, ctx)

	// Filter by Rating = "notafloat"
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Rating": {Value: "notafloat", Operator: metadata.OpEq},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// No products should match
	if len(results) != 0 {
		t.Errorf("Expected 0 products with invalid float filter, got %d", len(results))
	}
}

func TestQuery_Filter_Uint_Gte(t *testing.T) {
	db, cleanup := setupNumericTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestNumericProduct]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, testNumericProductMeta)
	seedNumericProducts(t, wrapper, ctx)

	// Filter by Stock >= 75
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Stock": {Value: "75", Operator: metadata.OpGte},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Should get HighRated (100) and MidRated (75)
	if len(results) != 2 {
		t.Errorf("Expected 2 products with Stock >= 75, got %d", len(results))
	}
}

func TestQuery_Filter_Uint_InvalidValue(t *testing.T) {
	db, cleanup := setupNumericTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestNumericProduct]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, testNumericProductMeta)
	seedNumericProducts(t, wrapper, ctx)

	// Filter by Stock = "notauint"
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Stock": {Value: "notauint", Operator: metadata.OpEq},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// No products should match
	if len(results) != 0 {
		t.Errorf("Expected 0 products with invalid uint filter, got %d", len(results))
	}
}

func TestQuery_Filter_Float_Between(t *testing.T) {
	db, cleanup := setupNumericTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestNumericProduct]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, testNumericProductMeta)
	seedNumericProducts(t, wrapper, ctx)

	// Filter by Rating between 2.5 and 4.0
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Rating": {Value: "2.5,4.0", Operator: metadata.OpBt},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Should get MidRated (3.5)
	if len(results) != 1 {
		t.Errorf("Expected 1 product with Rating between 2.5 and 4.0, got %d", len(results))
	}
}

func TestQuery_Filter_Bt_UnknownField(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}

	// Create metadata that allows filtering on a field that doesn't exist in the model
	badMeta := &metadata.TypeMetadata{
		TypeID:           "query_product_id",
		TypeName:         "TestQueryProduct",
		TableName:        "query_products",
		URLParamUUID:     "productId",
		ModelType:        reflect.TypeOf(TestQueryProduct{}),
		FilterableFields: []string{"NonExistentField"},
		SortableFields:   []string{"Name"},
		DefaultLimit:     10,
		MaxLimit:         100,
	}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, badMeta)
	seedQueryProducts(t, wrapper, ctx)

	// Filter on non-existent field with bt operator to trigger getZeroValue edge case
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"NonExistentField": {Value: "50", Operator: metadata.OpBt},
		},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	// This will fail to find the column name, but tests the code path
	results, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		// Expected - field doesn't exist
		return
	}

	// If no error, we should get all products since filter couldn't be applied
	if len(results) != 5 {
		t.Errorf("Expected 5 products (filter skipped), got %d", len(results))
	}
}

// testQueryProductMetaWithSums is metadata with SummableFields configured
var testQueryProductMetaWithSums = &metadata.TypeMetadata{
	TypeID:           "query_product_sum_id",
	TypeName:         "TestQueryProduct",
	TableName:        "query_products",
	URLParamUUID:     "productId",
	ModelType:        reflect.TypeOf(TestQueryProduct{}),
	FilterableFields: []string{"Name", "Category", "Price", "InStock"},
	SortableFields:   []string{"Name", "Price", "CreatedAt"},
	SummableFields:   []string{"Price", "Name", "InStock"},
	DefaultLimit:     10,
	MaxLimit:         100,
}

func TestQuery_Sum_SingleField(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMetaWithSums)
	seedQueryProducts(t, wrapper, ctx)

	// Request sum of Price field
	opts := &metadata.QueryOptions{
		Sums: []string{"Price"},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	_, _, sums, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Total price: Apple=100 + Banana=50 + Carrot=30 + Donut=150 + Eggplant=80 = 410
	expectedSum := 410.0
	if sums["Price"] != expectedSum {
		t.Errorf("Expected sum of Price to be %v, got %v", expectedSum, sums["Price"])
	}
}

func TestQuery_Sum_MultipleFields(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMetaWithSums)
	seedQueryProducts(t, wrapper, ctx)

	// Request sum of Price (int) and Name (varchar — SQLite returns 0 for non-numeric text)
	opts := &metadata.QueryOptions{
		Sums: []string{"Price", "Name"},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	_, _, sums, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	expectedPriceSum := 410.0
	if sums["Price"] != expectedPriceSum {
		t.Errorf("Expected sum of Price to be %v, got %v", expectedPriceSum, sums["Price"])
	}

	// SQLite: non-numeric text sums to 0
	if sums["Name"] != 0 {
		t.Errorf("Expected sum of Name (varchar) to be 0, got %v", sums["Name"])
	}
}

func TestQuery_Sum_WithFilter(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMetaWithSums)
	seedQueryProducts(t, wrapper, ctx)

	// Request sum of Price with filter Category=Fruit
	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Category": {Value: categoryFruit, Operator: metadata.OpEq},
		},
		Sums: []string{"Price"},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	_, _, sums, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Only Fruit products: Apple=100 + Banana=50 = 150
	expectedSum := 150.0
	if sums["Price"] != expectedSum {
		t.Errorf("Expected sum of Price (filtered) to be %v, got %v", expectedSum, sums["Price"])
	}
}

func TestQuery_Sum_WithCount(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMetaWithSums)
	seedQueryProducts(t, wrapper, ctx)

	// Request both count and sum - should be combined into one query
	opts := &metadata.QueryOptions{
		CountTotal: true,
		Sums:       []string{"Price"},
		Limit:      2, // Pagination shouldn't affect sum/count
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, count, sums, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Results should be limited to 2
	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}

	// Count should be total (5)
	if count != 5 {
		t.Errorf("Expected count of 5, got %d", count)
	}

	// Sum should be for all items (410), not just the paginated ones
	expectedSum := 410.0
	if sums["Price"] != expectedSum {
		t.Errorf("Expected sum of Price to be %v, got %v", expectedSum, sums["Price"])
	}
}

func TestQuery_Sum_VarcharField_NoPanic(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMetaWithSums)
	seedQueryProducts(t, wrapper, ctx)

	// Summing a varchar field should not panic.
	// SQLite returns 0 for non-numeric text; PostgreSQL would return a database error.
	opts := &metadata.QueryOptions{
		Sums: []string{"Name"},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	_, _, sums, err := wrapper.GetAll(ctx)
	if err != nil {
		// Database error is acceptable (PostgreSQL rejects SUM on text)
		return
	}

	// SQLite: non-numeric text values sum to 0
	if sums["Name"] != 0 {
		t.Errorf("Expected sum of Name (varchar) to be 0, got %v", sums["Name"])
	}
}

func TestQuery_Sum_BoolField(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMetaWithSums)
	seedQueryProducts(t, wrapper, ctx)

	// Bool fields are now passed to the database — SUM counts true values (stored as 1)
	opts := &metadata.QueryOptions{
		Sums: []string{"InStock"},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	_, _, sums, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// 4 products with InStock=true, 1 with false → SUM = 4
	expectedSum := 4.0
	if sums["InStock"] != expectedSum {
		t.Errorf("Expected sum of InStock (bool) to be %v, got %v", expectedSum, sums["InStock"])
	}
}

func TestQuery_Sum_FieldNotInAllowlist(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMetaWithSums)
	seedQueryProducts(t, wrapper, ctx)

	// Request sum of CreatedAt (not in SummableFields)
	opts := &metadata.QueryOptions{
		Sums: []string{"CreatedAt"},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	_, _, sums, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Field not in allowlist should return 0 (with slog warning)
	if sums["CreatedAt"] != 0 {
		t.Errorf("Expected sum of CreatedAt (not allowed) to be 0, got %v", sums["CreatedAt"])
	}
}

func TestQuery_Sum_NonExistentField(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMetaWithSums)
	seedQueryProducts(t, wrapper, ctx)

	// Request sum of a field that doesn't exist
	opts := &metadata.QueryOptions{
		Sums: []string{"NonExistentField"},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	_, _, sums, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Non-existent field should return 0 (with slog warning)
	if sums["NonExistentField"] != 0 {
		t.Errorf("Expected sum of NonExistentField to be 0, got %v", sums["NonExistentField"])
	}
}

func TestQuery_Sum_MixedValidAndInvalid(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMetaWithSums)
	seedQueryProducts(t, wrapper, ctx)

	// Price (int) and Name (varchar) — both pass to DB; Name sums to 0 in SQLite
	opts := &metadata.QueryOptions{
		Sums: []string{"Price", "Name"},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	_, _, sums, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	expectedPriceSum := 410.0
	if sums["Price"] != expectedPriceSum {
		t.Errorf("Expected sum of Price to be %v, got %v", expectedPriceSum, sums["Price"])
	}

	if sums["Name"] != 0 {
		t.Errorf("Expected sum of Name (varchar) to be 0, got %v", sums["Name"])
	}
}

func TestQuery_Sum_NoSumsRequested(t *testing.T) {
	db, cleanup := setupQueryTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestQueryProduct]{Store: db}
	ctx := ctxWithQueryMeta(testQueryProductMetaWithSums)
	seedQueryProducts(t, wrapper, ctx)

	// No sums requested - should return nil map
	opts := &metadata.QueryOptions{}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	_, _, sums, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Should return nil or empty map when no sums requested
	if len(sums) > 0 {
		t.Errorf("Expected nil or empty sums map when no sums requested, got %v", sums)
	}
}

// TestDecimalProduct is a test model with shopspring/decimal fields to verify
// that struct-based numeric types work with WithSums (issue #50)
type TestDecimalProduct struct {
	bun.BaseModel `bun:"table:decimal_products"`
	ID            int             `bun:"id,pk,autoincrement" json:"id"`
	Name          string          `bun:"name,notnull" json:"name"`
	Category      string          `bun:"category,notnull" json:"category"`
	UnitPrice     decimal.Decimal `bun:"unit_price,notnull,type:decimal(10,2)" json:"unitPrice"`
	TaxAmount     decimal.Decimal `bun:"tax_amount,notnull,type:decimal(10,2)" json:"taxAmount"`
}

var testDecimalProductMeta = &metadata.TypeMetadata{
	TypeID:           "decimal_product_id",
	TypeName:         "TestDecimalProduct",
	TableName:        "decimal_products",
	URLParamUUID:     "productId",
	ModelType:        reflect.TypeOf(TestDecimalProduct{}),
	FilterableFields: []string{"Name", "Category"},
	SortableFields:   []string{"Name", "UnitPrice"},
	SummableFields:   []string{"UnitPrice", "TaxAmount"},
	DefaultLimit:     10,
	MaxLimit:         100,
}

func setupDecimalTestDB(t *testing.T) (*datastore.SQLite, func()) {
	return setupTestDBWithModel(t, (*TestDecimalProduct)(nil), (*TestDecimalProduct)(nil))
}

func seedDecimalProducts(t *testing.T, wrapper *datastore.Wrapper[TestDecimalProduct], ctx context.Context) {
	t.Helper()

	products := []TestDecimalProduct{
		{Name: "Widget", Category: "Hardware", UnitPrice: decimal.NewFromFloat(29.99), TaxAmount: decimal.NewFromFloat(3.00)},
		{Name: "Gadget", Category: "Hardware", UnitPrice: decimal.NewFromFloat(49.95), TaxAmount: decimal.NewFromFloat(5.00)},
		{Name: "Service", Category: "Software", UnitPrice: decimal.NewFromFloat(199.00), TaxAmount: decimal.NewFromFloat(19.90)},
		{Name: "License", Category: "Software", UnitPrice: decimal.NewFromFloat(99.50), TaxAmount: decimal.NewFromFloat(9.95)},
	}

	for _, p := range products {
		_, err := wrapper.Create(ctx, p)
		if err != nil {
			t.Fatal("Failed to seed decimal product:", err)
		}
	}
}

func TestQuery_Sum_DecimalField(t *testing.T) {
	db, cleanup := setupDecimalTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestDecimalProduct]{Store: db}
	ctx := ctxWithQueryMeta(testDecimalProductMeta)
	seedDecimalProducts(t, wrapper, ctx)

	opts := &metadata.QueryOptions{
		Sums: []string{"UnitPrice"},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	_, _, sums, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// 29.99 + 49.95 + 199.00 + 99.50 = 378.44
	expectedSum := 378.44
	if sums["UnitPrice"] != expectedSum {
		t.Errorf("Expected sum of UnitPrice to be %v, got %v", expectedSum, sums["UnitPrice"])
	}
}

func TestQuery_Sum_MultipleDecimalFields(t *testing.T) {
	db, cleanup := setupDecimalTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestDecimalProduct]{Store: db}
	ctx := ctxWithQueryMeta(testDecimalProductMeta)
	seedDecimalProducts(t, wrapper, ctx)

	opts := &metadata.QueryOptions{
		Sums: []string{"UnitPrice", "TaxAmount"},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	_, _, sums, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	expectedPrice := 378.44
	if sums["UnitPrice"] != expectedPrice {
		t.Errorf("Expected sum of UnitPrice to be %v, got %v", expectedPrice, sums["UnitPrice"])
	}

	// 3.00 + 5.00 + 19.90 + 9.95 = 37.85
	expectedTax := 37.85
	if math.Abs(sums["TaxAmount"]-expectedTax) > 0.001 {
		t.Errorf("Expected sum of TaxAmount to be %v, got %v", expectedTax, sums["TaxAmount"])
	}
}

func TestQuery_Sum_DecimalField_WithFilter(t *testing.T) {
	db, cleanup := setupDecimalTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestDecimalProduct]{Store: db}
	ctx := ctxWithQueryMeta(testDecimalProductMeta)
	seedDecimalProducts(t, wrapper, ctx)

	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Category": {Value: "Software", Operator: metadata.OpEq},
		},
		Sums: []string{"UnitPrice"},
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	_, _, sums, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	// Software only: Service=199.00 + License=99.50 = 298.50
	expectedSum := 298.50
	if sums["UnitPrice"] != expectedSum {
		t.Errorf("Expected sum of UnitPrice (filtered) to be %v, got %v", expectedSum, sums["UnitPrice"])
	}
}

func TestQuery_Sum_DecimalField_WithCount(t *testing.T) {
	db, cleanup := setupDecimalTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestDecimalProduct]{Store: db}
	ctx := ctxWithQueryMeta(testDecimalProductMeta)
	seedDecimalProducts(t, wrapper, ctx)

	opts := &metadata.QueryOptions{
		CountTotal: true,
		Sums:       []string{"UnitPrice"},
		Limit:      2,
	}
	ctx = context.WithValue(ctx, metadata.QueryOptionsKey, opts)

	results, count, sums, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll failed:", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}

	if count != 4 {
		t.Errorf("Expected count of 4, got %d", count)
	}

	// Sum should be for all items, not just paginated
	expectedSum := 378.44
	if sums["UnitPrice"] != expectedSum {
		t.Errorf("Expected sum of UnitPrice to be %v, got %v", expectedSum, sums["UnitPrice"])
	}
}
