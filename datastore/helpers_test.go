package datastore

import (
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

func TestBuildFilterCondition(t *testing.T) {
	tests := []struct {
		name     string
		table    string
		col      string
		operator string
		vals     []interface{}
		wantSQL  string
		wantArgs int
	}{
		{"eq", "users", "name", "eq", []interface{}{"alice"}, "users.name = ?", 1},
		{"empty op defaults to eq", "users", "name", "", []interface{}{"bob"}, "users.name = ?", 1},
		{"neq", "users", "status", "neq", []interface{}{"inactive"}, "users.status != ?", 1},
		{"gt", "orders", "amount", "gt", []interface{}{100}, "orders.amount > ?", 1},
		{"gte", "orders", "amount", "gte", []interface{}{100}, "orders.amount >= ?", 1},
		{"lt", "orders", "amount", "lt", []interface{}{50}, "orders.amount < ?", 1},
		{"lte", "orders", "amount", "lte", []interface{}{50}, "orders.amount <= ?", 1},
		{"like", "users", "email", "like", []interface{}{"%@test.com"}, "users.email LIKE ?", 1},
		{"in", "users", "role", "in", []interface{}{"admin", "user"}, "users.role IN (?, ?)", 2},
		{"nin", "users", "role", "nin", []interface{}{"banned"}, "users.role NOT IN (?)", 1},
		{"bt", "orders", "price", "bt", []interface{}{10, 100}, "orders.price BETWEEN ? AND ?", 2},
		{"nbt", "orders", "price", "nbt", []interface{}{0, 5}, "orders.price NOT BETWEEN ? AND ?", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args := buildFilterCondition(tt.table, tt.col, tt.operator, tt.vals)
			if sql != tt.wantSQL {
				t.Errorf("sql = %q, want %q", sql, tt.wantSQL)
			}
			if len(args) != tt.wantArgs {
				t.Errorf("args count = %d, want %d", len(args), tt.wantArgs)
			}
		})
	}
}
