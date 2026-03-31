package datastore

import (
	"reflect"
	"sync"
	"testing"

	"github.com/uptrace/bun"
)

const testAuthorIDCol = "author_id"

type columnNameTestModel struct {
	bun.BaseModel `bun:"table:test_ftc"`
	ID            int    `bun:"id,pk,autoincrement"`
	NMI           string `bun:"nmi,notnull"`
	AuthorID      int    `bun:"author_id,notnull"`
}

func TestColumnName(t *testing.T) {
	db, cleanup := setupHelperTestDB(t)
	defer cleanup()

	if err := Initialize(db); err != nil {
		t.Fatalf("failed to initialize datastore: %v", err)
	}
	defer func() { singleton = nil; once = sync.Once{} }()

	tType := reflect.TypeOf(columnNameTestModel{})

	t.Run("standard field", func(t *testing.T) {
		col, err := ColumnName(tType, "AuthorID")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if col != testAuthorIDCol {
			t.Errorf("expected 'author_id', got '%s'", col)
		}
	})

	t.Run("acronym field", func(t *testing.T) {
		col, err := ColumnName(tType, "NMI")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if col != "nmi" {
			t.Errorf("expected 'nmi', got '%s'", col)
		}
	})

	t.Run("pk field", func(t *testing.T) {
		col, err := ColumnName(tType, "ID")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if col != "id" {
			t.Errorf("expected 'id', got '%s'", col)
		}
	})

	t.Run("field not found", func(t *testing.T) {
		_, err := ColumnName(tType, "Nonexistent")
		if err == nil {
			t.Fatal("expected error for nonexistent field")
		}
	})
}

type schemaRelUser struct {
	bun.BaseModel `bun:"table:schema_rel_users"`
	ID            int    `bun:"id,pk,autoincrement"`
	Name          string `bun:"name"`
}

type schemaRelPost struct {
	bun.BaseModel `bun:"table:schema_rel_posts"`
	ID            int            `bun:"id,pk,autoincrement"`
	AuthorID      int            `bun:"author_id,notnull"`
	Author        *schemaRelUser `bun:"rel:belongs-to,join:author_id=id"`
	Title         string         `bun:"title"`
}

type schemaUnrelatedModel struct {
	bun.BaseModel `bun:"table:schema_unrelated"`
	ID            int    `bun:"id,pk,autoincrement"`
	Name          string `bun:"name"`
}

func TestFindRelation(t *testing.T) {
	db, cleanup := setupHelperTestDB(t)
	defer cleanup()

	if err := Initialize(db); err != nil {
		t.Fatalf("failed to initialize datastore: %v", err)
	}
	defer func() { singleton = nil; once = sync.Once{} }()

	userType := reflect.TypeOf(schemaRelUser{})
	postType := reflect.TypeOf(schemaRelPost{})

	t.Run("child belongs-to parent", func(t *testing.T) {
		rel, err := FindRelation(postType, userType)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rel.ForeignKeyCol != testAuthorIDCol {
			t.Errorf("expected ForeignKeyCol 'author_id', got %q", rel.ForeignKeyCol)
		}
		if rel.ParentJoinCol != "id" {
			t.Errorf("expected ParentJoinCol 'id', got %q", rel.ParentJoinCol)
		}
		if rel.FieldName != "Author" {
			t.Errorf("expected FieldName 'Author', got %q", rel.FieldName)
		}
	})

	t.Run("parent belongs-to child (inverted)", func(t *testing.T) {
		rel, err := FindRelation(userType, postType)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rel.ForeignKeyCol != testAuthorIDCol {
			t.Errorf("expected ForeignKeyCol 'author_id', got %q", rel.ForeignKeyCol)
		}
		if rel.ParentJoinCol != "id" {
			t.Errorf("expected ParentJoinCol 'id', got %q", rel.ParentJoinCol)
		}
		if rel.FieldName != "Author" {
			t.Errorf("expected FieldName 'Author', got %q", rel.FieldName)
		}
	})

	t.Run("no relationship", func(t *testing.T) {
		unrelatedType := reflect.TypeOf(schemaUnrelatedModel{})
		_, err := FindRelation(unrelatedType, userType)
		if err == nil {
			t.Fatal("expected error for unrelated types")
		}
	})

	t.Run("nil parent returns empty", func(t *testing.T) {
		rel, err := FindRelation(postType, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rel.ForeignKeyCol != "" {
			t.Errorf("expected empty ForeignKeyCol, got %q", rel.ForeignKeyCol)
		}
	})
}

func TestTableName(t *testing.T) {
	db, cleanup := setupHelperTestDB(t)
	defer cleanup()

	if err := Initialize(db); err != nil {
		t.Fatalf("failed to initialize datastore: %v", err)
	}
	defer func() { singleton = nil; once = sync.Once{} }()

	t.Run("returns table name", func(t *testing.T) {
		name := TableName(reflect.TypeOf(schemaRelUser{}))
		if name != "schema_rel_users" {
			t.Errorf("expected 'schema_rel_users', got %q", name)
		}
	})
}
