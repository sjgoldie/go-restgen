package datastore_test

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/datastore"
	apperrors "github.com/sjgoldie/go-restgen/errors"
	"github.com/sjgoldie/go-restgen/metadata"
)

// afterCommitCall captures what a hook invocation observed.
type afterCommitCall struct {
	op        metadata.Operation
	oldStatus string
	newStatus string
	id        int
	ctxErr    error
	txInCtx   bool
	userID    string
	getErr    error
}

// afterCommitRecorder records every hook invocation. The hook reads the row back
// through the datastore with the hook context, so getErr shows whether the
// committed state was visible (present after create/update, absent after delete).
type afterCommitRecorder struct {
	mu    sync.Mutex
	calls []afterCommitCall
}

func (r *afterCommitRecorder) hook(wrapper *datastore.Wrapper[TestAuditItem]) metadata.AfterCommitFunc[TestAuditItem] {
	return func(ac metadata.AfterCommitContext[TestAuditItem]) error {
		call := afterCommitCall{op: ac.Operation, ctxErr: ac.Ctx.Err()}
		_, call.txInCtx = ac.Ctx.Value(metadata.RLSTxKey).(bun.Tx)
		if auth, ok := ac.Ctx.Value(metadata.AuthInfoKey).(*metadata.AuthInfo); ok {
			call.userID = auth.UserID
		}
		if ac.Old != nil {
			call.oldStatus = ac.Old.Status
			call.id = ac.Old.ID
		}
		if ac.New != nil {
			call.newStatus = ac.New.Status
			call.id = ac.New.ID
		}
		_, call.getErr = wrapper.Get(ac.Ctx, strconv.Itoa(call.id))

		r.mu.Lock()
		defer r.mu.Unlock()
		r.calls = append(r.calls, call)
		return nil
	}
}

func (r *afterCommitRecorder) snapshot() []afterCommitCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]afterCommitCall(nil), r.calls...)
}

func afterCommitMeta(hook any) *metadata.TypeMetadata {
	return &metadata.TypeMetadata{
		TypeID:       "test_audit_item",
		TypeName:     "TestAuditItem",
		TableName:    "test_audit_items",
		URLParamUUID: "id",
		PKField:      "ID",
		ModelType:    reflect.TypeOf(TestAuditItem{}),
		AfterCommit:  hook,
	}
}

func TestAfterCommit_CRUDOperations(t *testing.T) {
	db, cleanup := setupAuditTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestAuditItem]{Store: db}
	recorder := &afterCommitRecorder{}
	meta := afterCommitMeta(recorder.hook(wrapper))
	// An auditor forces the write through runInTx, so the hook must wait for that
	// transaction to commit before it can see the row and its audit record.
	meta.Auditor = metadata.AuditFunc[TestAuditItem](func(ac metadata.AuditContext[TestAuditItem]) any {
		id := 0
		if ac.New != nil {
			id = ac.New.ID
		} else if ac.Old != nil {
			id = ac.Old.ID
		}
		return &TestAuditLog{ItemID: id, Operation: string(ac.Operation)}
	})

	ctx := context.WithValue(context.Background(), metadata.MetadataKey, meta)
	ctx = context.WithValue(ctx, metadata.AuthInfoKey, &metadata.AuthInfo{UserID: "user-1"})

	created, err := wrapper.Create(ctx, TestAuditItem{Name: "Test", Status: "pending"})
	if err != nil {
		t.Fatal("create failed:", err)
	}
	id := strconv.Itoa(created.ID)

	created.Status = "active"
	if _, err := wrapper.Update(ctx, id, *created); err != nil {
		t.Fatal("update failed:", err)
	}

	created.Status = "done"
	if _, err := wrapper.Patch(ctx, id, *created); err != nil {
		t.Fatal("patch failed:", err)
	}

	if err := wrapper.Delete(ctx, id); err != nil {
		t.Fatal("delete failed:", err)
	}

	calls := recorder.snapshot()
	if len(calls) != 4 {
		t.Fatalf("expected 4 hook calls, got %d", len(calls))
	}

	expected := []struct {
		op        metadata.Operation
		oldStatus string
		newStatus string
		visible   bool
	}{
		{metadata.OpCreate, "", "pending", true},
		{metadata.OpUpdate, "pending", "active", true},
		{metadata.OpPatch, "active", "done", true},
		{metadata.OpDelete, "done", "", false},
	}
	for i, want := range expected {
		got := calls[i]
		if got.op != want.op {
			t.Errorf("call %d: expected op %q, got %q", i, want.op, got.op)
		}
		if got.oldStatus != want.oldStatus || got.newStatus != want.newStatus {
			t.Errorf("call %d: expected old/new %q/%q, got %q/%q", i, want.oldStatus, want.newStatus, got.oldStatus, got.newStatus)
		}
		if got.id != created.ID {
			t.Errorf("call %d: expected id %d, got %d", i, created.ID, got.id)
		}
		if got.ctxErr != nil {
			t.Errorf("call %d: hook context must not be cancelled, got %v", i, got.ctxErr)
		}
		if got.txInCtx {
			t.Errorf("call %d: hook context must not carry the committed transaction", i)
		}
		if got.userID != "user-1" {
			t.Errorf("call %d: expected AuthInfo preserved in hook context, got user %q", i, got.userID)
		}
		if want.visible && got.getErr != nil {
			t.Errorf("call %d: expected committed row visible from hook, got %v", i, got.getErr)
		}
		if !want.visible && !errors.Is(got.getErr, apperrors.ErrNotFound) {
			t.Errorf("call %d: expected deleted row absent from hook, got %v", i, got.getErr)
		}
	}

	var logs []TestAuditLog
	if err := db.GetDB().NewSelect().Model(&logs).Scan(context.Background()); err != nil {
		t.Fatal("failed to query audit logs:", err)
	}
	if len(logs) != 4 {
		t.Errorf("expected 4 audit logs alongside the hooks, got %d", len(logs))
	}
}

