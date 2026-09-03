package datastore_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/driver/sqliteshim"

	"github.com/sjgoldie/go-restgen/datastore"
	apperrors "github.com/sjgoldie/go-restgen/errors"
	"github.com/sjgoldie/go-restgen/metadata"
)

type hardItem struct {
	bun.BaseModel `bun:"table:hard_items"`
	ID            int    `bun:"id,pk,autoincrement"`
	Name          string `bun:"name,notnull"`
	Status        string `bun:"status"`
	OwnerID       string `bun:"owner_id"`
	AssigneeID    string `bun:"assignee_id"`
}

type hardParent struct {
	bun.BaseModel `bun:"table:hard_parents"`
	MyPK          int    `bun:"my_pk,pk,autoincrement"`
	Name          string `bun:"name"`
}

type hardChild struct {
	bun.BaseModel `bun:"table:hard_children"`
	ID            int    `bun:"id,pk,autoincrement"`
	ParentRef     int    `bun:"parent_ref,notnull"`
	Name          string `bun:"name"`
}

type hardOrg struct {
	bun.BaseModel `bun:"table:hard_orgs"`
	Key           string `bun:"org_key,pk"`
	Name          string `bun:"name"`
}

type hardPost struct {
	bun.BaseModel `bun:"table:hard_posts"`
	ID            int            `bun:"id,pk,autoincrement"`
	Title         string         `bun:"title"`
	OrgID         string         `bun:"org_id"`
	Comments      []*hardComment `bun:"rel:has-many,join:id=post_id"`
}

type hardComment struct {
	bun.BaseModel `bun:"table:hard_comments"`
	ID            int    `bun:"id,pk,autoincrement"`
	PostID        int    `bun:"post_id,notnull"`
	UserID        string `bun:"user_id"`
	OrgID         string `bun:"org_id"`
}

func newHardDB(t *testing.T) *datastore.SQLite {
	t.Helper()
	db, err := datastore.NewSQLite(":memory:")
	if err != nil {
		t.Fatal("failed to create database:", err)
	}
	// ColumnName resolves through the global datastore; make sure one exists
	// even when this file runs in isolation (first initialisation wins).
	_ = datastore.Initialize(db)
	ctx := context.Background()
	for _, model := range []any{(*hardItem)(nil), (*hardParent)(nil), (*hardChild)(nil), (*hardOrg)(nil), (*hardPost)(nil), (*hardComment)(nil)} {
		if _, err := db.GetDB().NewCreateTable().Model(model).IfNotExists().Exec(ctx); err != nil {
			db.Cleanup()
			t.Fatalf("failed to create table for %T: %v", model, err)
		}
	}
	t.Cleanup(db.Cleanup)
	return db
}

func hardItemMeta() *metadata.TypeMetadata {
	return &metadata.TypeMetadata{
		TypeID:       "hard_item",
		TypeName:     "HardItem",
		TableName:    "hard_items",
		URLParamUUID: "id",
		PKField:      "ID",
		ModelType:    reflect.TypeOf(hardItem{}),
	}
}

func ownedCtx(meta *metadata.TypeMetadata, userID string, scopes ...string) context.Context {
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, meta)
	ctx = context.WithValue(ctx, metadata.OwnershipEnforcedKey, true)
	ctx = context.WithValue(ctx, metadata.OwnershipUserIDKey, userID)
	return context.WithValue(ctx, metadata.AuthInfoKey, &metadata.AuthInfo{UserID: userID, Scopes: scopes})
}

func mustInsert(t *testing.T, db *datastore.SQLite, model any) {
	t.Helper()
	if _, err := db.GetDB().NewInsert().Model(model).Exec(context.Background()); err != nil {
		t.Fatalf("insert %T: %v", model, err)
	}
}

// #2: batch PATCH validators must receive the new item.

