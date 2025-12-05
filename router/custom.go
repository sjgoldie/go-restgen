package router

import "github.com/sjgoldie/go-restgen/handler"

// CustomGetConfig holds custom Get handler configuration for route registration.
// This wraps the generic custom function for passing through the options interface{}.
type CustomGetConfig[T any] struct {
	Fn handler.CustomGetFunc[T]
}

// WithCustomGet creates a CustomGetConfig for use in RegisterRoutes.
// The custom function replaces the standard svc.Get call while all other
// handler logic (parsing, auth, error handling, response) remains the same.
//
// Example:
//
//	router.RegisterRoutes[User](b, "/me",
//	    router.IsAuthenticated(),
//	    router.WithCustomGet(func(ctx context.Context, svc *service.Service[User], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, relations []string) (*User, error) {
//	        return svc.Get(ctx, auth.UserID, relations)
//	    }),
//	)
func WithCustomGet[T any](fn handler.CustomGetFunc[T]) CustomGetConfig[T] {
	return CustomGetConfig[T]{Fn: fn}
}

// CustomGetAllConfig holds custom GetAll handler configuration for route registration.
type CustomGetAllConfig[T any] struct {
	Fn handler.CustomGetAllFunc[T]
}

// WithCustomGetAll creates a CustomGetAllConfig for use in RegisterRoutes.
// The custom function replaces the standard svc.GetAll call.
func WithCustomGetAll[T any](fn handler.CustomGetAllFunc[T]) CustomGetAllConfig[T] {
	return CustomGetAllConfig[T]{Fn: fn}
}

// CustomCreateConfig holds custom Create handler configuration for route registration.
type CustomCreateConfig[T any] struct {
	Fn handler.CustomCreateFunc[T]
}

// WithCustomCreate creates a CustomCreateConfig for use in RegisterRoutes.
// The custom function replaces the standard svc.Create call.
func WithCustomCreate[T any](fn handler.CustomCreateFunc[T]) CustomCreateConfig[T] {
	return CustomCreateConfig[T]{Fn: fn}
}

// CustomUpdateConfig holds custom Update handler configuration for route registration.
type CustomUpdateConfig[T any] struct {
	Fn handler.CustomUpdateFunc[T]
}

// WithCustomUpdate creates a CustomUpdateConfig for use in RegisterRoutes.
// The custom function replaces the standard svc.Update call.
func WithCustomUpdate[T any](fn handler.CustomUpdateFunc[T]) CustomUpdateConfig[T] {
	return CustomUpdateConfig[T]{Fn: fn}
}

// CustomDeleteConfig holds custom Delete handler configuration for route registration.
type CustomDeleteConfig[T any] struct {
	Fn handler.CustomDeleteFunc[T]
}

// WithCustomDelete creates a CustomDeleteConfig for use in RegisterRoutes.
// The custom function replaces the standard svc.Delete call.
func WithCustomDelete[T any](fn handler.CustomDeleteFunc[T]) CustomDeleteConfig[T] {
	return CustomDeleteConfig[T]{Fn: fn}
}
