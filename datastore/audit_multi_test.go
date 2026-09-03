package datastore_test

import (
	"context"
	"reflect"
	"strconv"
	"testing"

	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/metadata"
)

// TestVersionRow is a second audit-style model, written alongside TestAuditLog
// to exercise auditors that return more than one record.
type TestVersionRow struct {
	bun.BaseModel `bun:"table:test_version_rows"`
	ID            int    `bun:"id,pk,autoincrement"`
	ItemID        int    `bun:"item_id,notnull"`
	Status        string `bun:"status,notnull"`
	Marker        string `bun:"marker,unique"`
}

func setupMultiAuditTestDB(t *testing.T) (*datastore.SQLite, func()) {
	t.Helper()

	db, cleanup := setupAuditTestDB(t)
	if _, err := db.GetDB().NewCreateTable().Model((*TestVersionRow)(nil)).IfNotExists().Exec(context.Background()); err != nil {
		cleanup()
		t.Fatal("Failed to create version rows table:", err)
	}
	return db, func() {
		_, _ = db.GetDB().NewDropTable().Model((*TestVersionRow)(nil)).IfExists().Exec(context.Background())
		cleanup()
	}
}

func multiAuditMeta(auditor metadata.AuditFunc[TestAuditItem]) *metadata.TypeMetadata {
	return &metadata.TypeMetadata{
		TypeID:       "test_audit_item",
		TypeName:     "TestAuditItem",
		TableName:    "test_audit_items",
		URLParamUUID: "id",
		PKField:      "ID",
		ModelType:    reflect.TypeOf(TestAuditItem{}),
		Auditor:      auditor,
	}
}

func auditItemID(ac metadata.AuditContext[TestAuditItem]) int {
	if ac.New != nil {
		return ac.New.ID
	}
	if ac.Old != nil {
		return ac.Old.ID
	}
	return 0
}

func countRows(t *testing.T, db *datastore.SQLite, model any) int {
	t.Helper()
	n, err := db.GetDB().NewSelect().Model(model).Count(context.Background())
	if err != nil {
		t.Fatalf("count %T failed: %v", model, err)
	}
	return n
}

func TestAuditMulti_AuditAndVersionRowsCommitTogether(t *testing.T) {
	db, cleanup := setupMultiAuditTestDB(t)
	defer cleanup()

	auditor := metadata.AuditFunc[TestAuditItem](func(ac metadata.AuditContext[TestAuditItem]) any {
		id := auditItemID(ac)
		status := ""
		if ac.New != nil {
			status = ac.New.Status
		}
		return []any{
			&TestAuditLog{ItemID: id, Operation: string(ac.Operation), NewStatus: status},
			&TestVersionRow{ItemID: id, Status: status, Marker: string(ac.Operation) + "-" + strconv.Itoa(id)},
		}
	})

	wrapper := &datastore.Wrapper[TestAuditItem]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, multiAuditMeta(auditor))

	created, err := wrapper.Create(ctx, TestAuditItem{Name: "Test", Status: "pending"})
	if err != nil {
		t.Fatal("create failed:", err)
	}
	created.Status = "active"
	if _, err := wrapper.Update(ctx, strconv.Itoa(created.ID), *created); err != nil {
		t.Fatal("update failed:", err)
	}

	if n := countRows(t, db, (*TestAuditLog)(nil)); n != 2 {
		t.Errorf("expected 2 audit logs, got %d", n)
	}

	var versions []TestVersionRow
	if err := db.GetDB().NewSelect().Model(&versions).Order("id").Scan(context.Background()); err != nil {
		t.Fatal("failed to query version rows:", err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 version rows, got %d", len(versions))
	}
	if versions[0].Status != "pending" || versions[1].Status != "active" {
		t.Errorf("expected version statuses [pending active], got [%s %s]", versions[0].Status, versions[1].Status)
	}
	if versions[0].ItemID != created.ID || versions[1].ItemID != created.ID {
		t.Errorf("expected version rows for item %d, got %d and %d", created.ID, versions[0].ItemID, versions[1].ItemID)
	}
}

func TestAuditMulti_NilElementsAreSkipped(t *testing.T) {
	db, cleanup := setupMultiAuditTestDB(t)
	defer cleanup()

	auditor := metadata.AuditFunc[TestAuditItem](func(ac metadata.AuditContext[TestAuditItem]) any {
		// A conditionally built row left as a typed nil pointer, plus an untyped nil.
		var version *TestVersionRow
		if ac.Operation == metadata.OpDelete {
			version = &TestVersionRow{ItemID: auditItemID(ac), Status: "deleted", Marker: "delete"}
		}
		return []any{
			&TestAuditLog{ItemID: auditItemID(ac), Operation: string(ac.Operation)},
			version,
			nil,
		}
	})

	wrapper := &datastore.Wrapper[TestAuditItem]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, multiAuditMeta(auditor))

	created, err := wrapper.Create(ctx, TestAuditItem{Name: "Test", Status: "pending"})
	if err != nil {
		t.Fatal("create must succeed when the version element is a typed nil:", err)
	}
	if n := countRows(t, db, (*TestVersionRow)(nil)); n != 0 {
		t.Errorf("expected no version rows after create, got %d", n)
	}

	if err := wrapper.Delete(ctx, strconv.Itoa(created.ID)); err != nil {
		t.Fatal("delete failed:", err)
	}
	if n := countRows(t, db, (*TestVersionRow)(nil)); n != 1 {
		t.Errorf("expected 1 version row after delete, got %d", n)
	}
	if n := countRows(t, db, (*TestAuditLog)(nil)); n != 2 {
		t.Errorf("expected 2 audit logs, got %d", n)
	}
}