func TestBatchPatch_ValidatorReceivesNewItem(t *testing.T) {
	db := newHardDB(t)
	meta := hardItemMeta()
	var seenNew []string
	meta.Validator = metadata.ValidatorFunc[hardItem](func(vc metadata.ValidationContext[hardItem]) error {
		if vc.Operation == metadata.OpPatch {
			if vc.New == nil {
				t.Fatal("validator received New = nil under OpPatch")
			}
			seenNew = append(seenNew, vc.New.Status)
		}
		return nil
	})
	wrapper := &datastore.Wrapper[hardItem]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, meta)

	mustInsert(t, db, &hardItem{Name: "a", Status: "pending"})
	mustInsert(t, db, &hardItem{Name: "b", Status: "pending"})

	if _, err := wrapper.BatchPatch(ctx, []hardItem{{ID: 1, Name: "a", Status: "done"}, {ID: 2, Name: "b", Status: "done"}}); err != nil {
		t.Fatal("batch patch failed:", err)
	}
	if len(seenNew) != 2 || seenNew[0] != "done" || seenNew[1] != "done" {
		t.Errorf("validator saw new statuses %v, want [done done]", seenNew)
	}
}

// #4: parent-chain and tenant-table filters must use the real PK column.

func TestAlternatePK_ParentChainFilterUsesParentPKColumn(t *testing.T) {
	db := newHardDB(t)
	mustInsert(t, db, &hardParent{Name: "p1"})
	mustInsert(t, db, &hardParent{Name: "p2"})
	mustInsert(t, db, &hardChild{ParentRef: 1, Name: "c1"})
	mustInsert(t, db, &hardChild{ParentRef: 2, Name: "c2"})

	parentMeta := &metadata.TypeMetadata{
		TypeID: "hard_parent", TypeName: "HardParent", TableName: "hard_parents",
		URLParamUUID: "parent", PKField: "MyPK", ModelType: reflect.TypeOf(hardParent{}),
	}
	childMeta := &metadata.TypeMetadata{
		TypeID: "hard_child", TypeName: "HardChild", TableName: "hard_children",
		URLParamUUID: "child", PKField: "ID", ModelType: reflect.TypeOf(hardChild{}),
		ParentType: parentMeta.ModelType, ParentMeta: parentMeta,
		ForeignKeyCol: "parent_ref", ParentJoinCol: "my_pk", ParentJoinField: "MyPK",
	}

	wrapper := &datastore.Wrapper[hardChild]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, childMeta)
	ctx = context.WithValue(ctx, metadata.ParentIDsKey, map[string]string{"parent": "2"})

	items, _, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll under a parent with a non-id PK column failed:", err)
	}
	if len(items) != 1 || items[0].Name != "c2" {
		t.Errorf("expected only c2 under parent 2, got %+v", items)
	}
}

func TestAlternatePK_TenantTableFilterUsesPKColumn(t *testing.T) {
	db := newHardDB(t)
	mustInsert(t, db, &hardOrg{Key: "org-a", Name: "A"})
	mustInsert(t, db, &hardOrg{Key: "org-b", Name: "B"})

	orgMeta := &metadata.TypeMetadata{
		TypeID: "hard_org", TypeName: "HardOrg", TableName: "hard_orgs",
		URLParamUUID: "id", PKField: "Key", ModelType: reflect.TypeOf(hardOrg{}), IsTenantTable: true,
	}
	wrapper := &datastore.Wrapper[hardOrg]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, orgMeta)
	ctx = context.WithValue(ctx, metadata.TenantScopedKey, true)
	ctx = context.WithValue(ctx, metadata.TenantIDValueKey, "org-a")

	items, _, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal("GetAll on a tenant table with a non-id PK column failed:", err)
	}
	if len(items) != 1 || items[0].Key != "org-a" {
		t.Errorf("expected only org-a, got %+v", items)
	}
	if _, err := wrapper.Get(ctx, "org-b"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Errorf("another tenant's row must be invisible, got %v", err)
	}
}

// #7: creating the tenant entity itself must target the caller's own tenant.

