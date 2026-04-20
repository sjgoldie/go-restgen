package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sjgoldie/go-restgen/metadata"
)

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
