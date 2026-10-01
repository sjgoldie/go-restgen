package datastore

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/uptrace/bun"

	apperrors "github.com/sjgoldie/go-restgen/errors"
	"github.com/sjgoldie/go-restgen/metadata"
)

type cuAccount struct {
	bun.BaseModel `bun:"table:cu_accounts"`
	ID            int    `bun:"id,pk,autoincrement"`
	OrgID         string `bun:"org_id,unique:org_subject"`
	Subject       string `bun:"subject,unique:org_subject"`
	Name          string `bun:"name"`
}

type cuPair struct {
	bun.BaseModel `bun:"table:cu_pairs"`
	A             int    `bun:"a,pk"`
	B             int    `bun:"b,pk"`
	Subject       string `bun:"subject"`
}

func setupAccounts(t *testing.T) *SQLite {
	t.Helper()
	db, cleanup := setupHelperTestDB(t)
	t.Cleanup(cleanup)
	_ = Initialize(db)
	ctx := context.Background()
	if _, err := db.GetDB().NewCreateTable().Model((*cuAccount)(nil)).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	accounts := []cuAccount{
		{OrgID: "org-a", Subject: "alice", Name: "Alice"},
		{OrgID: "org-b", Subject: "alice", Name: "Alice B"},
		{OrgID: "org-a", Subject: "bob", Name: "Bob"},
	}
	if _, err := db.GetDB().NewInsert().Model(&accounts).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestResolveKeyByField(t *testing.T) {
	db := setupAccounts(t)
	ctx := context.Background()
	plain := &metadata.TypeMetadata{ModelType: reflect.TypeFor[cuAccount]()}
	tenant := &metadata.TypeMetadata{ModelType: reflect.TypeFor[cuAccount](), TenantField: "OrgID"}

	if key, err := ResolveKeyByField(ctx, db, plain, "Subject", "bob", ""); err != nil || key != "3" {
		t.Errorf("single match: got %q, %v", key, err)
	}
	if key, err := ResolveKeyByField(ctx, db, tenant, "Subject", "alice", "org-b"); err != nil || key != "2" {
		t.Errorf("tenant scoped: got %q, %v", key, err)
	}
	if _, err := ResolveKeyByField(ctx, db, tenant, "Subject", "bob", "org-b"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Errorf("row in another tenant: got %v, want ErrNotFound", err)
	}
	if _, err := ResolveKeyByField(ctx, db, plain, "Subject", "alice", ""); !errors.Is(err, apperrors.ErrNotFound) {
		t.Errorf("more than one match: got %v, want ErrNotFound", err)
	}
	if _, err := ResolveKeyByField(ctx, db, plain, "Subject", "carol", ""); !errors.Is(err, apperrors.ErrNotFound) {
		t.Errorf("no match: got %v, want ErrNotFound", err)
	}

	if _, err := ResolveKeyByField(ctx, db, plain, "Missing", "bob", ""); err == nil {
		t.Error("unknown field: expected an error")
	}
	badTenant := &metadata.TypeMetadata{ModelType: reflect.TypeFor[cuAccount](), TenantField: "Missing"}
	if _, err := ResolveKeyByField(ctx, db, badTenant, "Subject", "bob", "org-a"); err == nil {
		t.Error("unknown tenant field: expected an error")
	}
	composite := &metadata.TypeMetadata{ModelType: reflect.TypeFor[cuPair]()}
	if _, err := ResolveKeyByField(ctx, db, composite, "Subject", "bob", ""); err == nil {
		t.Error("composite primary key: expected an error")
	}
	rls := &metadata.TypeMetadata{ModelType: reflect.TypeFor[cuAccount](), TenantField: "OrgID", UseRLS: true}
	if _, err := ResolveKeyByField(ctx, db, rls, "Subject", "bob", "org-a"); err == nil {
		t.Error("RLS route without PostgreSQL: the tenant transaction cannot be scoped, expected an error")
	}
	missingTable := &metadata.TypeMetadata{ModelType: reflect.TypeFor[cuPair]()}
	missingTable.ModelType = reflect.TypeFor[partTask]()
	if _, err := ResolveKeyByField(ctx, db, missingTable, "Title", "x", ""); err == nil {
		t.Error("query failure: expected an error")
	}
}

func TestSetCurrentUserField(t *testing.T) {
	setupAccounts(t)
	meta := &metadata.TypeMetadata{ModelType: reflect.TypeFor[cuAccount](), CurrentUserField: "Subject"}
	alice := context.WithValue(context.Background(), metadata.AuthInfoKey, &metadata.AuthInfo{UserID: "alice"})

	item := &cuAccount{Subject: "bob"}
	if err := setCurrentUserField(alice, meta, item); err != nil || item.Subject != "alice" {
		t.Errorf("caller's ID written: got %q, %v", item.Subject, err)
	}
	if err := setCurrentUserField(context.Background(), meta, item); !errors.Is(err, apperrors.ErrForbidden) {
		t.Errorf("no caller: got %v, want ErrForbidden", err)
	}
	if err := setCurrentUserField(alice, &metadata.TypeMetadata{}, item); err != nil {
		t.Errorf("not a current user route: got %v", err)
	}
	bad := &metadata.TypeMetadata{ModelType: reflect.TypeFor[cuAccount](), CurrentUserField: "Missing"}
	if err := setCurrentUserField(alice, bad, item); err == nil {
		t.Error("unknown field: expected an error")
	}
}

func TestFieldIsUnique(t *testing.T) {
	setupAccounts(t)
	if !FieldIsUnique(reflect.TypeFor[cuAccount](), "Subject") {
		t.Error("Subject is declared unique")
	}
	if FieldIsUnique(reflect.TypeFor[cuAccount](), "Name") {
		t.Error("Name is not declared unique")
	}
	if FieldIsUnique(reflect.TypeFor[cuAccount](), "Missing") {
		t.Error("an unknown field is not unique")
	}
}
