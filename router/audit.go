package router

import "github.com/sjgoldie/go-restgen/metadata"

// AuditConfig holds audit configuration for route registration
// This wraps the generic audit function for passing through the options interface{}
type AuditConfig[T any] struct {
	Fn metadata.AuditFunc[T]
}

// WithAudit creates an AuditConfig for use in RegisterRoutes
// The audit function is called after successful Create, Update, and Delete operations
// within the same database transaction.
//
// Return nil from the audit function to skip audit for a particular operation.
// If the audit insert fails, the entire transaction (including the main operation) is rolled back.
//
// Example:
//
//	router.RegisterRoutes[Job](b, "/jobs",
//	    router.AllPublic(),
//	    router.WithAudit(func(ac metadata.AuditContext[Job]) any {
//	        return &JobAudit{
//	            JobID:     ac.New.ID,
//	            Operation: string(ac.Operation),
//	            OldData:   toJSON(ac.Old),
//	            NewData:   toJSON(ac.New),
//	            UserID:    getUserFromContext(ac.Ctx),
//	            Timestamp: time.Now(),
//	        }
//	    }),
//	)
func WithAudit[T any](fn metadata.AuditFunc[T]) AuditConfig[T] {
	return AuditConfig[T]{Fn: fn}
}
