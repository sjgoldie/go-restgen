package router

import "github.com/sjgoldie/go-restgen/metadata"

// ValidatorConfig holds validation configuration for route registration
// This wraps the generic validator function for passing through the options interface{}
type ValidatorConfig[T any] struct {
	Fn metadata.ValidatorFunc[T]
}

// WithValidator creates a ValidatorConfig for use in RegisterRoutes
// The validator function is called before Create, Update, and Delete operations
//
// Example:
//
//	router.RegisterRoutes[Job](b, "/jobs",
//	    router.AllPublic(),
//	    router.WithValidator(func(vc metadata.ValidationContext[Job]) error {
//	        if vc.Operation == metadata.OpCreate && vc.New.Status != "pending" {
//	            return errors.New("new jobs must start in pending status")
//	        }
//	        return nil
//	    }),
//	)
func WithValidator[T any](fn metadata.ValidatorFunc[T]) ValidatorConfig[T] {
	return ValidatorConfig[T]{Fn: fn}
}