func TestTenantTableCreate_EnforcesCallerTenant(t *testing.T) {
	db := newHardDB(t)
	orgMeta := &metadata.TypeMetadata{
		TypeID: "hard_org", TypeName: "HardOrg", TableName: "hard_orgs",
		URLParamUUID: "id", PKField: "Key", ModelType: reflect.TypeOf(hardOrg{}), IsTenantTable: true,
	}
	wrapper := &datastore.Wrapper[hardOrg]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, orgMeta)
	ctx = context.WithValue(ctx, metadata.TenantScopedKey, true)
	ctx = context.WithValue(ctx, metadata.TenantIDValueKey, "org-a")

	created, err := wrapper.Create(ctx, hardOrg{Name: "mine"})
	if err != nil {
		t.Fatal("create with an empty PK must succeed:", err)
	}
	if created.Key != "org-a" {
		t.Errorf("empty PK must be filled with the caller's tenant, got %q", created.Key)
	}

	_, err = wrapper.Create(ctx, hardOrg{Key: "org-b", Name: "theirs"})
	var validationErr *apperrors.ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("creating another tenant's row must be a validation error, got %v", err)
	}
	if n, _ := db.GetDB().NewSelect().Model((*hardOrg)(nil)).Where("org_key = ?", "org-b").Count(context.Background()); n != 0 {
		t.Error("rejected row must not be written")
	}

	_, err = wrapper.BatchCreate(ctx, []hardOrg{{Key: "org-b", Name: "theirs"}})
	if !errors.As(err, &validationErr) {
		t.Errorf("batch create must apply the same rule, got %v", err)
	}

	// Scoped but with no tenant ID in context is a server-side inconsistency, not a create
	broken := context.WithValue(context.WithValue(context.Background(), metadata.MetadataKey, orgMeta), metadata.TenantScopedKey, true)
	if _, err := wrapper.Create(broken, hardOrg{Name: "nobody"}); err == nil {
		t.Error("tenant scoped without a tenant ID must fail")
	}

	// Without tenant scoping the rule does not apply
	unscoped := context.WithValue(context.Background(), metadata.MetadataKey, orgMeta)
	if _, err := wrapper.Create(unscoped, hardOrg{Key: "org-z", Name: "provisioned"}); err != nil {
		t.Errorf("unscoped create must be unrestricted, got %v", err)
	}
}

// #6: ownership fields are re-asserted on update unless the caller has a bypass scope.

func TestUpdate_ReassertsOwnershipForNonBypassCaller(t *testing.T) {
	db := newHardDB(t)
	meta := hardItemMeta()
	meta.OwnershipFields = []string{"OwnerID"}
	meta.BypassScopes = []string{"admin"}
	wrapper := &datastore.Wrapper[hardItem]{Store: db}

	created, err := wrapper.Create(ownedCtx(meta, "u1"), hardItem{Name: "mine"})
	if err != nil {
		t.Fatal("create failed:", err)
	}
	if created.OwnerID != "u1" {
		t.Fatalf("expected owner u1 on create, got %q", created.OwnerID)
	}
	id := strconv.Itoa(created.ID)

	updated, err := wrapper.Update(ownedCtx(meta, "u1"), id, hardItem{ID: created.ID, Name: "mine", OwnerID: "u2"})
	if err != nil {
		t.Fatal("update failed:", err)
	}
	if updated.OwnerID != "u1" {
		t.Errorf("owner must not be reassigned by a non-bypass caller, got %q", updated.OwnerID)
	}

	patched, err := wrapper.Patch(ownedCtx(meta, "u1"), id, hardItem{ID: created.ID, Name: "mine", OwnerID: ""})
	if err != nil {
		t.Fatal("patch failed:", err)
	}
	if patched.OwnerID != "u1" {
		t.Errorf("owner must not be orphaned by a non-bypass caller, got %q", patched.OwnerID)
	}

	batch, err := wrapper.BatchUpdate(ownedCtx(meta, "u1"), []hardItem{{ID: created.ID, Name: "mine", OwnerID: "u2"}})
	if err != nil {
		t.Fatal("batch update failed:", err)
	}
	if batch[0].OwnerID != "u1" {
		t.Errorf("batch update must re-assert ownership too, got %q", batch[0].OwnerID)
	}

	admin, err := wrapper.Update(ownedCtx(meta, "root", "admin"), id, hardItem{ID: created.ID, Name: "mine", OwnerID: "u2"})
	if err != nil {
		t.Fatal("admin update failed:", err)
	}
	if admin.OwnerID != "u2" {
		t.Errorf("a bypass caller may transfer ownership, got %q", admin.OwnerID)
	}
}

