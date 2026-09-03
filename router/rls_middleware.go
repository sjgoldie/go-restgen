package router

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/handler"
	"github.com/sjgoldie/go-restgen/metadata"
)

// wrapWithRLS wraps a handler with PostgreSQL Row-Level Security support.
// When the route has UseRLS=true and a tenant ID is in context, the request
// is wrapped in a transaction with set_config('app.tenant_id', <id>, true)
// so that PostgreSQL RLS policies can scope queries to the tenant.
//
// On commit/rollback: the transaction commits if the handler returns 2xx/3xx,
// rolls back on 4xx/5xx. This is consistent with HTTP semantics — error
// responses imply the request should not have side effects. If the handler
// panics the transaction is rolled back before the panic propagates to any
// outer recoverer, so the connection is never left held.
//
// After a successful commit, after-commit hooks queued by the datastore during
// the request (see WithAfterCommit) are run. Hooks are discarded on rollback.
//
// If UseRLS is false or no tenant ID is set, this middleware is a pass-through.
func wrapWithRLS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		meta, _ := metadata.FromContext(ctx)
		if meta == nil || !meta.UseRLS {
			next.ServeHTTP(w, r)
			return
		}

		tenantID, ok := ctx.Value(metadata.TenantIDValueKey).(string)
		if !ok || tenantID == "" {
			next.ServeHTTP(w, r)
			return
		}

		store, err := datastore.Get()
		if err != nil {
			slog.ErrorContext(ctx, "RLS middleware: datastore not initialized", "error", err)
			handler.WriteError(w, http.StatusInternalServerError, handler.ErrCodeInternalError, http.StatusText(http.StatusInternalServerError))
			return
		}

		tx, err := datastore.BeginTenantTx(ctx, store.GetDB(), tenantID)
		if err != nil {
			slog.ErrorContext(ctx, "RLS middleware: failed to begin tenant transaction", "error", err, "tenant_id", tenantID)
			handler.WriteError(w, http.StatusInternalServerError, handler.ErrCodeInternalError, http.StatusText(http.StatusInternalServerError))
			return
		}

		rw := &rlsStatusRecorder{ResponseWriter: w, status: http.StatusOK}

		ctx = context.WithValue(ctx, metadata.RLSTxKey, tx)
		ctx = datastore.WithAfterCommitQueue(ctx)

		handlerReturned := false
		defer func() {
			if handlerReturned {
				return
			}
			// The handler panicked. Release the transaction, then let the panic
			// continue to the outer recoverer.
			if err := tx.Rollback(); err != nil {
				slog.ErrorContext(ctx, "RLS middleware: rollback after panic failed", "error", err)
			}
		}()

		next.ServeHTTP(rw, r.WithContext(ctx))
		handlerReturned = true

		if rw.status >= 400 {
			if err := tx.Rollback(); err != nil {
				slog.ErrorContext(ctx, "RLS middleware: rollback failed", "error", err)
			}
			return
		}

		if err := tx.Commit(); err != nil {
			slog.ErrorContext(ctx, "RLS middleware: commit failed", "error", err)
			return
		}

		datastore.RunAfterCommit(ctx)
	})
}

// rlsStatusRecorder wraps http.ResponseWriter to capture the response status code
// so the RLS middleware can decide whether to commit or roll back the transaction.
type rlsStatusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *rlsStatusRecorder) WriteHeader(status int) {
	if !r.wroteHeader {
		r.status = status
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *rlsStatusRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.wroteHeader = true
	}
	return r.ResponseWriter.Write(b)
}

// Flush implements http.Flusher by delegating to the underlying ResponseWriter,
// so SSE handlers on RLS routes stream events instead of buffering them.
// Flushing before WriteHeader implicitly sends a 200, matching net/http.
func (r *rlsStatusRecorder) Flush() {
	r.wroteHeader = true
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap returns the underlying ResponseWriter for http.ResponseController compatibility.
func (r *rlsStatusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}
