package datastore

import (
	"reflect"
	"testing"

	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/metadata"
)

type pkColumnModel struct {
	bun.BaseModel `bun:"table:pk_column_models"`
	Code          string `bun:"code_col,pk"`
}

type pkColumnComposite struct {
	bun.BaseModel `bun:"table:pk_column_composites"`
	Left          string `bun:"left_col,pk"`
	Right         string `bun:"right_col,pk"`
	Label         string `bun:"label_col"`
}

func TestPKColumn_ResolvesFromSchema(t *testing.T) {
	db, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Cleanup()
	w := &Wrapper[pkColumnModel]{Store: db}

	if got := w.pkColumn(nil); got != "id" {
		t.Errorf("nil meta: got %q, want id", got)
	}
	if got := w.pkColumn(&metadata.TypeMetadata{PKField: "Code"}); got != "id" {
		t.Errorf("no model type: got %q, want id", got)
	}
	if got := w.pkColumn(&metadata.TypeMetadata{ModelType: reflect.TypeOf(pkColumnModel{})}); got != "code_col" {
		t.Errorf("single PK from schema: got %q, want code_col", got)
	}
	if got := w.pkColumn(&metadata.TypeMetadata{ModelType: reflect.TypeOf(&pkColumnModel{})}); got != "code_col" {
		t.Errorf("pointer model type: got %q, want code_col", got)
	}
	if got := w.pkColumn(&metadata.TypeMetadata{ModelType: reflect.TypeOf(pkColumnComposite{}), PKField: "Right"}); got != "right_col" {
		t.Errorf("composite PK with PKField: got %q, want right_col", got)
	}
	if got := w.pkColumn(&metadata.TypeMetadata{ModelType: reflect.TypeOf(pkColumnComposite{}), PKField: "Missing"}); got != "id" {
		t.Errorf("composite PK with unknown PKField: got %q, want id", got)
	}
}
