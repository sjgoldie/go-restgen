package datastore

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/metadata"
)

// afterCommitQueue collects hooks that must wait for the transaction owner to commit.
type afterCommitQueue struct {
	mu  sync.Mutex
	fns []func()
}

func (q *afterCommitQueue) add(fn func()) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.fns = append(q.fns, fn)
}

// drain removes and returns all queued hooks.
func (q *afterCommitQueue) drain() []func() {
	q.mu.Lock()
	defer q.mu.Unlock()
	fns := q.fns
	q.fns = nil
	return fns
}

// afterCommitQueueKeyType is the context key type for the after-commit queue
type afterCommitQueueKeyType string

// afterCommitQueueKey is the context key for the after-commit queue
const afterCommitQueueKey afterCommitQueueKeyType = "restgen_after_commit_queue"

// WithAfterCommitQueue returns a context carrying a queue for after-commit hooks.
// Whoever places a transaction in context under metadata.RLSTxKey owns that
// transaction and must also call this, then call RunAfterCommit once the
// transaction has committed. Hooks queued on a transaction that rolls back are
// simply never run.
func WithAfterCommitQueue(ctx context.Context) context.Context {
	return context.WithValue(ctx, afterCommitQueueKey, &afterCommitQueue{})
}

// RunAfterCommit runs, in order, every after-commit hook queued on ctx and clears the queue.
// It is a no-op when ctx carries no queue.
func RunAfterCommit(ctx context.Context) {
	q := queueFromContext(ctx)
	if q == nil {
		return
	}
	for _, fn := range q.drain() {
		fn()
	}
}

func queueFromContext(ctx context.Context) *afterCommitQueue {
	q, _ := ctx.Value(afterCommitQueueKey).(*afterCommitQueue)
	return q
}

// BeginTenantTx begins a transaction on db and scopes it to tenantID with
// SELECT set_config('app.tenant_id', tenantID, true), so PostgreSQL row-level
// security policies apply to every statement on the returned transaction.
// The setting is transaction-local and dies with the transaction.
// If scoping fails the transaction is rolled back and the error returned, so a
// caller never receives an unscoped transaction.
func BeginTenantTx(ctx context.Context, db *bun.DB, tenantID string) (bun.Tx, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return bun.Tx{}, fmt.Errorf("begin tenant transaction: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', ?, true)", tenantID); err != nil {
		_ = tx.Rollback()
		return bun.Tx{}, fmt.Errorf("set tenant id: %w", err)
	}
	return tx, nil
}

// afterCommit schedules the configured after-commit hook for a successful mutation.
// Outside an RLS transaction the wrapper's own write has already committed, so the
// hook runs immediately. Inside an RLS transaction the hook is queued for the
// transaction owner to run after commit.
func (w *Wrapper[T]) afterCommit(ctx context.Context, meta *metadata.TypeMetadata, op metadata.Operation, oldItem, newItem *T) {
	if meta.AfterCommit == nil {
		return
	}
	hook, ok := meta.AfterCommit.(metadata.AfterCommitFunc[T])
	if !ok {
		return
	}

	hookCtx := afterCommitContext(ctx)
	run := func() { w.runAfterCommit(hookCtx, meta, hook, op, oldItem, newItem) }

	if _, inTx := ctx.Value(metadata.RLSTxKey).(bun.Tx); !inTx {
		run()
		return
	}

	q := queueFromContext(ctx)
	if q == nil {
		slog.ErrorContext(ctx, "after-commit hook skipped: transaction in context has no after-commit queue; the transaction owner must use datastore.WithAfterCommitQueue and datastore.RunAfterCommit",
			"type", meta.TypeName, "operation", op)
		return
	}
	q.add(run)
}

// afterCommitContext derives the hook context from the request context.
// Request values are preserved, cancellation and deadlines are dropped so a
// client disconnect after commit cannot abort the hook, and the committed
// transaction is removed so datastore calls fall back to the pool (or to the
// fresh tenant transaction that runAfterCommit installs).
func afterCommitContext(ctx context.Context) context.Context {
	ctx = context.WithoutCancel(ctx)
	return context.WithValue(ctx, metadata.RLSTxKey, nil)
}

// runAfterCommit invokes the hook, wrapping it in a fresh tenant-scoped
// transaction when the route uses RLS. The hook is never invoked without that
// scoping when RLS is configured: if the tenant transaction cannot be opened
// the hook is skipped and the failure logged.
func (w *Wrapper[T]) runAfterCommit(ctx context.Context, meta *metadata.TypeMetadata, hook metadata.AfterCommitFunc[T], op metadata.Operation, oldItem, newItem *T) {
	tenantID, _ := ctx.Value(metadata.TenantIDValueKey).(string)
	if !meta.UseRLS || tenantID == "" {
		if err := w.invokeAfterCommit(ctx, hook, op, oldItem, newItem); err != nil {
			slog.ErrorContext(ctx, "after-commit hook failed", "type", meta.TypeName, "operation", op, "error", err)
		}
		return
	}

	tx, err := BeginTenantTx(ctx, w.Store.GetDB(), tenantID)
	if err != nil {
		slog.ErrorContext(ctx, "after-commit hook skipped: failed to begin tenant transaction",
			"type", meta.TypeName, "operation", op, "tenant_id", tenantID, "error", err)
		return
	}
	ctx = context.WithValue(ctx, metadata.RLSTxKey, tx)

	if err := w.invokeAfterCommit(ctx, hook, op, oldItem, newItem); err != nil {
		slog.ErrorContext(ctx, "after-commit hook failed", "type", meta.TypeName, "operation", op, "error", err)
		if err := tx.Rollback(); err != nil {
			slog.ErrorContext(ctx, "after-commit hook: rollback failed", "type", meta.TypeName, "operation", op, "error", err)
		}
		return
	}
	if err := tx.Commit(); err != nil {
		slog.ErrorContext(ctx, "after-commit hook: commit failed", "type", meta.TypeName, "operation", op, "error", err)
	}
}

// invokeAfterCommit calls the hook, converting a panic into an error so the
// caller can roll back and log without unwinding the request.
func (w *Wrapper[T]) invokeAfterCommit(ctx context.Context, hook metadata.AfterCommitFunc[T], op metadata.Operation, oldItem, newItem *T) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("after-commit hook panicked: %v", r)
		}
	}()
	return hook(metadata.AfterCommitContext[T]{
		Operation: op,
		Old:       oldItem,
		New:       newItem,
		Ctx:       ctx,
	})
}