// #1: the datastore honours the current method's ownership config from context.

func TestOwnershipScope_FromContextOverridesTypeWideFields(t *testing.T) {
	db := newHardDB(t)
	meta := hardItemMeta()
	meta.OwnershipFields = []string{"OwnerID"}
	mustInsert(t, db, &hardItem{Name: "owned", OwnerID: "u1", AssigneeID: "u2"})
	mustInsert(t, db, &hardItem{Name: "assigned", OwnerID: "u3", AssigneeID: "u1"})
	wrapper := &datastore.Wrapper[hardItem]{Store: db}

	items, _, _, _, err := wrapper.GetAll(ownedCtx(meta, "u1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "owned" {
		t.Errorf("type-wide fields: expected [owned], got %+v", items)
	}

	// The auth middleware puts this method's config in context: assignee-based
	ctx := context.WithValue(ownedCtx(meta, "u1"), metadata.OwnershipScopeKey, &metadata.OwnershipScope{Fields: []string{"AssigneeID"}})
	items, _, _, _, err = wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "assigned" {
		t.Errorf("per-method fields: expected [assigned], got %+v", items)
	}

	// Per-method bypass scopes travel with the fields
	ctx = context.WithValue(ownedCtx(meta, "u1", "support"), metadata.OwnershipScopeKey, &metadata.OwnershipScope{Fields: []string{"AssigneeID"}, BypassScopes: []string{"support"}})
	items, _, _, _, err = wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Errorf("per-method bypass: expected both rows, got %d", len(items))
	}
}

// #20: one query for a batch, in input order, invisible rows are not found.

