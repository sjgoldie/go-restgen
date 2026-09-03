package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sjgoldie/go-restgen/metadata"
)

func TestMetadataMiddleware_ReportsTypeNameToSeededHolder(t *testing.T) {
	meta := &metadata.TypeMetadata{TypeName: "Widget", MaxBodySize: metadata.DefaultMaxBodySize}
	mw := createMetadataMiddleware(meta)

	holder := &metadata.ResourceName{}
	req := httptest.NewRequest("GET", "/widgets", nil)
	req = req.WithContext(context.WithValue(req.Context(), metadata.ResourceNameKey, holder))

	called := false
	mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(httptest.NewRecorder(), req)

	if !called {
		t.Fatal("inner handler was not called")
	}
	if holder.Name != "Widget" {
		t.Errorf("holder.Name = %q, want Widget", holder.Name)
	}
}

func TestMetadataMiddleware_NoHolderIsFine(t *testing.T) {
	meta := &metadata.TypeMetadata{TypeName: "Widget", MaxBodySize: metadata.DefaultMaxBodySize}
	mw := createMetadataMiddleware(meta)

	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, httptest.NewRequest("GET", "/widgets", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}
