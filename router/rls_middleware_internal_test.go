package router

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/sjgoldie/go-restgen/metadata"
)

// rlsRequestWithRLSMeta builds a request with the metadata + tenant ID context
// values that wrapWithRLS expects when UseRLS=true.
func rlsRequestWithRLSMeta(tenantID string) *http.Request {
	meta := &metadata.TypeMetadata{UseRLS: true, TenantField: "OrgID"}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, meta)
	ctx = context.WithValue(ctx, metadata.TenantIDValueKey, tenantID)
	return httptest.NewRequest("GET", "/", nil).WithContext(ctx)
}

// setConfigSQLFor returns a regex pattern matching the SET LOCAL SQL bun
// renders for the given tenant ID. bun inlines ? placeholders before reaching
// the driver, so the tenant ID appears literally in the SQL string.
func setConfigSQLFor(tenantID string) string {
	return `^SELECT set_config\('app\.tenant_id', '` + tenantID + `', true\)$`
}

func TestRLSStatusRecorder_DefaultStatusIs200(t *testing.T) {
	w := httptest.NewRecorder()
	rw := &rlsStatusRecorder{ResponseWriter: w, status: http.StatusOK}

	if rw.status != http.StatusOK {
		t.Errorf("expected default status 200, got %d", rw.status)
	}
}

func TestRLSStatusRecorder_WriteHeaderCapturesStatus(t *testing.T) {
	w := httptest.NewRecorder()
	rw := &rlsStatusRecorder{ResponseWriter: w, status: http.StatusOK}

	rw.WriteHeader(http.StatusCreated)

	if rw.status != http.StatusCreated {
		t.Errorf("expected captured status 201, got %d", rw.status)
	}
	if w.Code != http.StatusCreated {
		t.Errorf("expected underlying ResponseWriter status 201, got %d", w.Code)
	}
}

func TestRLSStatusRecorder_WriteHeaderIdempotent(t *testing.T) {
	w := httptest.NewRecorder()
	rw := &rlsStatusRecorder{ResponseWriter: w, status: http.StatusOK}

	rw.WriteHeader(http.StatusCreated)
	rw.WriteHeader(http.StatusInternalServerError)

	if rw.status != http.StatusCreated {
		t.Errorf("expected first captured status 201 (subsequent WriteHeader calls ignored), got %d", rw.status)
	}
}

func TestRLSStatusRecorder_WriteMarksHeaderWritten(t *testing.T) {
	w := httptest.NewRecorder()
	rw := &rlsStatusRecorder{ResponseWriter: w, status: http.StatusOK}

	n, err := rw.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
	if n != 5 {
		t.Errorf("expected 5 bytes written, got %d", n)
	}
	if !rw.wroteHeader {
		t.Error("expected wroteHeader to be true after Write")
	}
	if rw.status != http.StatusOK {
		t.Errorf("expected implicit status 200 when Write called first, got %d", rw.status)
	}
}

func TestRLSStatusRecorder_WriteAfterWriteHeader(t *testing.T) {
	w := httptest.NewRecorder()
	rw := &rlsStatusRecorder{ResponseWriter: w, status: http.StatusOK}

	rw.WriteHeader(http.StatusAccepted)
	_, _ = rw.Write([]byte("body"))

	if rw.status != http.StatusAccepted {
		t.Errorf("expected captured status 202, got %d", rw.status)
	}
}

