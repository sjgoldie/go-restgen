package datastore

import (
	"context"
	"reflect"
	"testing"

	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/metadata"
)

// setupHelperTestDB creates an in-memory SQLite DB for helper tests
func setupHelperTestDB(t *testing.T) (*SQLite, func()) {
	t.Helper()
	db, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal("Failed to create test database:", err)
	}
	cleanup := func() { db.Cleanup() }
	return db, cleanup
}

func TestParseRelationPath(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantRels  []string
		wantField string
	}{
		{"direct field", "Status", nil, "Status"},
		{"one level", "Account.Status", []string{"Account"}, "Status"},
		{"two levels", "Account.User.Email", []string{"Account", "User"}, "Email"},
		{"three levels", "Account.User.Profile.Name", []string{"Account", "User", "Profile"}, "Name"},
		{"four levels", "A.B.C.D.Field", []string{"A", "B", "C", "D"}, "Field"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRelationPath(tt.input)
			if got.field != tt.wantField {
				t.Errorf("field = %q, want %q", got.field, tt.wantField)
			}
			if len(got.relations) != len(tt.wantRels) {
				t.Errorf("relations = %v, want %v", got.relations, tt.wantRels)
				return
			}
			for i, r := range got.relations {
				if r != tt.wantRels[i] {
					t.Errorf("relations[%d] = %q, want %q", i, r, tt.wantRels[i])
				}
			}
		})
	}
}

// Test types for getRelationNameForParent
type testParentType struct {
	bun.BaseModel `bun:"table:parents"`
	ID            int `bun:"id,pk"`
}

type testChildType struct {
	bun.BaseModel `bun:"table:children"`
	ID            int             `bun:"id,pk"`
	ParentID      int             `bun:"parent_id"`
	Parent        *testParentType `bun:"rel:belongs-to,join:parent_id=id"`
}

type testChildCustomName struct {
	bun.BaseModel `bun:"table:children"`
	ID            int             `bun:"id,pk"`
	MyParentID    int             `bun:"my_parent_id"`
	MyParent      *testParentType `bun:"rel:belongs-to,join:my_parent_id=id"`
}

// Type with no bun relation tag
type testChildNoRelation struct {
	bun.BaseModel `bun:"table:children"`
	ID            int `bun:"id,pk"`
	ParentID      int `bun:"parent_id"`
}

// Type with wrong parent type in relation
type testOtherParent struct {
	bun.BaseModel `bun:"table:other_parents"`
	ID            int `bun:"id,pk"`
}

type testChildWrongParent struct {
	bun.BaseModel `bun:"table:children"`
	ID            int              `bun:"id,pk"`
	OtherID       int              `bun:"other_id"`
	Other         *testOtherParent `bun:"rel:belongs-to,join:other_id=id"`
}

