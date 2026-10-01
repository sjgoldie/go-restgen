package datastore

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

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

// useStore makes s the global store for the test, restoring the previous store afterwards.
func useStore(t *testing.T, s Store) {
	t.Helper()
	previous := singleton
	singleton = s
	t.Cleanup(func() { singleton = previous })
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
	useStore(t, setupAccounts(t))
	ctx := context.Background()
	plain := &metadata.TypeMetadata{ModelType: reflect.TypeFor[cuAccount]()}
	tenant := &metadata.TypeMetadata{ModelType: reflect.TypeFor[cuAccount](), TenantField: "OrgID"}

	if key, err := ResolveKeyByField(ctx, plain, "Subject", "bob", ""); err != nil || key != "3" {
		t.Errorf("single match: got %q, %v", key, err)
	}
	if key, err := ResolveKeyByField(ctx, tenant, "Subject", "alice", "org-b"); err != nil || key != "2" {
		t.Errorf("tenant scoped: got %q, %v", key, err)
	}
	if _, err := ResolveKeyByField(ctx, tenant, "Subject", "bob", "org-b"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Errorf("row in another tenant: got %v, want ErrNotFound", err)
	}
	if _, err := ResolveKeyByField(ctx, plain, "Subject", "alice", ""); !errors.Is(err, apperrors.ErrNotFound) {
		t.Errorf("more than one match: got %v, want ErrNotFound", err)
	}
	if _, err := ResolveKeyByField(ctx, plain, "Subject", "carol", ""); !errors.Is(err, apperrors.ErrNotFound) {
		t.Errorf("no match: got %v, want ErrNotFound", err)
	}

	if _, err := ResolveKeyByField(ctx, plain, "Missing", "bob", ""); err == nil {
		t.Error("unknown field: expected an error")
	}
	badTenant := &metadata.TypeMetadata{ModelType: reflect.TypeFor[cuAccount](), TenantField: "Missing"}
	if _, err := ResolveKeyByField(ctx, badTenant, "Subject", "bob", "org-a"); err == nil {
		t.Error("unknown tenant field: expected an error")
	}
	composite := &metadata.TypeMetadata{ModelType: reflect.TypeFor[cuPair]()}
	if _, err := ResolveKeyByField(ctx, composite, "Subject", "bob", ""); err == nil {
		t.Error("composite primary key: expected an error")
	}
	rls := &metadata.TypeMetadata{ModelType: reflect.TypeFor[cuAccount](), TenantField: "OrgID", UseRLS: true}
	if _, err := ResolveKeyByField(ctx, rls, "Subject", "bob", "org-a"); err == nil {
		t.Error("RLS route without PostgreSQL: the tenant transaction cannot be scoped, expected an error")
	}
	missingTable := &metadata.TypeMetadata{ModelType: reflect.TypeFor[cuPair]()}
	missingTable.ModelType = reflect.TypeFor[partTask]()
	if _, err := ResolveKeyByField(ctx, missingTable, "Title", "x", ""); err == nil {
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

type cuMissing struct {
	bun.BaseModel `bun:"table:cu_missing"`
	ID            int    `bun:"id,pk,autoincrement"`
	Subject       string `bun:"subject"`
	ParentID      int    `bun:"parent_id"`
}

// sqlmockStore is a PostgreSQL-dialect Store backed by go-sqlmock, for RLS paths SQLite cannot run.
type sqlmockStore struct{ db *bun.DB }

func (s *sqlmockStore) GetDB() *bun.DB            { return s.db }
func (s *sqlmockStore) GetTimeout() time.Duration { return 30 * time.Second }
func (s *sqlmockStore) IlikeOp() string           { return "ILIKE" }
func (s *sqlmockStore) Cleanup()                  {}

func TestResolveKeyByField_RLS(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	db := bun.NewDB(sqlDB, pgdialect.New())
	t.Cleanup(func() { _ = db.Close() })
	useStore(t, &sqlmockStore{db: db})

	mock.ExpectBegin()
	mock.ExpectExec(`^SELECT set_config\('app\.tenant_id', 'org-a', true\)$`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`^SELECT "id" FROM "cu_accounts" WHERE \("subject" = 'alice'\) AND \("org_id" = 'org-a'\)`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("7"))
	mock.ExpectRollback()

	meta := &metadata.TypeMetadata{ModelType: reflect.TypeFor[cuAccount](), TenantField: "OrgID", UseRLS: true}
	key, err := ResolveKeyByField(context.Background(), meta, "Subject", "alice", "org-a")
	if err != nil || key != "7" {
		t.Errorf("got %q, %v", key, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("the lookup must run in a transaction scoped to the tenant: %v", err)
	}
}

func TestCurrentUserLookups_WithoutStore(t *testing.T) {
	useStore(t, nil)
	ctx := context.Background()
	account := &metadata.TypeMetadata{ModelType: reflect.TypeFor[cuAccount]()}

	if _, err := ResolveKeyByField(ctx, account, "Subject", "alice", ""); err == nil {
		t.Error("ResolveKeyByField: expected an error")
	}
	single := &metadata.TypeMetadata{ModelType: reflect.TypeFor[cuAccount](), ParentMeta: account, ParentFKField: "Subject"}
	if _, err := ResolveSingleRouteKey(ctx, single, "1"); err == nil {
		t.Error("ResolveSingleRouteKey: expected an error")
	}
	if FieldIsUnique(reflect.TypeFor[cuAccount](), "Subject") {
		t.Error("FieldIsUnique: expected false")
	}
}

func TestResolveSingleRouteKey_QueryError(t *testing.T) {
	useStore(t, setupAccounts(t))
	single := &metadata.TypeMetadata{
		TypeName:      "cuAccount",
		ModelType:     reflect.TypeFor[cuAccount](),
		ParentMeta:    &metadata.TypeMetadata{ModelType: reflect.TypeFor[cuMissing]()},
		ParentFKField: "ParentID",
	}
	if _, err := ResolveSingleRouteKey(context.Background(), single, "1"); err == nil || errors.Is(err, apperrors.ErrNotFound) {
		t.Errorf("parent table missing: got %v, want a database error", err)
	}
}

func TestUpdate_CurrentUserFieldNeedsACaller(t *testing.T) {
	db := setupAccounts(t)
	useStore(t, db)
	meta := &metadata.TypeMetadata{
		TypeName:         "cuAccount",
		TableName:        "cu_accounts",
		ModelType:        reflect.TypeFor[cuAccount](),
		PKField:          "ID",
		CurrentUserField: "Subject",
	}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, meta)
	w := &Wrapper[cuAccount]{Store: db}

	if _, err := w.Update(ctx, "1", cuAccount{ID: 1, Subject: "bob", Name: "x"}); !errors.Is(err, apperrors.ErrForbidden) {
		t.Errorf("update without a caller: got %v, want ErrForbidden", err)
	}
	authed := context.WithValue(ctx, metadata.AuthInfoKey, &metadata.AuthInfo{UserID: "alice"})
	updated, err := w.Update(authed, "1", cuAccount{ID: 1, OrgID: "org-a", Subject: "bob", Name: "x"})
	if err != nil || updated.Subject != "alice" {
		t.Errorf("update by the caller: the identity field must hold the caller's ID, got %+v, %v", updated, err)
	}
}
