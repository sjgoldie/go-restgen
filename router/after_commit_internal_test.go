package router

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/metadata"
)

type rlsHookModel struct {
	bun.BaseModel `bun:"table:rls_hook_models"`
	ID            int    `bun:"id,pk,autoincrement"`
	Name          string `bun:"name,notnull"`
	OrgID         string `bun:"org_id"`
}

const insertHookModelSQL = `^INSERT INTO "rls_hook_models"`

func hookModelRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "name", "org_id"}).AddRow(1, "hooked", "")
}

// rlsHookRequest builds a request for tenant org-a on an RLS route whose metadata carries an after-commit hook.
func rlsHookRequest(hook metadata.AfterCommitFunc[rlsHookModel]) *http.Request {
	meta := &metadata.TypeMetadata{
		TypeID:       "rls_hook_model",
		TypeName:     "RLSHookModel",
		TableName:    "rls_hook_models",
		URLParamUUID: "id",
		PKField:      "ID",
		ModelType:    reflect.TypeOf(rlsHookModel{}),
		TenantField:  "OrgID",
		UseRLS:       true,
		AfterCommit:  hook,
	}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, meta)
	ctx = context.WithValue(ctx, metadata.TenantIDValueKey, "org-a")
	return httptest.NewRequest("POST", "/", nil).WithContext(ctx)
}

// createThroughWrapper performs a datastore create on the request context, the
// way a handler does, so the write lands on the middleware's transaction.
func createThroughWrapper(t *testing.T, r *http.Request) {
	t.Helper()
	store, err := datastore.Get()
	if err != nil {
		t.Fatal("datastore not initialised:", err)
	}
	wrapper := &datastore.Wrapper[rlsHookModel]{Store: store}
	if _, err := wrapper.Create(r.Context(), rlsHookModel{Name: "hooked"}); err != nil {
		t.Errorf("create failed: %v", err)
	}
}

func TestWrapWithRLS_AfterCommitHookRunsAfterCommitInFreshTenantTx(t *testing.T) {
	mock := useSqlmock(t)
	// Request transaction: begin, scope, insert, commit.
	mock.ExpectBegin()
	mock.ExpectExec(setConfigSQLFor("org-a")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(insertHookModelSQL).WillReturnRows(hookModelRows())
	mock.ExpectCommit()
	// Hook transaction: a fresh begin scoped to the same tenant, then commit.
	mock.ExpectBegin()
	mock.ExpectExec(setConfigSQLFor("org-a")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	var sequence []string
	hook := func(ac metadata.AfterCommitContext[rlsHookModel]) error {
		if _, ok := ac.Ctx.Value(metadata.RLSTxKey).(bun.Tx); !ok {
			t.Error("hook context must carry a tenant transaction on an RLS route")
		}
		if ac.Operation != metadata.OpCreate || ac.New == nil || ac.New.ID != 1 {
			t.Errorf("unexpected hook payload: op=%s new=%v", ac.Operation, ac.New)
		}
		sequence = append(sequence, "hook")
		return nil
	}

	wrapped := wrapWithRLS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		createThroughWrapper(t, r)
		w.WriteHeader(http.StatusCreated)
		sequence = append(sequence, "response")
	}))

	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, rlsHookRequest(hook))

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if len(sequence) != 2 || sequence[0] != "response" || sequence[1] != "hook" {
		t.Errorf("expected hook to run after the response, got sequence %v", sequence)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sqlmock expectations not met: %v", err)
	}
}

func TestWrapWithRLS_AfterCommitHookErrorRollsBackHookTx(t *testing.T) {
	mock := useSqlmock(t)
	mock.ExpectBegin()
	mock.ExpectExec(setConfigSQLFor("org-a")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(insertHookModelSQL).WillReturnRows(hookModelRows())
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectExec(setConfigSQLFor("org-a")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	hook := func(metadata.AfterCommitContext[rlsHookModel]) error {
		return errors.New("workflow unavailable")
	}

	wrapped := wrapWithRLS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		createThroughWrapper(t, r)
		w.WriteHeader(http.StatusCreated)
	}))

	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, rlsHookRequest(hook))

	if w.Code != http.StatusCreated {
		t.Errorf("hook failure must not change the response, got %d", w.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sqlmock expectations not met: %v", err)
	}
}

func TestWrapWithRLS_AfterCommitHooksDiscardedOnErrorResponse(t *testing.T) {
	mock := useSqlmock(t)
	mock.ExpectBegin()
	mock.ExpectExec(setConfigSQLFor("org-a")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(insertHookModelSQL).WillReturnRows(hookModelRows())
	mock.ExpectRollback()

	hookCalled := false
	hook := func(metadata.AfterCommitContext[rlsHookModel]) error {
		hookCalled = true
		return nil
	}

	wrapped := wrapWithRLS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		createThroughWrapper(t, r)
		w.WriteHeader(http.StatusConflict)
	}))

	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, rlsHookRequest(hook))

	if hookCalled {
		t.Error("hooks must not run when the request transaction rolls back")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sqlmock expectations not met: %v", err)
	}
}

func TestWrapWithRLS_RollsBackAndDiscardsHooksWhenHandlerPanics(t *testing.T) {
	mock := useSqlmock(t)
	mock.ExpectBegin()
	mock.ExpectExec(setConfigSQLFor("org-a")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(insertHookModelSQL).WillReturnRows(hookModelRows())
	mock.ExpectRollback()

	hookCalled := false
	hook := func(metadata.AfterCommitContext[rlsHookModel]) error {
		hookCalled = true
		return errors.New("hook must not run after a handler panic")
	}

	wrapped := wrapWithRLS(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		createThroughWrapper(t, r)
		panic("handler exploded")
	}))

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		wrapped.ServeHTTP(httptest.NewRecorder(), rlsHookRequest(hook))
	}()

	if recovered != "handler exploded" {
		t.Errorf("panic must propagate to the outer recoverer, got %v", recovered)
	}
	if hookCalled {
		t.Error("hooks must not run when the handler panics")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("transaction must be rolled back on panic: %v", err)
	}
}

func TestRLSStatusRecorder_FlushDelegatesAndImpliesHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &rlsStatusRecorder{ResponseWriter: rec, status: http.StatusOK}

	rw.Flush()

	if !rec.Flushed {
		t.Error("expected Flush to reach the underlying ResponseWriter")
	}
	if !rw.wroteHeader {
		t.Error("expected Flush to mark the header as written")
	}
	rw.WriteHeader(http.StatusInternalServerError)
	if rw.status != http.StatusOK {
		t.Errorf("status captured after Flush must stay 200, got %d", rw.status)
	}
}

func TestRLSStatusRecorder_Unwrap(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &rlsStatusRecorder{ResponseWriter: rec, status: http.StatusOK}

	if rw.Unwrap() != rec {
		t.Error("expected Unwrap to return the underlying ResponseWriter")
	}
}

func TestWrapWithRLS_SSEHandlerCanFlushThroughRecorder(t *testing.T) {
	mock := useSqlmock(t)
	mock.ExpectBegin()
	mock.ExpectExec(setConfigSQLFor("org-a")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	wrapped := wrapWithRLS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, ok := w.(http.Flusher); !ok {
			t.Error("ResponseWriter on an RLS route must implement http.Flusher for SSE")
		}
		w.WriteHeader(http.StatusOK)
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("ResponseController flush failed: %v", err)
		}
	}))

	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, rlsRequestWithRLSMeta("org-a"))

	if !rec.Flushed {
		t.Error("expected flush to reach the client")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sqlmock expectations not met: %v", err)
	}
}
