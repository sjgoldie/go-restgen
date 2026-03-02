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

// ActionConfig holds action endpoint configuration for route registration.
// Actions are custom operations on a single resource (e.g., POST /orders/{id}/cancel).
type ActionConfig[T any] struct {
	Name string
	Fn   handler.ActionFunc[T]
	Auth AuthConfig
}

// WithAction creates an ActionConfig for use in RegisterRoutes.
// Actions are registered as POST /resource/{id}/{name} endpoints.
// The action handler receives the pre-fetched item and raw request body.
//
// Example:
//
//	router.RegisterRoutes[Order](b, "/orders",
//	    router.AllPublic(),
//	    router.WithAction("cancel", cancelHandler, router.AuthConfig{
//	        Scopes: []string{"user"},
//	    }),
//	)
func WithAction[T any](name string, fn handler.ActionFunc[T], auth AuthConfig) ActionConfig[T] {
	return ActionConfig[T]{Name: name, Fn: fn, Auth: auth}
}

// BatchLimitConfig holds the batch limit configuration for route registration.
type BatchLimitConfig struct {
	Limit int
}

// WithBatchLimit sets the maximum number of items allowed in batch operations.
// If not set, there is no limit on batch size.
// Batch endpoints are only created if batch methods have auth configured.
//
// Example:
//
//	router.RegisterRoutes[Post](b, "/posts",
//	    router.AllScopedWithBatch("admin"),
//	    router.WithBatchLimit(100),
//	)
func WithBatchLimit(limit int) BatchLimitConfig {
	return BatchLimitConfig{Limit: limit}
}

// CustomBatchCreateConfig holds custom batch create handler configuration.
type CustomBatchCreateConfig[T any] struct {
	Fn handler.CustomBatchCreateFunc[T]
}

// WithCustomBatchCreate creates a CustomBatchCreateConfig for use in RegisterRoutes.
func WithCustomBatchCreate[T any](fn handler.CustomBatchCreateFunc[T]) CustomBatchCreateConfig[T] {
	return CustomBatchCreateConfig[T]{Fn: fn}
}

// CustomBatchUpdateConfig holds custom batch update handler configuration.
type CustomBatchUpdateConfig[T any] struct {
	Fn handler.CustomBatchUpdateFunc[T]
}

// WithCustomBatchUpdate creates a CustomBatchUpdateConfig for use in RegisterRoutes.
func WithCustomBatchUpdate[T any](fn handler.CustomBatchUpdateFunc[T]) CustomBatchUpdateConfig[T] {
	return CustomBatchUpdateConfig[T]{Fn: fn}
}

// CustomBatchDeleteConfig holds custom batch delete handler configuration.
type CustomBatchDeleteConfig[T any] struct {
	Fn handler.CustomBatchDeleteFunc[T]
}

// WithCustomBatchDelete creates a CustomBatchDeleteConfig for use in RegisterRoutes.
func WithCustomBatchDelete[T any](fn handler.CustomBatchDeleteFunc[T]) CustomBatchDeleteConfig[T] {
	return CustomBatchDeleteConfig[T]{Fn: fn}
}

// PKFieldConfig holds custom primary key field configuration.
type PKFieldConfig struct {
	FieldName string
}

// WithAlternatePK overrides the default primary key field name.
// By convention, go-restgen assumes the primary key field is named "ID".
// Use this option when your model uses a different field name for the primary key.
//
// Example:
//
//	type MyModel struct {
//	    bun.BaseModel `bun:"table:my_models"`
//	    MyPK          int `bun:"my_pk,pk,autoincrement" json:"my_pk"`
//	    Name          string `bun:"name" json:"name"`
//	}
//
//	router.RegisterRoutes[MyModel](b, "/models",
//	    router.AllPublic(),
//	    router.WithAlternatePK("MyPK"),
//	)
func WithAlternatePK(fieldName string) PKFieldConfig {
	return PKFieldConfig{FieldName: fieldName}
}

// FuncConfig holds anything func endpoint configuration for route registration.
// Anything funcs are like actions but support any HTTP method and any return type.
// Registered as METHOD /resource/{id}/{name} endpoints.
type FuncConfig[T any] struct {
	Method string
	Name   string
	Fn     handler.FuncHandler[T]
	Auth   AuthConfig
}

// WithFunc creates a FuncConfig for use in RegisterRoutes.
// The func handler receives the pre-fetched item and raw request body,
// and can return any type with an explicit HTTP status code.
//
// Example:
//
//	router.RegisterRoutes[Organisation](b, "/organisations",
//	    router.AllScoped("admin"),
//	    router.WithFunc("GET", "wf-status", getWorkflowStatus, router.AllScoped("admin")),
//	)
func WithFunc[T any](method, name string, fn handler.FuncHandler[T], auth AuthConfig) FuncConfig[T] {
	return FuncConfig[T]{Method: method, Name: name, Fn: fn, Auth: auth}
}

// SSEConfig holds SSE endpoint configuration for route registration.
// SSE endpoints are always GET and stream events to the client.
type SSEConfig[T any] struct {
	Name string
	Fn   handler.SSEFunc[T]
	Auth AuthConfig
}

// WithSSE creates an SSEConfig for use in RegisterRoutes.
// SSE endpoints are registered as GET /resource/{id}/{name}.
// The handler receives the pre-fetched item and writes events to a channel.
//
// Example:
//
//	router.RegisterRoutes[Organisation](b, "/organisations",
//	    router.AllScoped("admin"),
//	    router.WithSSE("status-stream", streamOrgStatus, router.AllScoped("admin")),
//	)
func WithSSE[T any](name string, fn handler.SSEFunc[T], auth AuthConfig) SSEConfig[T] {
	return SSEConfig[T]{Name: name, Fn: fn, Auth: auth}
}