func TestGetRelationNameForParent(t *testing.T) {
	db, cleanup := setupHelperTestDB(t)
	defer cleanup()

	// Register models with Bun so schema is populated
	db.GetDB().RegisterModel((*testParentType)(nil))
	db.GetDB().RegisterModel((*testChildType)(nil))
	db.GetDB().RegisterModel((*testChildCustomName)(nil))
	db.GetDB().RegisterModel((*testChildNoRelation)(nil))
	db.GetDB().RegisterModel((*testOtherParent)(nil))
	db.GetDB().RegisterModel((*testChildWrongParent)(nil))

	wrapper := &Wrapper[testChildType]{Store: db}

	parentMeta := &metadata.TypeMetadata{
		TypeName:  "testParentType",
		ModelType: reflect.TypeOf(testParentType{}),
	}

	tests := []struct {
		name      string
		childType reflect.Type
		want      string
	}{
		{
			name:      "finds relation via bun schema",
			childType: reflect.TypeOf(testChildType{}),
			want:      "Parent",
		},
		{
			name:      "finds custom named relation via bun schema",
			childType: reflect.TypeOf(testChildCustomName{}),
			want:      "MyParent",
		},
		{
			name:      "fallback to type name when no matching relation",
			childType: reflect.TypeOf(testChildNoRelation{}),
			want:      "testParentType",
		},
		{
			name:      "fallback when relation points to different type",
			childType: reflect.TypeOf(testChildWrongParent{}),
			want:      "testParentType",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			childMeta := &metadata.TypeMetadata{
				ModelType: tt.childType,
			}
			got := wrapper.getRelationNameForParent(childMeta, parentMeta)
			if got != tt.want {
				t.Errorf("getRelationNameForParent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetRelationNameForParent_Fallback(t *testing.T) {
	db, cleanup := setupHelperTestDB(t)
	defer cleanup()

	db.GetDB().RegisterModel((*testChildNoRelation)(nil))

	wrapper := &Wrapper[testChildNoRelation]{Store: db}

	tests := []struct {
		name     string
		typeName string
		want     string
	}{
		{"strips package prefix", "myapp.Account", "Account"},
		{"strips Rel prefix", "RelAccount", "Account"},
		{"strips both prefixes", "datastore.RelUser", "User"},
		{"no prefix", "Blog", "Blog"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			childMeta := &metadata.TypeMetadata{
				ModelType: reflect.TypeOf(testChildNoRelation{}),
			}
			parentMeta := &metadata.TypeMetadata{
				TypeName:  tt.typeName,
				ModelType: reflect.TypeOf(struct{}{}), // won't match any child field
			}
			got := wrapper.getRelationNameForParent(childMeta, parentMeta)
			if got != tt.want {
				t.Errorf("getRelationNameForParent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMatchesParentName(t *testing.T) {
	db, cleanup := setupHelperTestDB(t)
	defer cleanup()

	db.GetDB().RegisterModel((*testParentType)(nil))
	db.GetDB().RegisterModel((*testChildType)(nil))
	db.GetDB().RegisterModel((*testChildNoRelation)(nil))

	wrapper := &Wrapper[testChildType]{Store: db}

	tests := []struct {
		name        string
		childType   reflect.Type
		parentType  string
		relName     string
		explicitRel string
		want        bool
	}{
		{
			name:       "matches via bun schema",
			childType:  reflect.TypeOf(testChildType{}),
			parentType: "testParentType",
			relName:    "Parent",
			want:       true,
		},
		{
			name:       "matches case insensitive",
			childType:  reflect.TypeOf(testChildType{}),
			parentType: "testParentType",
			relName:    "parent",
			want:       true,
		},
		{
			name:        "matches explicit RelationName",
			childType:   reflect.TypeOf(testChildNoRelation{}),
			parentType:  "SomeType",
			relName:     "MyRelation",
			explicitRel: "MyRelation",
			want:        true,
		},
		{
			name:       "no match",
			childType:  reflect.TypeOf(testChildType{}),
			parentType: "testParentType",
			relName:    "WrongName",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			childMeta := &metadata.TypeMetadata{
				ModelType: tt.childType,
			}
			parentMeta := &metadata.TypeMetadata{
				TypeName:     tt.parentType,
				ModelType:    reflect.TypeOf(testParentType{}),
				RelationName: tt.explicitRel,
			}
			got := wrapper.matchesParentName(childMeta, parentMeta, tt.relName)
			if got != tt.want {
				t.Errorf("matchesParentName() = %v, want %v", got, tt.want)
			}
		})
	}
}

type filterTestItem struct {
	bun.BaseModel `bun:"table:filter_items"`
	ID            int    `bun:"id,pk,autoincrement"`
	Name          string `bun:"name"`
	Status        string `bun:"status"`
	Amount        int    `bun:"amount"`
	Category      string `bun:"category"`
}

func TestApplyFilter_WithTableName(t *testing.T) {
	db, cleanup := setupHelperTestDB(t)
	defer cleanup()

	ctx := context.Background()
	_, err := db.GetDB().NewCreateTable().Model((*filterTestItem)(nil)).IfNotExists().Exec(ctx)
	if err != nil {
		t.Fatal(err)
	}

	items := []filterTestItem{
		{Name: "alice", Status: "active", Amount: 100, Category: "A"},
		{Name: "bob", Status: "inactive", Amount: 50, Category: "B"},
		{Name: "charlie", Status: "active", Amount: 200, Category: "A"},
		{Name: "diana", Status: "pending", Amount: 75, Category: "C"},
	}
	_, err = db.GetDB().NewInsert().Model(&items).Exec(ctx)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		col       string
		operator  string
		vals      []interface{}
		wantCount int
	}{
		{"eq", "name", metadata.OpEq, []interface{}{"alice"}, 1},
		{"empty op defaults to eq", "name", "", []interface{}{"bob"}, 1},
		{"neq", "status", metadata.OpNeq, []interface{}{"active"}, 2},
		{"gt", "amount", metadata.OpGt, []interface{}{75}, 2},
		{"gte", "amount", metadata.OpGte, []interface{}{75}, 3},
		{"lt", "amount", metadata.OpLt, []interface{}{100}, 2},
		{"lte", "amount", metadata.OpLte, []interface{}{100}, 3},
		{"like", "name", metadata.OpLike, []interface{}{"a%"}, 1},
		{"ilike", "name", metadata.OpIlike, []interface{}{"A%"}, 1},
		{"in", "category", metadata.OpIn, []interface{}{"A", "B"}, 3},
		{"nin", "category", metadata.OpNin, []interface{}{"A"}, 2},
		{"bt", "amount", metadata.OpBt, []interface{}{50, 100}, 3},
		{"nbt", "amount", metadata.OpNbt, []interface{}{50, 100}, 1},
		{"empty vals is no-op", "name", metadata.OpEq, []interface{}{}, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := db.GetDB().NewSelect().Table("filter_items")
			result := applyFilter(query, "filter_items", tt.col, tt.operator, tt.vals, db.IlikeOp())

			count, err := result.Count(ctx)
			if err != nil {
				t.Fatalf("query failed: %v", err)
			}
			if count != tt.wantCount {
				t.Errorf("count = %d, want %d", count, tt.wantCount)
			}
		})
	}
}

type existsTestParent struct {
	bun.BaseModel `bun:"table:exists_parents"`
	ID            int    `bun:"id,pk,autoincrement"`
	Name          string `bun:"name"`
}

type existsTestChild struct {
	bun.BaseModel `bun:"table:exists_children"`
	ID            int    `bun:"id,pk,autoincrement"`
	ParentID      int    `bun:"parent_id"`
	Status        string `bun:"status"`
}

type existsTestGrandchild struct {
	bun.BaseModel `bun:"table:exists_grandchildren"`
	ID            int    `bun:"id,pk,autoincrement"`
	ChildID       int    `bun:"child_id"`
	Value         string `bun:"value"`
}

type existsChainFixture struct {
	ctx            context.Context
	wrapper        *Wrapper[existsTestParent]
	parentMeta     *metadata.TypeMetadata
	childMeta      *metadata.TypeMetadata
	grandchildMeta *metadata.TypeMetadata
}

func setupExistsChainFixture(t *testing.T, db *SQLite) existsChainFixture {
	t.Helper()
	ctx := context.Background()

	for _, model := range []interface{}{
		(*existsTestParent)(nil),
		(*existsTestChild)(nil),
		(*existsTestGrandchild)(nil),
	} {
		_, err := db.GetDB().NewCreateTable().Model(model).IfNotExists().Exec(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	db.GetDB().RegisterModel((*existsTestParent)(nil), (*existsTestChild)(nil), (*existsTestGrandchild)(nil))

	parents := []existsTestParent{
		{Name: "has-children"},
		{Name: "no-children"},
		{Name: "has-grandchildren"},
	}
	_, err := db.GetDB().NewInsert().Model(&parents).Exec(ctx)
	if err != nil {
		t.Fatal(err)
	}

	children := []existsTestChild{
		{ParentID: 1, Status: "active"},
		{ParentID: 1, Status: "inactive"},
		{ParentID: 3, Status: "active"},
	}
	_, err = db.GetDB().NewInsert().Model(&children).Exec(ctx)
	if err != nil {
		t.Fatal(err)
	}

	grandchildren := []existsTestGrandchild{
		{ChildID: 3, Value: "deep"},
	}
	_, err = db.GetDB().NewInsert().Model(&grandchildren).Exec(ctx)
	if err != nil {
		t.Fatal(err)
	}

	return existsChainFixture{
		ctx:     ctx,
		wrapper: &Wrapper[existsTestParent]{Store: db},
		parentMeta: &metadata.TypeMetadata{
			ModelType: reflect.TypeOf(existsTestParent{}),
			TableName: "exists_parents",
		},
		childMeta: &metadata.TypeMetadata{
			ModelType:     reflect.TypeOf(existsTestChild{}),
			TableName:     "exists_children",
			ForeignKeyCol: "parent_id",
		},
		grandchildMeta: &metadata.TypeMetadata{
			ModelType:     reflect.TypeOf(existsTestGrandchild{}),
			TableName:     "exists_grandchildren",
			ForeignKeyCol: "child_id",
		},
	}
}

func TestBuildExistsChain(t *testing.T) {
	db, cleanup := setupHelperTestDB(t)
	defer cleanup()

	f := setupExistsChainFixture(t, db)
	ctx := f.ctx
	wrapper := f.wrapper
	parentMeta := f.parentMeta
	childMeta := f.childMeta
	grandchildMeta := f.grandchildMeta

	t.Run("empty chain returns nil", func(t *testing.T) {
		result := wrapper.buildExistsChain(parentMeta, nil, nil)
		if result != nil {
			t.Error("expected nil for empty chain")
		}
	})

	t.Run("single level without inner filter", func(t *testing.T) {
		chain := []*metadata.TypeMetadata{childMeta}
		existsSubq := wrapper.buildExistsChain(parentMeta, chain, nil)

		var results []existsTestParent
		err := db.GetDB().NewSelect().
			Model(&results).
			Where("EXISTS (?)", existsSubq).
			Scan(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 2 {
			t.Errorf("got %d results, want 2 (parents with children)", len(results))
		}
	})

	t.Run("single level with inner filter", func(t *testing.T) {
		chain := []*metadata.TypeMetadata{childMeta}
		existsSubq := wrapper.buildExistsChain(parentMeta, chain, func(q *bun.SelectQuery) *bun.SelectQuery {
			return applyFilter(q, "exists_children", "status", metadata.OpEq, []interface{}{"inactive"}, "LIKE")
		})

		var results []existsTestParent
		err := db.GetDB().NewSelect().
			Model(&results).
			Where("EXISTS (?)", existsSubq).
			Scan(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 {
			t.Errorf("got %d results, want 1", len(results))
			return
		}
		if results[0].Name != "has-children" {
			t.Errorf("got name %q, want %q", results[0].Name, "has-children")
		}
	})

	t.Run("two level chain without inner filter", func(t *testing.T) {
		chain := []*metadata.TypeMetadata{childMeta, grandchildMeta}
		existsSubq := wrapper.buildExistsChain(parentMeta, chain, nil)

		var results []existsTestParent
		err := db.GetDB().NewSelect().
			Model(&results).
			Where("EXISTS (?)", existsSubq).
			Scan(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 {
			t.Errorf("got %d results, want 1 (only parent with grandchildren)", len(results))
			return
		}
		if results[0].Name != "has-grandchildren" {
			t.Errorf("got name %q, want %q", results[0].Name, "has-grandchildren")
		}
	})

	t.Run("two level chain with inner filter", func(t *testing.T) {
		chain := []*metadata.TypeMetadata{childMeta, grandchildMeta}
		existsSubq := wrapper.buildExistsChain(parentMeta, chain, func(q *bun.SelectQuery) *bun.SelectQuery {
			return applyFilter(q, "exists_grandchildren", "value", metadata.OpEq, []interface{}{"deep"}, "LIKE")
		})

		var results []existsTestParent
		err := db.GetDB().NewSelect().
			Model(&results).
			Where("EXISTS (?)", existsSubq).
			Scan(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 {
			t.Errorf("got %d results, want 1", len(results))
		}
	})

	t.Run("two level chain with non-matching filter", func(t *testing.T) {
		chain := []*metadata.TypeMetadata{childMeta, grandchildMeta}
		existsSubq := wrapper.buildExistsChain(parentMeta, chain, func(q *bun.SelectQuery) *bun.SelectQuery {
			return applyFilter(q, "exists_grandchildren", "value", metadata.OpEq, []interface{}{"nonexistent"}, "LIKE")
		})

		var results []existsTestParent
		err := db.GetDB().NewSelect().
			Model(&results).
			Where("EXISTS (?)", existsSubq).
			Scan(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 0 {
			t.Errorf("got %d results, want 0", len(results))
		}
	})

	t.Run("NOT EXISTS", func(t *testing.T) {
		chain := []*metadata.TypeMetadata{childMeta}
		existsSubq := wrapper.buildExistsChain(parentMeta, chain, nil)

		var results []existsTestParent
		err := db.GetDB().NewSelect().
			Model(&results).
			Where("NOT EXISTS (?)", existsSubq).
			Scan(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 {
			t.Errorf("got %d results, want 1 (parent with no children)", len(results))
			return
		}
		if results[0].Name != "no-children" {
			t.Errorf("got name %q, want %q", results[0].Name, "no-children")
		}
	})

	t.Run("inner filter with in operator", func(t *testing.T) {
		chain := []*metadata.TypeMetadata{childMeta}
		existsSubq := wrapper.buildExistsChain(parentMeta, chain, func(q *bun.SelectQuery) *bun.SelectQuery {
			return applyFilter(q, "exists_children", "status", metadata.OpIn, []interface{}{"active", "inactive"}, "LIKE")
		})

		var results []existsTestParent
		err := db.GetDB().NewSelect().
			Model(&results).
			Where("EXISTS (?)", existsSubq).
			Scan(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 2 {
			t.Errorf("got %d results, want 2", len(results))
		}
	})
}

func TestBuildExistsChain_CrossIsolation(t *testing.T) {
	db, cleanup := setupHelperTestDB(t)
	defer cleanup()

	f := setupExistsChainFixture(t, db)
	ctx := f.ctx
	wrapper := f.wrapper
	parentMeta := f.parentMeta
	childMeta := f.childMeta
	grandchildMeta := f.grandchildMeta

	t.Run("filter matches other parents children only", func(t *testing.T) {
		// Parent 1 has active+inactive children, parent 3 has active only.
		// Filtering for inactive must return parent 1 and exclude parent 3,
		// even though inactive children exist in the table.
		// A broken FK correlation (missing child.fk = parent.pk) would return both.
		chain := []*metadata.TypeMetadata{childMeta}
		existsSubq := wrapper.buildExistsChain(parentMeta, chain, func(q *bun.SelectQuery) *bun.SelectQuery {
			return applyFilter(q, "exists_children", "status", metadata.OpEq, []interface{}{"inactive"}, "LIKE")
		})

		var results []existsTestParent
		err := db.GetDB().NewSelect().
			Model(&results).
			Where("EXISTS (?)", existsSubq).
			Scan(ctx)
		if err != nil {
			t.Fatal(err)
		}

		names := make(map[string]bool)
		for _, r := range results {
			names[r.Name] = true
		}
		if names["has-grandchildren"] {
			t.Error("has-grandchildren should be excluded: its children are all active")
		}
		if names["no-children"] {
			t.Error("no-children should be excluded: it has no children at all")
		}
		if !names["has-children"] {
			t.Error("has-children should be included: it has an inactive child")
		}
	})

	t.Run("grandchildren dont leak to wrong parent", func(t *testing.T) {
		// Grandchild exists under parent 3's child chain.
		// Parent 1 has children but NO grandchildren.
		// A broken nested FK correlation would let parent 1 match
		// because grandchildren exist somewhere in the table.
		chain := []*metadata.TypeMetadata{childMeta, grandchildMeta}
		existsSubq := wrapper.buildExistsChain(parentMeta, chain, nil)

		var results []existsTestParent
		err := db.GetDB().NewSelect().
			Model(&results).
			Where("EXISTS (?)", existsSubq).
			Scan(ctx)
		if err != nil {
			t.Fatal(err)
		}

		names := make(map[string]bool)
		for _, r := range results {
			names[r.Name] = true
		}
		if names["has-children"] {
			t.Error("has-children should be excluded: its children have no grandchildren")
		}
		if names["no-children"] {
			t.Error("no-children should be excluded: it has no children at all")
		}
		if !names["has-grandchildren"] {
			t.Error("has-grandchildren should be included: it has grandchildren")
		}
		if len(results) != 1 {
			t.Errorf("got %d results, want exactly 1", len(results))
		}
	})

	t.Run("filtered grandchild doesnt leak", func(t *testing.T) {
		// Grandchild with value="deep" exists under parent 3.
		// Filtering grandchildren for value="deep" must NOT return parent 1,
		// even though parent 1 has children (just not ones with grandchildren).
		chain := []*metadata.TypeMetadata{childMeta, grandchildMeta}
		existsSubq := wrapper.buildExistsChain(parentMeta, chain, func(q *bun.SelectQuery) *bun.SelectQuery {
			return applyFilter(q, "exists_grandchildren", "value", metadata.OpEq, []interface{}{"deep"}, "LIKE")
		})

		var results []existsTestParent
		err := db.GetDB().NewSelect().
			Model(&results).
			Where("EXISTS (?)", existsSubq).
			Scan(ctx)
		if err != nil {
			t.Fatal(err)
		}

		names := make(map[string]bool)
		for _, r := range results {
			names[r.Name] = true
		}
		if names["has-children"] {
			t.Error("data leak: has-children returned despite having no grandchildren with value=deep")
		}
		if names["no-children"] {
			t.Error("data leak: no-children returned despite having no children at all")
		}
		if !names["has-grandchildren"] {
			t.Error("has-grandchildren should be included")
		}
	})
}

func TestBuildCountChain(t *testing.T) {
	db, cleanup := setupHelperTestDB(t)
	defer cleanup()

	ctx := context.Background()

	for _, model := range []interface{}{
		(*existsTestParent)(nil),
		(*existsTestChild)(nil),
		(*existsTestGrandchild)(nil),
	} {
		_, err := db.GetDB().NewCreateTable().Model(model).IfNotExists().Exec(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	db.GetDB().RegisterModel((*existsTestParent)(nil), (*existsTestChild)(nil), (*existsTestGrandchild)(nil))

	parents := []existsTestParent{
		{Name: "has-two-children"},
		{Name: "has-one-child"},
		{Name: "no-children"},
	}
	_, err := db.GetDB().NewInsert().Model(&parents).Exec(ctx)
	if err != nil {
		t.Fatal(err)
	}

	children := []existsTestChild{
		{ParentID: 1, Status: "active"},
		{ParentID: 1, Status: "inactive"},
		{ParentID: 2, Status: "active"},
	}
	_, err = db.GetDB().NewInsert().Model(&children).Exec(ctx)
	if err != nil {
		t.Fatal(err)
	}

	grandchildren := []existsTestGrandchild{
		{ChildID: 1, Value: "a"},
		{ChildID: 1, Value: "b"},
		{ChildID: 1, Value: "c"},
		{ChildID: 3, Value: "d"},
	}
	_, err = db.GetDB().NewInsert().Model(&grandchildren).Exec(ctx)
	if err != nil {
		t.Fatal(err)
	}

	wrapper := &Wrapper[existsTestParent]{Store: db}

	parentMeta := &metadata.TypeMetadata{
		ModelType: reflect.TypeOf(existsTestParent{}),
		TableName: "exists_parents",
	}

	childMeta := &metadata.TypeMetadata{
		ModelType:     reflect.TypeOf(existsTestChild{}),
		TableName:     "exists_children",
		ForeignKeyCol: "parent_id",
	}

	grandchildMeta := &metadata.TypeMetadata{
		ModelType:     reflect.TypeOf(existsTestGrandchild{}),
		TableName:     "exists_grandchildren",
		ForeignKeyCol: "child_id",
	}

	t.Run("empty chain returns nil", func(t *testing.T) {
		result := wrapper.buildCountChain(parentMeta, nil)
		if result != nil {
			t.Error("expected nil for empty chain")
		}
	})

	t.Run("single level count", func(t *testing.T) {
		chain := []*metadata.TypeMetadata{childMeta}
		countSubq := wrapper.buildCountChain(parentMeta, chain)

		var results []existsTestParent
		err := db.GetDB().NewSelect().
			Model(&results).
			Where("(?) > ?", countSubq, 1).
			Scan(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 {
			t.Errorf("got %d results, want 1 (parent with >1 children)", len(results))
			return
		}
		if results[0].Name != "has-two-children" {
			t.Errorf("got name %q, want %q", results[0].Name, "has-two-children")
		}
	})

	t.Run("single level count eq zero", func(t *testing.T) {
		chain := []*metadata.TypeMetadata{childMeta}
		countSubq := wrapper.buildCountChain(parentMeta, chain)

		var results []existsTestParent
		err := db.GetDB().NewSelect().
			Model(&results).
			Where("(?) = ?", countSubq, 0).
			Scan(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 {
			t.Errorf("got %d results, want 1 (parent with 0 children)", len(results))
			return
		}
		if results[0].Name != "no-children" {
			t.Errorf("got name %q, want %q", results[0].Name, "no-children")
		}
	})

	t.Run("two level count", func(t *testing.T) {
		chain := []*metadata.TypeMetadata{childMeta, grandchildMeta}
		countSubq := wrapper.buildCountChain(parentMeta, chain)

		var results []existsTestParent
		err := db.GetDB().NewSelect().
			Model(&results).
			Where("(?) >= ?", countSubq, 3).
			Scan(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 {
			t.Errorf("got %d results, want 1 (parent 1 has 3 grandchildren)", len(results))
			return
		}
		if results[0].Name != "has-two-children" {
			t.Errorf("got name %q, want %q", results[0].Name, "has-two-children")
		}
	})

	t.Run("cross-parent count isolation", func(t *testing.T) {
		chain := []*metadata.TypeMetadata{childMeta, grandchildMeta}
		countSubq := wrapper.buildCountChain(parentMeta, chain)

		var results []existsTestParent
		err := db.GetDB().NewSelect().
			Model(&results).
			Where("(?) = ?", countSubq, 1).
			Scan(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 {
			t.Errorf("got %d results, want 1 (parent 2 has 1 grandchild)", len(results))
			return
		}
		if results[0].Name != "has-one-child" {
			t.Errorf("got name %q, want %q", results[0].Name, "has-one-child")
		}
	})
}

func TestApplyRelationFilter_Exists(t *testing.T) {
	db, cleanup := setupHelperTestDB(t)
	defer cleanup()

	ctx := context.Background()

	for _, model := range []interface{}{
		(*existsTestParent)(nil),
		(*existsTestChild)(nil),
	} {
		_, err := db.GetDB().NewCreateTable().Model(model).IfNotExists().Exec(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	db.GetDB().RegisterModel((*existsTestParent)(nil), (*existsTestChild)(nil))

	parents := []existsTestParent{
		{Name: "has-children"},
		{Name: "no-children"},
	}
	_, err := db.GetDB().NewInsert().Model(&parents).Exec(ctx)
	if err != nil {
		t.Fatal(err)
	}

	children := []existsTestChild{
		{ParentID: 1, Status: "active"},
	}
	_, err = db.GetDB().NewInsert().Model(&children).Exec(ctx)
	if err != nil {
		t.Fatal(err)
	}

	wrapper := &Wrapper[existsTestParent]{Store: db}

	childMeta := &metadata.TypeMetadata{
		ModelType:     reflect.TypeOf(existsTestChild{}),
		TableName:     "exists_children",
		ForeignKeyCol: "parent_id",
	}

	parentMeta := &metadata.TypeMetadata{
		ModelType: reflect.TypeOf(existsTestParent{}),
		TableName: "exists_parents",
		ChildMeta: map[string]*metadata.TypeMetadata{"Children": childMeta},
	}

	t.Run("exists true", func(t *testing.T) {
		query := db.GetDB().NewSelect().Model(&[]existsTestParent{})
		query = wrapper.applyRelationFilter(ctx, query, parentMeta, "Children",
			metadata.FilterValue{Value: "true", Operator: metadata.OpExists}, nil)

		count, err := query.Count(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("got %d, want 1", count)
		}
	})

	t.Run("exists false", func(t *testing.T) {
		query := db.GetDB().NewSelect().Model(&[]existsTestParent{})
		query = wrapper.applyRelationFilter(ctx, query, parentMeta, "Children",
			metadata.FilterValue{Value: "false", Operator: metadata.OpExists}, nil)

		count, err := query.Count(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("got %d, want 1", count)
		}
	})

	t.Run("auth blocks unauthorized relation", func(t *testing.T) {
		query := db.GetDB().NewSelect().Model(&[]existsTestParent{})
		allowed := metadata.AllowedIncludes{"Other": false}
		query = wrapper.applyRelationFilter(ctx, query, parentMeta, "Children",
			metadata.FilterValue{Value: "true", Operator: metadata.OpExists}, allowed)

		count, err := query.Count(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if count != 2 {
			t.Errorf("got %d, want 2 (filter should be skipped)", count)
		}
	})

	t.Run("auth allows authorized relation", func(t *testing.T) {
		query := db.GetDB().NewSelect().Model(&[]existsTestParent{})
		allowed := metadata.AllowedIncludes{"Children": false}
		query = wrapper.applyRelationFilter(ctx, query, parentMeta, "Children",
			metadata.FilterValue{Value: "true", Operator: metadata.OpExists}, allowed)

		count, err := query.Count(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("got %d, want 1", count)
		}
	})
}

func TestApplyRelationFilter_Count(t *testing.T) {
	db, cleanup := setupHelperTestDB(t)
	defer cleanup()

	ctx := context.Background()

	for _, model := range []interface{}{
		(*existsTestParent)(nil),
		(*existsTestChild)(nil),
	} {
		_, err := db.GetDB().NewCreateTable().Model(model).IfNotExists().Exec(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	db.GetDB().RegisterModel((*existsTestParent)(nil), (*existsTestChild)(nil))

	parents := []existsTestParent{
		{Name: "three-children"},
		{Name: "one-child"},
		{Name: "no-children"},
	}
	_, err := db.GetDB().NewInsert().Model(&parents).Exec(ctx)
	if err != nil {
		t.Fatal(err)
	}

	children := []existsTestChild{
		{ParentID: 1, Status: "active"},
		{ParentID: 1, Status: "active"},
		{ParentID: 1, Status: "active"},
		{ParentID: 2, Status: "active"},
	}
	_, err = db.GetDB().NewInsert().Model(&children).Exec(ctx)
	if err != nil {
		t.Fatal(err)
	}

	wrapper := &Wrapper[existsTestParent]{Store: db}

	childMeta := &metadata.TypeMetadata{
		ModelType:     reflect.TypeOf(existsTestChild{}),
		TableName:     "exists_children",
		ForeignKeyCol: "parent_id",
	}

	parentMeta := &metadata.TypeMetadata{
		ModelType: reflect.TypeOf(existsTestParent{}),
		TableName: "exists_parents",
		ChildMeta: map[string]*metadata.TypeMetadata{"Children": childMeta},
	}

	t.Run("count_gt", func(t *testing.T) {
		query := db.GetDB().NewSelect().Model(&[]existsTestParent{})
		query = wrapper.applyRelationFilter(ctx, query, parentMeta, "Children",
			metadata.FilterValue{Value: "1", Operator: metadata.OpCountGt}, nil)

		count, err := query.Count(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("got %d, want 1 (only three-children has >1)", count)
		}
	})

	t.Run("count_gte", func(t *testing.T) {
		query := db.GetDB().NewSelect().Model(&[]existsTestParent{})
		query = wrapper.applyRelationFilter(ctx, query, parentMeta, "Children",
			metadata.FilterValue{Value: "1", Operator: metadata.OpCountGte}, nil)

		count, err := query.Count(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if count != 2 {
			t.Errorf("got %d, want 2 (three-children and one-child)", count)
		}
	})

	t.Run("count_eq zero", func(t *testing.T) {
		query := db.GetDB().NewSelect().Model(&[]existsTestParent{})
		query = wrapper.applyRelationFilter(ctx, query, parentMeta, "Children",
			metadata.FilterValue{Value: "0", Operator: metadata.OpCountEq}, nil)

		count, err := query.Count(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("got %d, want 1 (no-children)", count)
		}
	})

	t.Run("count_lt", func(t *testing.T) {
		query := db.GetDB().NewSelect().Model(&[]existsTestParent{})
		query = wrapper.applyRelationFilter(ctx, query, parentMeta, "Children",
			metadata.FilterValue{Value: "3", Operator: metadata.OpCountLt}, nil)

		count, err := query.Count(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if count != 2 {
			t.Errorf("got %d, want 2 (one-child=1, no-children=0)", count)
		}
	})

	t.Run("invalid count value ignored", func(t *testing.T) {
		query := db.GetDB().NewSelect().Model(&[]existsTestParent{})
		query = wrapper.applyRelationFilter(ctx, query, parentMeta, "Children",
			metadata.FilterValue{Value: "abc", Operator: metadata.OpCountGt}, nil)

		count, err := query.Count(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if count != 3 {
			t.Errorf("got %d, want 3 (filter should be skipped for invalid value)", count)
		}
	})
}

func TestAllowedIncludes_ChildFieldFilter(t *testing.T) {
	db, cleanup := setupHelperTestDB(t)
	defer cleanup()

	if err := Initialize(db); err != nil {
		t.Fatal(err)
	}
	defer Cleanup()

	ctx := context.Background()

	for _, model := range []interface{}{
		(*existsTestParent)(nil),
		(*existsTestChild)(nil),
	} {
		_, err := db.GetDB().NewCreateTable().Model(model).IfNotExists().Exec(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	db.GetDB().RegisterModel((*existsTestParent)(nil), (*existsTestChild)(nil))

	parents := []existsTestParent{
		{Name: "has-active"},
		{Name: "has-inactive"},
	}
	_, err := db.GetDB().NewInsert().Model(&parents).Exec(ctx)
	if err != nil {
		t.Fatal(err)
	}

	children := []existsTestChild{
		{ParentID: 1, Status: "active"},
		{ParentID: 2, Status: "inactive"},
	}
	_, err = db.GetDB().NewInsert().Model(&children).Exec(ctx)
	if err != nil {
		t.Fatal(err)
	}

	childMeta := &metadata.TypeMetadata{
		ModelType:        reflect.TypeOf(existsTestChild{}),
		TableName:        "exists_children",
		ForeignKeyCol:    "parent_id",
		FilterableFields: []string{"Status"},
	}

	parentMeta := &metadata.TypeMetadata{
		ModelType: reflect.TypeOf(existsTestParent{}),
		TableName: "exists_parents",
		PKField:   "ID",
		ChildMeta: map[string]*metadata.TypeMetadata{"Children": childMeta},
	}

	opts := &metadata.QueryOptions{
		Filters: map[string]metadata.FilterValue{
			"Children.Status": {Value: "active", Operator: metadata.OpEq},
		},
	}

	wrapper := &Wrapper[existsTestParent]{Store: db}

	t.Run("no auth context allows filter", func(t *testing.T) {
		query := db.GetDB().NewSelect().Model(&[]existsTestParent{})
		query = wrapper.applyQueryFilters(ctx, query, opts, parentMeta)

		count, err := query.Count(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("got %d, want 1 (filter should apply)", count)
		}
	})

	t.Run("authorized relation allows filter", func(t *testing.T) {
		allowed := metadata.AllowedIncludes{"Children": false}
		authCtx := context.WithValue(ctx, metadata.AllowedIncludesKey, allowed)

		query := db.GetDB().NewSelect().Model(&[]existsTestParent{})
		query = wrapper.applyQueryFilters(authCtx, query, opts, parentMeta)

		count, err := query.Count(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("got %d, want 1 (filter should apply)", count)
		}
	})

	t.Run("unauthorized relation blocks filter", func(t *testing.T) {
		allowed := metadata.AllowedIncludes{"Other": false}
		authCtx := context.WithValue(ctx, metadata.AllowedIncludesKey, allowed)

		query := db.GetDB().NewSelect().Model(&[]existsTestParent{})
		query = wrapper.applyQueryFilters(authCtx, query, opts, parentMeta)

		count, err := query.Count(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if count != 2 {
			t.Errorf("got %d, want 2 (filter should be silently skipped)", count)
		}
	})
}

type tenantParent struct {
	bun.BaseModel `bun:"table:tenant_parents"`
	ID            int    `bun:"id,pk,autoincrement"`
	Name          string `bun:"name"`
	TenantID      string `bun:"tenant_id"`
}

type tenantChild struct {
	bun.BaseModel `bun:"table:tenant_children"`
	ID            int    `bun:"id,pk,autoincrement"`
	ParentID      int    `bun:"parent_id"`
	TenantID      string `bun:"tenant_id"`
	Status        string `bun:"status"`
}

type tenantGrandchild struct {
	bun.BaseModel `bun:"table:tenant_grandchildren"`
	ID            int    `bun:"id,pk,autoincrement"`
	ChildID       int    `bun:"child_id"`
	TenantID      string `bun:"tenant_id"`
	Value         string `bun:"value"`
}

type tenantChainFixture struct {
	ctx            context.Context
	wrapper        *Wrapper[tenantParent]
	parentMeta     *metadata.TypeMetadata
	childMeta      *metadata.TypeMetadata
	grandchildMeta *metadata.TypeMetadata
}

func setupTenantChainFixture(t *testing.T, db *SQLite) tenantChainFixture {
	t.Helper()
	ctx := context.Background()

	for _, model := range []interface{}{
		(*tenantParent)(nil),
		(*tenantChild)(nil),
		(*tenantGrandchild)(nil),
	} {
		_, err := db.GetDB().NewCreateTable().Model(model).IfNotExists().Exec(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	db.GetDB().RegisterModel((*tenantParent)(nil), (*tenantChild)(nil), (*tenantGrandchild)(nil))

	// Tenant A: alice (has child), alex (no children)
	// Tenant B: bob (has child), beth (child has wrong tenant_id=A)
	parents := []tenantParent{
		{Name: "alice", TenantID: "A"},
		{Name: "alex", TenantID: "A"},
		{Name: "bob", TenantID: "B"},
		{Name: "beth", TenantID: "B"},
	}
	_, err := db.GetDB().NewInsert().Model(&parents).Exec(ctx)
	if err != nil {
		t.Fatal(err)
	}

	children := []tenantChild{
		{ParentID: 1, TenantID: "A", Status: "active"},
		{ParentID: 3, TenantID: "B", Status: "active"},
		{ParentID: 4, TenantID: "A", Status: "active"},
	}
	_, err = db.GetDB().NewInsert().Model(&children).Exec(ctx)
	if err != nil {
		t.Fatal(err)
	}

	grandchildren := []tenantGrandchild{
		{ChildID: 1, TenantID: "A", Value: "alpha"},
		{ChildID: 2, TenantID: "B", Value: "bravo"},
	}
	_, err = db.GetDB().NewInsert().Model(&grandchildren).Exec(ctx)
	if err != nil {
		t.Fatal(err)
	}

	return tenantChainFixture{
		ctx:     ctx,
		wrapper: &Wrapper[tenantParent]{Store: db},
		parentMeta: &metadata.TypeMetadata{
			ModelType: reflect.TypeOf(tenantParent{}),
			TableName: "tenant_parents",
		},
		childMeta: &metadata.TypeMetadata{
			ModelType:     reflect.TypeOf(tenantChild{}),
			TableName:     "tenant_children",
			ForeignKeyCol: "parent_id",
		},
		grandchildMeta: &metadata.TypeMetadata{
			ModelType:     reflect.TypeOf(tenantGrandchild{}),
			TableName:     "tenant_grandchildren",
			ForeignKeyCol: "child_id",
		},
	}
}

func TestBuildExistsChain_TenantIsolation(t *testing.T) {
	db, cleanup := setupHelperTestDB(t)
	defer cleanup()

	f := setupTenantChainFixture(t, db)
	ctx := f.ctx
	wrapper := f.wrapper
	parentMeta := f.parentMeta
	childMeta := f.childMeta
	grandchildMeta := f.grandchildMeta

	t.Run("child tenant filter excludes cross-tenant data", func(t *testing.T) {
		// Filter for children with tenant_id=B.
		// bob has a child with tenant_id=B → included.
		// beth has a child but with tenant_id=A → excluded (cross-tenant leak if not filtered).
		// alice has a child with tenant_id=A → excluded.
		chain := []*metadata.TypeMetadata{childMeta}
		existsSubq := wrapper.buildExistsChain(parentMeta, chain, func(q *bun.SelectQuery) *bun.SelectQuery {
			return applyFilter(q, "tenant_children", "tenant_id", metadata.OpEq, []interface{}{"B"}, "LIKE")
		})

		var results []tenantParent
		err := db.GetDB().NewSelect().
			Model(&results).
			Where("EXISTS (?)", existsSubq).
			Scan(ctx)
		if err != nil {
			t.Fatal(err)
		}

		names := make(map[string]bool)
		for _, r := range results {
			names[r.Name] = true
		}
		if names["alice"] {
			t.Error("tenant leak: alice (tenant A) returned for tenant B child filter")
		}
		if names["alex"] {
			t.Error("tenant leak: alex returned despite having no children")
		}
		if names["beth"] {
			t.Error("tenant leak: beth returned despite her child having tenant_id=A, not B")
		}
		if !names["bob"] {
			t.Error("bob should be included: has child with tenant_id=B")
		}
		if len(results) != 1 {
			t.Errorf("got %d results, want exactly 1", len(results))
		}
	})

	t.Run("child tenant filter for tenant A", func(t *testing.T) {
		// Filter for children with tenant_id=A.
		// alice has child with tenant_id=A → included.
		// beth has child with tenant_id=A → also included (child has tenant A, even though beth is tenant B).
		// This is correct behavior: the filter is on child tenant, not parent tenant.
		// Real tenant enforcement needs conditions at EVERY chain level.
		chain := []*metadata.TypeMetadata{childMeta}
		existsSubq := wrapper.buildExistsChain(parentMeta, chain, func(q *bun.SelectQuery) *bun.SelectQuery {
			return applyFilter(q, "tenant_children", "tenant_id", metadata.OpEq, []interface{}{"A"}, "LIKE")
		})

		var results []tenantParent
		err := db.GetDB().NewSelect().
			Model(&results).
			Where("EXISTS (?)", existsSubq).
			Scan(ctx)
		if err != nil {
			t.Fatal(err)
		}

		names := make(map[string]bool)
		for _, r := range results {
			names[r.Name] = true
		}
		if !names["alice"] {
			t.Error("alice should be included: has child with tenant_id=A")
		}
		if !names["beth"] {
			t.Error("beth should be included: has child with tenant_id=A (leaf-only filtering)")
		}
		if names["bob"] {
			t.Error("bob should be excluded: child has tenant_id=B")
		}
		if names["alex"] {
			t.Error("alex should be excluded: no children")
		}
	})

	t.Run("grandchild tenant filter isolates across chain", func(t *testing.T) {
		// Filter for grandchildren with tenant_id=B.
		// Only bob's child has a grandchild with tenant_id=B → only bob returned.
		// alice's child has a grandchild but with tenant_id=A → excluded.
		chain := []*metadata.TypeMetadata{childMeta, grandchildMeta}
		existsSubq := wrapper.buildExistsChain(parentMeta, chain, func(q *bun.SelectQuery) *bun.SelectQuery {
			return applyFilter(q, "tenant_grandchildren", "tenant_id", metadata.OpEq, []interface{}{"B"}, "LIKE")
		})

		var results []tenantParent
		err := db.GetDB().NewSelect().
			Model(&results).
			Where("EXISTS (?)", existsSubq).
			Scan(ctx)
		if err != nil {
			t.Fatal(err)
		}

		names := make(map[string]bool)
		for _, r := range results {
			names[r.Name] = true
		}
		if names["alice"] {
			t.Error("tenant leak: alice returned despite grandchild having tenant_id=A")
		}
		if names["beth"] {
			t.Error("tenant leak: beth returned despite having no grandchildren")
		}
		if !names["bob"] {
			t.Error("bob should be included: has grandchild with tenant_id=B")
		}
		if len(results) != 1 {
			t.Errorf("got %d results, want exactly 1", len(results))
		}
	})

	t.Run("combined FK and tenant filter prevents leak", func(t *testing.T) {
		// Filter children: status=active AND tenant_id=B.
		// All three children are active, but only child 2 (bob's) has tenant_id=B.
		// beth's child is active but tenant_id=A → must be excluded.
		chain := []*metadata.TypeMetadata{childMeta}
		existsSubq := wrapper.buildExistsChain(parentMeta, chain, func(q *bun.SelectQuery) *bun.SelectQuery {
			q = applyFilter(q, "tenant_children", "status", metadata.OpEq, []interface{}{"active"}, "LIKE")
			q = applyFilter(q, "tenant_children", "tenant_id", metadata.OpEq, []interface{}{"B"}, "LIKE")
			return q
		})

		var results []tenantParent
		err := db.GetDB().NewSelect().
			Model(&results).
			Where("EXISTS (?)", existsSubq).
			Scan(ctx)
		if err != nil {
			t.Fatal(err)
		}

		names := make(map[string]bool)
		for _, r := range results {
			names[r.Name] = true
		}
		if names["alice"] {
			t.Error("tenant leak: alice returned for tenant B filter")
		}
		if names["beth"] {
			t.Error("tenant leak: beth returned despite child having tenant_id=A")
		}
		if !names["bob"] {
			t.Error("bob should be included: has active child with tenant_id=B")
		}
		if len(results) != 1 {
			t.Errorf("got %d results, want exactly 1", len(results))
		}
	})
}
