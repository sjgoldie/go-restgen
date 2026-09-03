package datastore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/metadata"
)

// sqlmockStore is a Store backed by go-sqlmock with the PostgreSQL dialect, so
// the RLS paths (set_config, tenant transactions) can be exercised without a
// live PostgreSQL. bun inlines placeholders before the driver sees the SQL, so
// expectations match the rendered statement.
type sqlmockStore struct {
	db *bun.DB
}

func (s *sqlmockStore) GetDB() *bun.DB            { return s.db }
func (s *sqlmockStore) GetTimeout() time.Duration { return 30 * time.Second }
func (s *sqlmockStore) IlikeOp() string           { return "ILIKE" }
func (s *sqlmockStore) Cleanup()                  {}

func newSqlmockStore(t *testing.T) (*sqlmockStore, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal("sqlmock.New:", err)
	}
	db := bun.NewDB(sqlDB, pgdialect.New())
	t.Cleanup(func() {
		_ = db.Close()
		_ = sqlDB.Close()
	})
	return &sqlmockStore{db: db}, mock
}

const (
	setConfigOrgASQL   = `^SELECT set_config\('app\.tenant_id', 'org-a', true\)$`
	insertAuditItemSQL = `^INSERT INTO "test_audit_items"`
)

func auditItemRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "name", "status"}).AddRow(1, "Test", "pending")
}

// expectRequestTx scripts the request transaction the RLS middleware would open:
// begin, scope to the tenant, the create's insert, then commit.
func expectRequestTx(mock sqlmock.Sqlmock) {
	mock.ExpectBegin()
	mock.ExpectExec(setConfigOrgASQL).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(insertAuditItemSQL).WillReturnRows(auditItemRows())
	mock.ExpectCommit()
}

// runCreateUnderRLS mirrors the RLS request regime: tenant transaction in
// context with an after-commit queue, a create on it, commit, then drain.
func runCreateUnderRLS(t *testing.T, store *sqlmockStore, hook metadata.AfterCommitFunc[TestAuditItem]) {
	t.Helper()

	meta := afterCommitMeta(hook)
	meta.UseRLS = true
	wrapper := &datastore.Wrapper[TestAuditItem]{Store: store}

	ctx := context.WithValue(context.Background(), metadata.MetadataKey, meta)
	ctx = context.WithValue(ctx, metadata.TenantIDValueKey, "org-a")

	reqTx, err := datastore.BeginTenantTx(ctx, store.GetDB(), "org-a")
	if err != nil {
		t.Fatal("failed to begin request transaction:", err)
	}
	ctx = context.WithValue(ctx, metadata.RLSTxKey, reqTx)
	ctx = datastore.WithAfterCommitQueue(ctx)

	if _, err := wrapper.Create(ctx, TestAuditItem{Name: "Test", Status: "pending"}); err != nil {
		t.Fatal("create failed:", err)
	}
	if err := reqTx.Commit(); err != nil {
		t.Fatal("commit failed:", err)
	}
	datastore.RunAfterCommit(ctx)
}

func TestAfterCommit_RLSHookRunsInFreshTenantTx(t *testing.T) {
	store, mock := newSqlmockStore(t)
	expectRequestTx(mock)
	mock.ExpectBegin()
	mock.ExpectExec(setConfigOrgASQL).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	calls := 0
	var hookTxPresent bool
	hook := metadata.AfterCommitFunc[TestAuditItem](func(ac metadata.AfterCommitContext[TestAuditItem]) error {
		calls++
		_, hookTxPresent = ac.Ctx.Value(metadata.RLSTxKey).(bun.Tx)
		if ac.Operation != metadata.OpCreate || ac.New == nil || ac.New.ID != 1 {
			t.Errorf("unexpected hook payload: op=%s new=%v", ac.Operation, ac.New)
		}
		return nil
	})

	runCreateUnderRLS(t, store, hook)

	if calls != 1 {
		t.Fatalf("expected 1 hook call, got %d", calls)
	}
	if !hookTxPresent {
		t.Error("hook context must carry a tenant-scoped transaction on an RLS route")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sqlmock expectations not met: %v", err)
	}
}

func TestAfterCommit_RLSHookErrorRollsBackHookTx(t *testing.T) {
	store, mock := newSqlmockStore(t)
	expectRequestTx(mock)
	mock.ExpectBegin()
	mock.ExpectExec(setConfigOrgASQL).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	hook := metadata.AfterCommitFunc[TestAuditItem](func(metadata.AfterCommitContext[TestAuditItem]) error {
		return errors.New("workflow unavailable")
	})

	runCreateUnderRLS(t, store, hook)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("hook error must roll back the hook transaction: %v", err)
	}
}

func TestAfterCommit_RLSHookPanicRollsBackHookTx(t *testing.T) {
	store, mock := newSqlmockStore(t)
	expectRequestTx(mock)
	mock.ExpectBegin()
	mock.ExpectExec(setConfigOrgASQL).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	hook := metadata.AfterCommitFunc[TestAuditItem](func(metadata.AfterCommitContext[TestAuditItem]) error {
		panic("hook exploded")
	})

	runCreateUnderRLS(t, store, hook)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("hook panic must roll back the hook transaction: %v", err)
	}
}

func TestAfterCommit_RLSHookCommitFailureIsLogged(t *testing.T) {
	store, mock := newSqlmockStore(t)
	expectRequestTx(mock)
	mock.ExpectBegin()
	mock.ExpectExec(setConfigOrgASQL).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit().WillReturnError(errors.New("commit failed"))

	hook := metadata.AfterCommitFunc[TestAuditItem](func(metadata.AfterCommitContext[TestAuditItem]) error {
		return nil
	})

	runCreateUnderRLS(t, store, hook)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sqlmock expectations not met: %v", err)
	}
}

func TestAfterCommit_RLSHookSkippedWhenSetConfigFails(t *testing.T) {
	store, mock := newSqlmockStore(t)
	expectRequestTx(mock)
	mock.ExpectBegin()
	mock.ExpectExec(setConfigOrgASQL).WillReturnError(errors.New("set_config failed"))
	mock.ExpectRollback()

	calls := 0
	hook := metadata.AfterCommitFunc[TestAuditItem](func(metadata.AfterCommitContext[TestAuditItem]) error {
		calls++
		return nil
	})

	runCreateUnderRLS(t, store, hook)

	if calls != 0 {
		t.Errorf("hook must never run unscoped, ran %d times", calls)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("failed scoping must roll back the hook transaction: %v", err)
	}
}

func TestAfterCommit_RLSHookSkippedWhenBeginFails(t *testing.T) {
	store, mock := newSqlmockStore(t)
	expectRequestTx(mock)
	mock.ExpectBegin().WillReturnError(errors.New("begin failed"))

	calls := 0
	hook := metadata.AfterCommitFunc[TestAuditItem](func(metadata.AfterCommitContext[TestAuditItem]) error {
		calls++
		return nil
	})

	runCreateUnderRLS(t, store, hook)

	if calls != 0 {
		t.Errorf("hook must not run when its transaction cannot be opened, ran %d times", calls)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sqlmock expectations not met: %v", err)
	}
}