// TestWrapWithRLS_NoMetadataPassesThrough verifies the middleware is a no-op
// when there's no metadata in context (e.g., if metadata middleware hasn't run).
func TestWrapWithRLS_NoMetadataPassesThrough(t *testing.T) {
	handlerCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	wrapped := wrapWithRLS(next)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	if !handlerCalled {
		t.Error("expected handler to be called when no metadata in context")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// TestWrapWithRLS_UseRLSFalsePassesThrough verifies the middleware is a no-op
// when UseRLS is false on the route metadata.
func TestWrapWithRLS_UseRLSFalsePassesThrough(t *testing.T) {
	handlerCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	wrapped := wrapWithRLS(next)

	meta := &metadata.TypeMetadata{UseRLS: false, TenantField: "OrgID"}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, meta)

	req := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	if !handlerCalled {
		t.Error("expected handler to be called when UseRLS=false")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// TestWrapWithRLS_MissingTenantIDPassesThrough verifies the middleware is a no-op
// when UseRLS=true but no tenant ID is set in context. In practice the auth
// middleware rejects this before reaching RLS, but the defensive check is
// still worth testing.
func TestWrapWithRLS_MissingTenantIDPassesThrough(t *testing.T) {
	handlerCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	wrapped := wrapWithRLS(next)

	meta := &metadata.TypeMetadata{UseRLS: true, TenantField: "OrgID"}
	ctx := context.WithValue(context.Background(), metadata.MetadataKey, meta)

	req := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	if !handlerCalled {
		t.Error("expected handler to be called when tenant ID is missing (defensive pass-through)")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// =============================================================================
// sqlmock-backed unit tests for the RLS transaction lifecycle.
// =============================================================================

func TestWrapWithRLS_CommitsOnSuccess(t *testing.T) {
	mock := useSqlmock(t)
	mock.ExpectBegin()
	mock.ExpectExec(setConfigSQLFor("org-a")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	handlerCalled := false
	wrapped := wrapWithRLS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, rlsRequestWithRLSMeta("org-a"))

	if !handlerCalled {
		t.Error("handler was not called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sqlmock expectations not met: %v", err)
	}
}

func TestWrapWithRLS_RollsBackOnClientError(t *testing.T) {
	mock := useSqlmock(t)
	mock.ExpectBegin()
	mock.ExpectExec(setConfigSQLFor("org-a")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	wrapped := wrapWithRLS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))

	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, rlsRequestWithRLSMeta("org-a"))

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sqlmock expectations not met: %v", err)
	}
}

func TestWrapWithRLS_RollsBackOnServerError(t *testing.T) {
	mock := useSqlmock(t)
	mock.ExpectBegin()
	mock.ExpectExec(setConfigSQLFor("org-b")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	wrapped := wrapWithRLS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, rlsRequestWithRLSMeta("org-b"))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sqlmock expectations not met: %v", err)
	}
}

func TestWrapWithRLS_BeginTxErrorReturns500(t *testing.T) {
	mock := useSqlmock(t)
	mock.ExpectBegin().WillReturnError(errors.New("begin failed"))

	handlerCalled := false
	wrapped := wrapWithRLS(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		handlerCalled = true
	}))

	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, rlsRequestWithRLSMeta("org-a"))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 when BeginTx fails, got %d", w.Code)
	}
	if handlerCalled {
		t.Error("handler should not be called when BeginTx fails")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sqlmock expectations not met: %v", err)
	}
}

func TestWrapWithRLS_SetConfigErrorRollsBackAndReturns500(t *testing.T) {
	mock := useSqlmock(t)
	mock.ExpectBegin()
	mock.ExpectExec(setConfigSQLFor("org-a")).WillReturnError(errors.New("set_config failed"))
	mock.ExpectRollback()

	handlerCalled := false
	wrapped := wrapWithRLS(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		handlerCalled = true
	}))

	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, rlsRequestWithRLSMeta("org-a"))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 when set_config fails, got %d", w.Code)
	}
	if handlerCalled {
		t.Error("handler should not be called when set_config fails")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sqlmock expectations not met: %v", err)
	}
}

func TestWrapWithRLS_CommitErrorIsLoggedNotPropagated(t *testing.T) {
	mock := useSqlmock(t)
	mock.ExpectBegin()
	mock.ExpectExec(setConfigSQLFor("org-a")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit().WillReturnError(errors.New("commit failed"))

	wrapped := wrapWithRLS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, rlsRequestWithRLSMeta("org-a"))

	// Commit failed but the response was already sent; status should be unchanged
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 (response written before commit), got %d", w.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sqlmock expectations not met: %v", err)
	}
}

func TestWrapWithRLS_RollbackErrorIsLoggedNotPropagated(t *testing.T) {
	mock := useSqlmock(t)
	mock.ExpectBegin()
	mock.ExpectExec(setConfigSQLFor("org-a")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback().WillReturnError(errors.New("rollback failed"))

	wrapped := wrapWithRLS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))

	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, rlsRequestWithRLSMeta("org-a"))

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 (response written before rollback), got %d", w.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sqlmock expectations not met: %v", err)
	}
}

func TestWrapWithRLS_TxAvailableInHandlerContext(t *testing.T) {
	mock := useSqlmock(t)
	mock.ExpectBegin()
	mock.ExpectExec(setConfigSQLFor("org-a")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	var txInCtx interface{}
	wrapped := wrapWithRLS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		txInCtx = r.Context().Value(metadata.RLSTxKey)
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, rlsRequestWithRLSMeta("org-a"))

	if txInCtx == nil {
		t.Error("expected RLS transaction to be in handler context")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sqlmock expectations not met: %v", err)
	}
}
