package router

import "github.com/sjgoldie/go-restgen/metadata"

// AfterCommitConfig holds after-commit hook configuration for route registration.
// This wraps the generic hook function for passing through the options interface{}.
type AfterCommitConfig[T any] struct {
	Fn metadata.AfterCommitFunc[T]
}

// WithAfterCommit creates an AfterCommitConfig for use in RegisterRoutes.
// The hook runs after a successful Create, Update, Patch, or Delete (including
// batch operations, once per item) has been durably committed, so side effects
// such as firing a workflow or publishing an event never observe uncommitted data.
//
// Without RLS the hook runs as soon as the datastore write has committed. With
// RLS (WithTenantScope(field, true)) the request transaction is owned by the
// framework and the hook runs after that transaction commits, inside a fresh
// transaction scoped to the same tenant via set_config('app.tenant_id', ...),
// so datastore calls made from the hook are row-level-security scoped exactly
// as they are during the request. If that transaction cannot be opened the
// hook is skipped and the failure logged; it is never run unscoped.
//
// The hook runs synchronously in the request goroutine. A returned error is
// logged and, under RLS, rolls back the hook's own transaction; it cannot affect
// the committed operation or the HTTP response. A hook that must not fail
// should write its own outbox record from WithAudit instead.
//
// Example:
//
//	router.RegisterRoutes[Order](b, "/orders",
//	    router.AllScoped("user"),
//	    router.WithAfterCommit(func(ac metadata.AfterCommitContext[Order]) error {
//	        if ac.Operation == metadata.OpUpdate && ac.Old.Status != ac.New.Status {
//	            return workflows.Start(ac.Ctx, "order-status-changed", ac.New.ID)
//	        }
//	        return nil
//	    }),
//	)
func WithAfterCommit[T any](fn metadata.AfterCommitFunc[T]) AfterCommitConfig[T] {
	return AfterCommitConfig[T]{Fn: fn}
}