func TestGetMany_OrderedAndScoped(t *testing.T) {
	db := newHardDB(t)
	meta := hardItemMeta()
	meta.OwnershipFields = []string{"OwnerID"}
	mustInsert(t, db, &hardItem{Name: "one", OwnerID: "u1"})
	mustInsert(t, db, &hardItem{Name: "two", OwnerID: "u1"})
	mustInsert(t, db, &hardItem{Name: "three", OwnerID: "u9"})
	wrapper := &datastore.Wrapper[hardItem]{Store: db}
	ctx := ownedCtx(meta, "u1")

	items, err := wrapper.GetMany(ctx, []string{"2", "1", "2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || items[0].Name != "two" || items[1].Name != "one" || items[2].Name != "two" {
		t.Errorf("expected input order [two one two], got %+v", items)
	}

	if _, err := wrapper.GetMany(ctx, []string{"1", "999"}); !errors.Is(err, apperrors.ErrNotFound) {
		t.Errorf("missing row: expected ErrNotFound, got %v", err)
	}
	if _, err := wrapper.GetMany(ctx, []string{"3"}); !errors.Is(err, apperrors.ErrNotFound) {
		t.Errorf("another user's row: expected ErrNotFound, got %v", err)
	}
	if empty, err := wrapper.GetMany(ctx, nil); err != nil || len(empty) != 0 {
		t.Errorf("no ids: expected empty result, got %v %v", empty, err)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := wrapper.GetMany(cancelled, []string{"1"}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled context must pass through, got %v", err)
	}

	// Ownership enforced but no user in context is a server-side inconsistency
	broken := context.WithValue(context.WithValue(context.Background(), metadata.MetadataKey, meta), metadata.OwnershipEnforcedKey, true)
	if _, err := wrapper.GetMany(broken, []string{"1"}); err == nil {
		t.Error("ownership enforced without a user ID must fail")
	}
}

// #3: relation counts and existence filters apply the child's ownership and tenant scoping.

func hardPostMeta() *metadata.TypeMetadata {
	postMeta := &metadata.TypeMetadata{
		TypeID: "hard_post", TypeName: "HardPost", TableName: "hard_posts",
		URLParamUUID: "post", PKField: "ID", ModelType: reflect.TypeOf(hardPost{}),
		TenantField: "OrgID", ChildMeta: map[string]*metadata.TypeMetadata{},
	}
	commentMeta := &metadata.TypeMetadata{
		TypeID: "hard_comment", TypeName: "HardComment", TableName: "hard_comments",
		URLParamUUID: "comment", PKField: "ID", ModelType: reflect.TypeOf(hardComment{}),
		ParentType: postMeta.ModelType, ParentMeta: postMeta,
		ForeignKeyCol: "post_id", ParentJoinCol: "id",
		OwnershipFields: []string{"UserID"}, BypassScopes: []string{"admin"}, TenantField: "OrgID",
	}
	postMeta.ChildMeta["Comments"] = commentMeta
	return postMeta
}

func seedHardPosts(t *testing.T, db *datastore.SQLite) {
	t.Helper()
	mustInsert(t, db, &hardPost{Title: "p1", OrgID: "org-a"})
	mustInsert(t, db, &hardPost{Title: "p2", OrgID: "org-a"})
	mustInsert(t, db, &hardComment{PostID: 1, UserID: "u1", OrgID: "org-a"})
	mustInsert(t, db, &hardComment{PostID: 1, UserID: "u1", OrgID: "org-a"})
	mustInsert(t, db, &hardComment{PostID: 1, UserID: "u2", OrgID: "org-a"})
	mustInsert(t, db, &hardComment{PostID: 2, UserID: "u2", OrgID: "org-b"})
}

func relationCtx(postMeta *metadata.TypeMetadata, userID string, applyOwnership bool, filters map[string]metadata.FilterValue, scopes ...string) context.Context {
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, postMeta)
	ctx = context.WithValue(ctx, metadata.AuthInfoKey, &metadata.AuthInfo{UserID: userID, Scopes: scopes})
	ctx = context.WithValue(ctx, metadata.AllowedIncludesKey, metadata.AllowedIncludes{"Comments": applyOwnership})
	if filters != nil {
		ctx = context.WithValue(ctx, metadata.QueryOptionsKey, &metadata.QueryOptions{Filters: filters})
	}
	return ctx
}

func postTitles(items []*hardPost) []string {
	titles := make([]string, 0, len(items))
	for _, p := range items {
		titles = append(titles, p.Title)
	}
	return titles
}

func TestIncludeCounts_RespectChildOwnership(t *testing.T) {
	db := newHardDB(t)
	seedHardPosts(t, db)
	postMeta := hardPostMeta()
	wrapper := &datastore.Wrapper[hardPost]{Store: db}
	posts := []*hardPost{{ID: 1}, {ID: 2}}

	counts, err := wrapper.ComputeIncludeCounts(relationCtx(postMeta, "u1", true, nil), posts, []string{"Comments"})
	if err != nil {
		t.Fatal(err)
	}
	if counts["Comments"]["1"] != 2 || counts["Comments"]["2"] != 0 {
		t.Errorf("u1 must only count own comments, got %v", counts)
	}

	counts, err = wrapper.ComputeIncludeCounts(relationCtx(postMeta, "u1", false, nil), posts, []string{"Comments"})
	if err != nil {
		t.Fatal(err)
	}
	if counts["Comments"]["1"] != 3 || counts["Comments"]["2"] != 1 {
		t.Errorf("without ownership every comment counts, got %v", counts)
	}

	counts, err = wrapper.ComputeIncludeCounts(relationCtx(postMeta, "root", true, nil, "admin"), posts, []string{"Comments"})
	if err != nil {
		t.Fatal(err)
	}
	if counts["Comments"]["1"] != 3 {
		t.Errorf("a bypass scope sees every comment, got %v", counts)
	}
}

func TestRelationFilters_RespectChildOwnership(t *testing.T) {
	db := newHardDB(t)
	seedHardPosts(t, db)
	postMeta := hardPostMeta()
	wrapper := &datastore.Wrapper[hardPost]{Store: db}

	exists := map[string]metadata.FilterValue{"Comments": {Value: "true", Operator: metadata.OpExists}}
	items, _, _, _, err := wrapper.GetAll(relationCtx(postMeta, "u1", true, exists))
	if err != nil {
		t.Fatal(err)
	}
	if got := postTitles(items); len(got) != 1 || got[0] != "p1" {
		t.Errorf("exists for u1: expected [p1], got %v", got)
	}

	items, _, _, _, err = wrapper.GetAll(relationCtx(postMeta, "u2", true, exists))
	if err != nil {
		t.Fatal(err)
	}
	if got := postTitles(items); len(got) != 2 {
		t.Errorf("exists for u2: expected both posts, got %v", got)
	}

	countGt := map[string]metadata.FilterValue{"Comments": {Value: "1", Operator: metadata.OpCountGt}}
	items, _, _, _, err = wrapper.GetAll(relationCtx(postMeta, "u2", true, countGt))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("count_gt 1 for u2 must match nothing (one comment per post), got %v", postTitles(items))
	}

	items, _, _, _, err = wrapper.GetAll(relationCtx(postMeta, "u2", false, countGt))
	if err != nil {
		t.Fatal(err)
	}
	if got := postTitles(items); len(got) != 1 || got[0] != "p1" {
		t.Errorf("count_gt 1 without ownership: expected [p1], got %v", got)
	}
}