func TestAfterCommit_HookErrorDoesNotFailOperation(t *testing.T) {
	db, cleanup := setupAuditTestDB(t)
	defer cleanup()

	calls := 0
	hook := metadata.AfterCommitFunc[TestAuditItem](func(metadata.AfterCommitContext[TestAuditItem]) error {
		calls++
		return errors.New("downstream unavailable")
	})
	wrapper := &datastore.Wrapper[TestAuditItem]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, afterCommitMeta(hook))

	created, err := wrapper.Create(ctx, TestAuditItem{Name: "Test", Status: "pending"})
	if err != nil {
		t.Fatal("create must succeed even when the hook fails:", err)
	}
	if calls != 1 {
		t.Errorf("expected hook to run once, ran %d times", calls)
	}
	if _, err := wrapper.Get(ctx, strconv.Itoa(created.ID)); err != nil {
		t.Errorf("created row must remain committed after hook failure: %v", err)
	}
}

func TestAfterCommit_HookPanicIsRecovered(t *testing.T) {
	db, cleanup := setupAuditTestDB(t)
	defer cleanup()

	hook := metadata.AfterCommitFunc[TestAuditItem](func(metadata.AfterCommitContext[TestAuditItem]) error {
		panic("hook exploded")
	})
	wrapper := &datastore.Wrapper[TestAuditItem]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, afterCommitMeta(hook))

	if _, err := wrapper.Create(ctx, TestAuditItem{Name: "Test", Status: "pending"}); err != nil {
		t.Fatal("create must succeed even when the hook panics:", err)
	}
}

func TestAfterCommit_BatchOperationsFireOncePerItem(t *testing.T) {
	db, cleanup := setupAuditTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestAuditItem]{Store: db}
	recorder := &afterCommitRecorder{}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, afterCommitMeta(recorder.hook(wrapper)))

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

	for i := range updates {
		updates[i].Status = "done"
	}
	if _, err := wrapper.BatchPatch(ctx, updates); err != nil {
		t.Fatal("batch patch failed:", err)
	}

	if err := wrapper.BatchDelete(ctx, updates); err != nil {
		t.Fatal("batch delete failed:", err)
	}

	calls := recorder.snapshot()
	if len(calls) != 8 {
		t.Fatalf("expected 8 hook calls (2 items x 4 operations), got %d", len(calls))
	}

	expected := []struct {
		op        metadata.Operation
		oldStatus string
		newStatus string
		visible   bool
	}{
		{metadata.OpCreate, "", "pending", true},
		{metadata.OpCreate, "", "pending", true},
		{metadata.OpUpdate, "pending", "active", true},
		{metadata.OpUpdate, "pending", "active", true},
		{metadata.OpPatch, "active", "done", true},
		{metadata.OpPatch, "active", "done", true},
		{metadata.OpDelete, "done", "", false},
		{metadata.OpDelete, "done", "", false},
	}
	for i, want := range expected {
		got := calls[i]
		if got.op != want.op {
			t.Errorf("call %d: expected op %q, got %q", i, want.op, got.op)
		}
		if got.oldStatus != want.oldStatus || got.newStatus != want.newStatus {
			t.Errorf("call %d: expected old/new %q/%q, got %q/%q", i, want.oldStatus, want.newStatus, got.oldStatus, got.newStatus)
		}
		if got.id != created[i%2].ID {
			t.Errorf("call %d: expected id %d, got %d", i, created[i%2].ID, got.id)
		}
		if want.visible && got.getErr != nil {
			t.Errorf("call %d: expected committed row visible from hook, got %v", i, got.getErr)
		}
		if !want.visible && !errors.Is(got.getErr, apperrors.ErrNotFound) {
			t.Errorf("call %d: expected deleted row absent from hook, got %v", i, got.getErr)
		}
	}
}