func TestAuditMulti_EmptySliceInsertsNothing(t *testing.T) {
	db, cleanup := setupMultiAuditTestDB(t)
	defer cleanup()

	auditor := metadata.AuditFunc[TestAuditItem](func(metadata.AuditContext[TestAuditItem]) any {
		return []any{}
	})

	wrapper := &datastore.Wrapper[TestAuditItem]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, multiAuditMeta(auditor))

	if _, err := wrapper.Create(ctx, TestAuditItem{Name: "Test", Status: "pending"}); err != nil {
		t.Fatal("create failed:", err)
	}
	if n := countRows(t, db, (*TestAuditLog)(nil)); n != 0 {
		t.Errorf("expected no audit logs, got %d", n)
	}
}

func TestAuditMulti_SecondInsertFailureRollsBackEverything(t *testing.T) {
	db, cleanup := setupMultiAuditTestDB(t)
	defer cleanup()

	// Pre-existing row that the version insert will collide with on the unique marker.
	if _, err := db.GetDB().NewInsert().Model(&TestVersionRow{ItemID: 0, Status: "seed", Marker: "taken"}).Exec(context.Background()); err != nil {
		t.Fatal("failed to seed version row:", err)
	}

	auditor := metadata.AuditFunc[TestAuditItem](func(ac metadata.AuditContext[TestAuditItem]) any {
		return []any{
			&TestAuditLog{ItemID: auditItemID(ac), Operation: string(ac.Operation)},
			&TestVersionRow{ItemID: auditItemID(ac), Status: "pending", Marker: "taken"},
		}
	})

	wrapper := &datastore.Wrapper[TestAuditItem]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, multiAuditMeta(auditor))

	if _, err := wrapper.Create(ctx, TestAuditItem{Name: "Test", Status: "pending"}); err == nil {
		t.Fatal("expected create to fail when the second audit insert fails")
	}

	if n := countRows(t, db, (*TestAuditItem)(nil)); n != 0 {
		t.Errorf("main write must roll back, found %d items", n)
	}
	if n := countRows(t, db, (*TestAuditLog)(nil)); n != 0 {
		t.Errorf("first audit insert must roll back, found %d audit logs", n)
	}
	if n := countRows(t, db, (*TestVersionRow)(nil)); n != 1 {
		t.Errorf("only the seed version row should remain, found %d", n)
	}
}

func TestAuditMulti_BatchWritesAllRecordsPerItem(t *testing.T) {
	db, cleanup := setupMultiAuditTestDB(t)
	defer cleanup()

	auditor := metadata.AuditFunc[TestAuditItem](func(ac metadata.AuditContext[TestAuditItem]) any {
		id := auditItemID(ac)
		return []any{
			&TestAuditLog{ItemID: id, Operation: string(ac.Operation)},
			&TestVersionRow{ItemID: id, Status: ac.New.Status, Marker: string(ac.Operation) + "-" + strconv.Itoa(id)},
		}
	})

	wrapper := &datastore.Wrapper[TestAuditItem]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, multiAuditMeta(auditor))

	created, err := wrapper.BatchCreate(ctx, []TestAuditItem{
		{Name: "One", Status: "pending"},
		{Name: "Two", Status: "pending"},
	})
	if err != nil {
		t.Fatal("batch create failed:", err)
	}

	updates := make([]TestAuditItem, 0, len(created))
	for _, item := range created {
		copied := *item
		copied.Status = "active"
		updates = append(updates, copied)
	}
	if _, err := wrapper.BatchUpdate(ctx, updates); err != nil {
		t.Fatal("batch update failed:", err)
	}

	if n := countRows(t, db, (*TestAuditLog)(nil)); n != 4 {
		t.Errorf("expected 4 audit logs (2 items x 2 operations), got %d", n)
	}
	if n := countRows(t, db, (*TestVersionRow)(nil)); n != 4 {
		t.Errorf("expected 4 version rows (2 items x 2 operations), got %d", n)
	}
}

func TestAuditMulti_TypedSliceStillBatchInserts(t *testing.T) {
	db, cleanup := setupMultiAuditTestDB(t)
	defer cleanup()

	auditor := metadata.AuditFunc[TestAuditItem](func(ac metadata.AuditContext[TestAuditItem]) any {
		id := auditItemID(ac)
		return &[]*TestAuditLog{
			{ItemID: id, Operation: string(ac.Operation), NewStatus: "first"},
			{ItemID: id, Operation: string(ac.Operation), NewStatus: "second"},
		}
	})

	wrapper := &datastore.Wrapper[TestAuditItem]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, multiAuditMeta(auditor))

	if _, err := wrapper.Create(ctx, TestAuditItem{Name: "Test", Status: "pending"}); err != nil {
		t.Fatal("create failed:", err)
	}
	if n := countRows(t, db, (*TestAuditLog)(nil)); n != 2 {
		t.Errorf("expected a typed slice to insert 2 audit logs, got %d", n)
	}
}