func TestRelationFilters_RespectTenant(t *testing.T) {
	db := newHardDB(t)
	seedHardPosts(t, db)
	postMeta := hardPostMeta()
	wrapper := &datastore.Wrapper[hardPost]{Store: db}

	exists := map[string]metadata.FilterValue{"Comments": {Value: "true", Operator: metadata.OpExists}}
	ctx := context.WithValue(relationCtx(postMeta, "u2", false, exists), metadata.TenantScopedKey, true)
	ctx = context.WithValue(ctx, metadata.TenantIDValueKey, "org-a")

	items, _, _, _, err := wrapper.GetAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// p2's only comment belongs to org-b, so within org-a it has none
	if got := postTitles(items); len(got) != 1 || got[0] != "p1" {
		t.Errorf("tenant-scoped exists: expected [p1], got %v", got)
	}

	counts, err := wrapper.ComputeIncludeCounts(ctx, []*hardPost{{ID: 1}, {ID: 2}}, []string{"Comments"})
	if err != nil {
		t.Fatal(err)
	}
	if counts["Comments"]["1"] != 3 || counts["Comments"]["2"] != 0 {
		t.Errorf("tenant-scoped counts: got %v", counts)
	}
}

// #19: per-query timeout is configurable per store.

func TestWithTimeout_Option(t *testing.T) {
	db, err := datastore.NewSQLite(":memory:", datastore.WithTimeout(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Cleanup()
	if db.GetTimeout() != 2*time.Second {
		t.Errorf("SQLite WithTimeout: got %v, want 2s", db.GetTimeout())
	}

	plain, err := datastore.NewSQLite(":memory:", datastore.WithTimeout(0))
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Cleanup()
	if plain.GetTimeout() != datastore.DefaultSQLiteTimeout {
		t.Errorf("WithTimeout(0) must keep the default, got %v", plain.GetTimeout())
	}

	sqlDB, err := sql.Open(sqliteshim.ShimName, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sqlDB.Close() }()
	external := datastore.NewSQLiteWithDB(sqlDB, datastore.WithTimeout(3*time.Second))
	if external.GetTimeout() != 3*time.Second {
		t.Errorf("NewSQLiteWithDB WithTimeout: got %v, want 3s", external.GetTimeout())
	}

	pg, err := datastore.NewPostgres("postgres://user:pass@localhost:5432/db?sslmode=disable", datastore.WithTimeout(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Cleanup()
	if pg.GetTimeout() != time.Second {
		t.Errorf("Postgres WithTimeout: got %v, want 1s", pg.GetTimeout())
	}
	pgDefault, err := datastore.NewPostgres("postgres://user:pass@localhost:5432/db?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	defer pgDefault.Cleanup()
	if pgDefault.GetTimeout() != datastore.DefaultPostgresTimeout {
		t.Errorf("Postgres default timeout: got %v", pgDefault.GetTimeout())
	}
}