func TestAfterCommit_DeferredUntilTransactionOwnerCommits(t *testing.T) {
	db, cleanup := setupAuditTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestAuditItem]{Store: db}
	recorder := &afterCommitRecorder{}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, afterCommitMeta(recorder.hook(wrapper)))

	tx, err := db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal("begin failed:", err)
	}
	ctx = context.WithValue(ctx, metadata.RLSTxKey, tx)
	ctx = datastore.WithAfterCommitQueue(ctx)

	if _, err := wrapper.Create(ctx, TestAuditItem{Name: "Test", Status: "pending"}); err != nil {
		_ = tx.Rollback()
		t.Fatal("create failed:", err)
	}
	if got := len(recorder.snapshot()); got != 0 {
		_ = tx.Rollback()
		t.Fatalf("hook must not run before the owning transaction commits, ran %d times", got)
	}

	if err := tx.Commit(); err != nil {
		t.Fatal("commit failed:", err)
	}
	datastore.RunAfterCommit(ctx)

	calls := recorder.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected 1 hook call after commit, got %d", len(calls))
	}
	if calls[0].getErr != nil {
		t.Errorf("expected committed row visible from hook, got %v", calls[0].getErr)
	}
	if calls[0].txInCtx {
		t.Error("hook context must not carry the committed transaction")
	}

	datastore.RunAfterCommit(ctx)
	if got := len(recorder.snapshot()); got != 1 {
		t.Errorf("draining twice must not re-run hooks, got %d calls", got)
	}
}

func TestAfterCommit_SkippedWhenTransactionHasNoQueue(t *testing.T) {
	db, cleanup := setupAuditTestDB(t)
	defer cleanup()

	wrapper := &datastore.Wrapper[TestAuditItem]{Store: db}
	recorder := &afterCommitRecorder{}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, afterCommitMeta(recorder.hook(wrapper)))

	tx, err := db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal("begin failed:", err)
	}
	ctx = context.WithValue(ctx, metadata.RLSTxKey, tx)

	if _, err := wrapper.Create(ctx, TestAuditItem{Name: "Test", Status: "pending"}); err != nil {
		_ = tx.Rollback()
		t.Fatal("create failed:", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal("commit failed:", err)
	}
	datastore.RunAfterCommit(ctx)

	if got := len(recorder.snapshot()); got != 0 {
		t.Errorf("hook must be skipped when the transaction owner provides no queue, ran %d times", got)
	}
}

func TestAfterCommit_SkippedOnTypeMismatch(t *testing.T) {
	db, cleanup := setupAuditTestDB(t)
	defer cleanup()

	calls := 0
	mismatched := metadata.AfterCommitFunc[TestAuditLog](func(metadata.AfterCommitContext[TestAuditLog]) error {
		calls++
		return nil
	})
	wrapper := &datastore.Wrapper[TestAuditItem]{Store: db}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, afterCommitMeta(mismatched))

	if _, err := wrapper.Create(ctx, TestAuditItem{Name: "Test", Status: "pending"}); err != nil {
		t.Fatal("create failed:", err)
	}
	if calls != 0 {
		t.Errorf("hook of the wrong type must be skipped, ran %d times", calls)
	}
}

func TestAfterCommit_RLSRouteFailsClosedWhenTenantTxCannotOpen(t *testing.T) {
	db, cleanup := setupAuditTestDB(t)
	defer cleanup()

	calls := 0
	hook := metadata.AfterCommitFunc[TestAuditItem](func(metadata.AfterCommitContext[TestAuditItem]) error {
		calls++
		return nil
	})
	meta := afterCommitMeta(hook)
	meta.UseRLS = true
	wrapper := &datastore.Wrapper[TestAuditItem]{Store: db}
	// SQLite has no set_config, so the tenant transaction cannot be scoped.
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, meta)
	ctx = context.WithValue(ctx, metadata.TenantIDValueKey, "org-a")

	if _, err := wrapper.Create(ctx, TestAuditItem{Name: "Test", Status: "pending"}); err != nil {
		t.Fatal("create must still succeed:", err)
	}
	if calls != 0 {
		t.Errorf("hook must never run unscoped on an RLS route, ran %d times", calls)
	}
}

func TestBeginTenantTx_FailureReleasesConnection(t *testing.T) {
	db, cleanup := setupAuditTestDB(t)
	defer cleanup()

	// With a single connection, a leaked transaction would block the next query.
	db.GetDB().SetMaxOpenConns(1)

	if _, err := datastore.BeginTenantTx(context.Background(), db.GetDB(), "org-a"); err == nil {
		t.Fatal("expected BeginTenantTx to fail on SQLite (no set_config)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := db.GetDB().NewSelect().Model((*TestAuditItem)(nil)).Count(ctx); err != nil {
		t.Errorf("connection must be released after a failed BeginTenantTx: %v", err)
	}
}

func TestRunAfterCommit_NoQueueIsNoop(t *testing.T) {
	datastore.RunAfterCommit(context.Background())
}
