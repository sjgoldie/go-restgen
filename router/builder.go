package router

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/sjgoldie/go-restgen/handler"
	"github.com/sjgoldie/go-restgen/metadata"
)

// NestedFunc is a function that registers nested routes using a Builder
type NestedFunc func(b *Builder)

// Builder provides context for registering nested routes and manages the chi router
type Builder struct {
	router         chi.Router             // Chi router being configured
	parentMeta     *metadata.TypeMetadata // Metadata of the immediate parent (nil for root)
	parentAuthGet  *AuthConfig            // Parent's auth config for GET (for populating ChildAuth)
	parentAuthList *AuthConfig            // Parent's auth config for LIST (for populating ChildAuth)
}

// customHandlers holds optional custom handler functions for a route registration
type customHandlers[T any] struct {
	get    handler.CustomGetFunc[T]
	getAll handler.CustomGetAllFunc[T]
	create handler.CustomCreateFunc[T]
	update handler.CustomUpdateFunc[T]
	delete handler.CustomDeleteFunc[T]
}

// batchHandlers holds optional custom batch handler functions
type batchHandlers[T any] struct {
	create handler.CustomBatchCreateFunc[T]
	update handler.CustomBatchUpdateFunc[T]
	delete handler.CustomBatchDeleteFunc[T]
}

// actionEntry holds a single action configuration with its auth
type actionEntry[T any] struct {
	name string
	fn   handler.ActionFunc[T]
	auth AuthConfig
}

// FileResourceConfig marks a route as a file resource
type FileResourceConfig struct{}

// metadataSetup holds the prepared metadata and auth configuration for route registration
type metadataSetup struct {
	meta               *metadata.TypeMetadata
	authMap            map[string]*AuthConfig
	metadataMiddleware func(http.Handler) http.Handler
}

// NewBuilder creates a new Builder for registering routes
func NewBuilder(r chi.Router) *Builder {
	return &Builder{
		router:     r,
		parentMeta: nil,
	}
}

// AsFileResource marks this route as a file resource.
// File resources use multipart form uploads instead of JSON.
// Requires file storage to be initialized via filestore.Initialize().
func AsFileResource() FileResourceConfig {
	return FileResourceConfig{}
}

// RegisterRoutes registers CRUD routes for a resource type T
// Accepts optional auth configs, query configs, validator, auditor, custom handlers, actions, and nested function for child routes
// Configs can be mixed with nested function in any order
func RegisterRoutes[T any](b *Builder, path string, options ...interface{}) {
	// Separate auth configs, query configs, validator, auditor, custom handlers, actions, relation config, and nested function
	var authConfigs []AuthConfig
	var queryConfigs []QueryConfig
	var validator metadata.ValidatorFunc[T]
	var auditor metadata.AuditFunc[T]
	var custom customHandlers[T]
	var batch batchHandlers[T]
	var batchLimit int
	var actions []actionEntry[T]
	var relationName string
	var nested NestedFunc
	var singleRoute *SingleRouteConfig
	var isFileResource bool
	var pkField string

	for _, opt := range options {
		switch v := opt.(type) {
		case AuthConfig:
			authConfigs = append(authConfigs, v)
		case QueryConfig:
			queryConfigs = append(queryConfigs, v)
		case ValidatorConfig[T]:
			validator = v.Fn
		case AuditConfig[T]:
			auditor = v.Fn
		case CustomGetConfig[T]:
			custom.get = v.Fn
		case CustomGetAllConfig[T]:
			custom.getAll = v.Fn
		case CustomCreateConfig[T]:
			custom.create = v.Fn
		case CustomUpdateConfig[T]:
			custom.update = v.Fn
		case CustomDeleteConfig[T]:
			custom.delete = v.Fn
		case BatchLimitConfig:
			batchLimit = v.Limit
		case CustomBatchCreateConfig[T]:
			batch.create = v.Fn
		case CustomBatchUpdateConfig[T]:
			batch.update = v.Fn
		case CustomBatchDeleteConfig[T]:
			batch.delete = v.Fn
		case ActionConfig[T]:
			actions = append(actions, actionEntry[T]{name: v.Name, fn: v.Fn, auth: v.Auth})
		case RelationConfig:
			relationName = v.Name
		case SingleRouteConfig:
			singleRoute = &v
		case FileResourceConfig:
			isFileResource = true
		case PKFieldConfig:
			pkField = v.FieldName
		case func(*Builder):
			nested = v
		}
	}

	registerRoutesWithBuilder[T](b, path, nested, authConfigs, queryConfigs, validator, auditor, custom, batch, batchLimit, actions, relationName, singleRoute, isFileResource, pkField)
}

// prepareMetadata assembles type metadata and auth configuration before route registration.
// This extracts the setup phase from registerRoutesWithBuilder to reduce cyclomatic complexity.
func prepareMetadata[T any](b *Builder, path string, authConfigs []AuthConfig, queryConfigs []QueryConfig, validator metadata.ValidatorFunc[T], auditor metadata.AuditFunc[T], batchLimit int, relationName string, isFileResource bool, pkField string) (string, *metadataSetup) {
	// Ensure path starts with /
	if len(path) > 0 && path[0] != '/' {
		path = "/" + path
	}

	// Get type information
	var t T
	tType := reflect.TypeOf(t)
	if tType.Kind() == reflect.Ptr {
		tType = tType.Elem()
	}
	typeName := tType.Name()

	// Generate unique type ID
	typeID := metadata.GenerateTypeID()

	// Generate unique UUID for URL parameter
	// This avoids chi routing conflicts with nested routes
	urlParamUUID := metadata.GenerateTypeID()

	// Get table name from bun tag
	tableName := getTableName(tType)

	// Check if this type has a parent relationship
	var parentType reflect.Type
	if b.parentMeta != nil {
		parentType = b.parentMeta.ModelType
	}
	foreignKeyCol, err := findParentRelationshipFromType(tType, parentType)
	if err != nil {
		slog.Warn("could not find parent relationship",
			"type", typeName,
			"error", err)
	}

	// Validate parent relationship
	validateParentRelationship(b.parentMeta, foreignKeyCol, typeName)

	// Determine PK field name - default to "ID" convention
	if pkField == "" {
		pkField = "ID"
	}

	// Create metadata with all configuration
	meta := &metadata.TypeMetadata{
		TypeID:         typeID,
		TypeName:       typeName,
		TableName:      tableName,
		URLParamUUID:   urlParamUUID,
		PKField:        pkField,
		ModelType:      tType,
		ParentType:     parentType,
		ParentMeta:     b.parentMeta,
		ForeignKeyCol:  foreignKeyCol,
		ChildMeta:      make(map[string]*metadata.TypeMetadata),
		IsFileResource: isFileResource,
	}

	// Register this type as a child of the parent (for ?include= support)
	// Only register if relationName is explicitly provided via WithRelationName
	if b.parentMeta != nil && relationName != "" {
		b.parentMeta.ChildMeta[relationName] = meta
	}

	// Merge auth configs (last wins for each method)
	authMap := mergeAuthConfigs(authConfigs)

	// Register child's auth config with parent's auth configs (for ?include= authorization)
	registerChildAuthConfig(b, relationName, authMap)

	// Extract ownership configuration from auth configs (last one wins)
	var ownershipFields []string
	var bypassScopes []string
	for _, config := range authConfigs {
		if config.Ownership != nil {
			ownershipFields = config.Ownership.Fields
			bypassScopes = config.Ownership.BypassScopes
		}
	}

	// Only set ownership if configured
	if len(ownershipFields) > 0 {
		meta.OwnershipFields = ownershipFields
		meta.BypassScopes = bypassScopes
	}

	// Merge query configs (last wins for each setting)
	meta = mergeQueryConfigs(meta, queryConfigs)

	// Set validator if provided
	if validator != nil {
		meta.Validator = validator
	}

	// Set auditor if provided
	if auditor != nil {
		meta.Auditor = auditor
	}

	// Set batch limit if provided
	if batchLimit > 0 {
		meta.BatchLimit = batchLimit
	}

	// Create middleware to inject metadata into context
	metadataMiddleware := createMetadataMiddleware(meta)

	return path, &metadataSetup{
		meta:               meta,
		authMap:            authMap,
		metadataMiddleware: metadataMiddleware,
	}
}

// registerRoutesWithBuilder is the internal implementation
func registerRoutesWithBuilder[T any](b *Builder, path string, nested NestedFunc, authConfigs []AuthConfig, queryConfigs []QueryConfig, validator metadata.ValidatorFunc[T], auditor metadata.AuditFunc[T], custom customHandlers[T], batch batchHandlers[T], batchLimit int, actions []actionEntry[T], relationName string, singleRoute *SingleRouteConfig, isFileResource bool, pkField string) {
	path, setup := prepareMetadata[T](b, path, authConfigs, queryConfigs, validator, auditor, batchLimit, relationName, isFileResource, pkField)
	meta := setup.meta
	authMap := setup.authMap
	metadataMiddleware := setup.metadataMiddleware
	r := b.router

	// Register routes
	r.Route(path, func(r chi.Router) {
		r.Use(metadataMiddleware)

		var nestedRouter chi.Router

		if singleRoute != nil {
			// Single route registration (for belongs-to relations like /posts/{id}/author)
			// Use parent's URL param UUID so handler gets parent ID, or empty for root-level
			meta.URLParamUUID = ""
			if b.parentMeta != nil {
				meta.URLParamUUID = b.parentMeta.URLParamUUID
			}
			meta.RelationName = relationName
			meta.ParentFKField = singleRoute.ParentFKField

			// GET endpoint - returns the single related object
			getFunc := custom.get
			if getFunc == nil {
				getFunc = handler.StandardGetByParentRelation[T]
			}
			r.Method("GET", "/", wrapHandler(handler.Get[T](getFunc), authMap[MethodGet]))

			// PUT endpoint - updates the single related object (optional)
			if singleRoute.WithPut {
				updateFunc := custom.update
				if updateFunc == nil {
					updateFunc = handler.StandardUpdateByParentRelation[T]
				}
				r.Method("PUT", "/", wrapHandler(handler.Update[T](updateFunc), authMap[MethodPut]))
			}

			nestedRouter = r
		} else {
			// Standard CRUD routes

			// List endpoint - GET /resources
			getAllFunc := custom.getAll
			if getAllFunc == nil {
				getAllFunc = handler.StandardGetAll[T]
			}
			r.Method("GET", "/", wrapHandler(handler.GetAll[T](getAllFunc), authMap[MethodList]))

			// Create endpoint - POST /resources
			createFunc := custom.create
			if createFunc == nil {
				createFunc = handler.StandardCreate[T]
			}
			r.Method("POST", "/", wrapHandler(handler.Create[T](createFunc), authMap[MethodPost]))

			// Batch endpoints - /resources/batch
			// Only register if batch methods have auth configured
			if hasBatchMethods(authMap) {
				r.Route("/batch", func(r chi.Router) {
					// Batch create - POST /resources/batch
					if authMap[MethodBatchCreate] != nil {
						batchCreateFunc := batch.create
						if batchCreateFunc == nil {
							batchCreateFunc = handler.StandardBatchCreate[T]
						}
						r.Method("POST", "/", wrapHandler(handler.BatchCreate[T](batchCreateFunc), authMap[MethodBatchCreate]))
					}

					// Batch update - PUT /resources/batch
					if authMap[MethodBatchUpdate] != nil {
						batchUpdateFunc := batch.update
						if batchUpdateFunc == nil {
							batchUpdateFunc = handler.StandardBatchUpdate[T]
						}
						r.Method("PUT", "/", wrapHandler(handler.BatchUpdate[T](batchUpdateFunc), authMap[MethodBatchUpdate]))
					}

					// Batch delete - DELETE /resources/batch
					if authMap[MethodBatchDelete] != nil {
						batchDeleteFunc := batch.delete
						if batchDeleteFunc == nil {
							batchDeleteFunc = handler.StandardBatchDelete[T]
						}
						r.Method("DELETE", "/", wrapHandler(handler.BatchDelete[T](batchDeleteFunc), authMap[MethodBatchDelete]))
					}
				})
			}

			// Register item routes under /{id}
			r.Route("/{"+meta.URLParamUUID+"}", func(r chi.Router) {
				// If there are nested routes, add middleware first
				if nested != nil {
					r.Use(createParentIDMiddleware(meta.URLParamUUID))
				}

				// Get endpoint - GET /resources/{id}
				getFunc := custom.get
				if getFunc == nil {
					getFunc = handler.StandardGet[T]
				}
				r.Method("GET", "/", wrapHandler(handler.Get[T](getFunc), authMap[MethodGet]))

				// Update endpoint - PUT /resources/{id}
				// File resources don't support update (you delete and re-upload)
				if !isFileResource {
					updateFunc := custom.update
					if updateFunc == nil {
						updateFunc = handler.StandardUpdate[T]
					}
					r.Method("PUT", "/", wrapHandler(handler.Update[T](updateFunc), authMap[MethodPut]))
				}

				// Delete endpoint - DELETE /resources/{id}
				deleteFunc := custom.delete
				if deleteFunc == nil {
					deleteFunc = handler.StandardDelete[T]
				}
				r.Method("DELETE", "/", wrapHandler(handler.Delete[T](deleteFunc), authMap[MethodDelete]))

				// Download endpoint - GET /resources/{id}/download (file resources)
				// For proxy mode: streams the file
				// For signed URL mode: redirects to signed URL
				if isFileResource {
					r.Method("GET", "/download", wrapHandler(handler.Download[T](), authMap[MethodGet]))
				}

				// Register action endpoints - POST /resources/{id}/{action-name}
				for _, action := range actions {
					r.Method("POST", "/"+action.name, wrapHandler(handler.Action[T](action.fn), &action.auth))
				}

				nestedRouter = r
			})
		}

		// Register nested routes (shared for both single and standard routes)
		if nested != nil && nestedRouter != nil {
			childBuilder := &Builder{
				router:         nestedRouter,
				parentMeta:     meta,
				parentAuthGet:  authMap[MethodGet],
				parentAuthList: authMap[MethodList],
			}
			nested(childBuilder)
		}
	})
}

// createMetadataMiddleware creates middleware that injects TypeMetadata into context
func createMetadataMiddleware(meta *metadata.TypeMetadata) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), metadata.MetadataKey, meta)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// wrapHandler wraps a handler with auth middleware if configured, otherwise blocks unauthorized access
func wrapHandler(h http.Handler, authConfig *AuthConfig) http.Handler {
	if authConfig != nil {
		return wrapWithAuth(h, authConfig)
	}
	return blockUnauthorized(h)
}

// createParentIDMiddleware creates middleware that extracts a parent ID from URL
// and adds it to the context for child queries
// paramUUID is the UUID used in the URL parameter name
// Parent IDs are stored as strings to support both integer and UUID primary keys
func createParentIDMiddleware(paramUUID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract parent ID from URL using the UUID parameter name
			// Keep as string to support both int and UUID PKs
			parentID := chi.URLParam(r, paramUUID)
			if parentID == "" {
				http.Error(w, "missing parent ID", http.StatusBadRequest)
				return
			}

			// Get existing parent IDs from context or create new map
			ctx := r.Context()
			parentIDs, ok := ctx.Value(metadata.ParentIDsKey).(map[string]string)
			if !ok || parentIDs == nil {
				parentIDs = make(map[string]string)
			}

			// Add this parent ID to the map, keyed by UUID
			parentIDs[paramUUID] = parentID

			// Store updated map in context
			ctx = context.WithValue(ctx, metadata.ParentIDsKey, parentIDs)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// findParentRelationshipFromType looks for a belongs-to relationship between child and parent types
// and extracts the foreign key from the bun relation tag. Returns the foreign key column name.
// Handles two cases:
// 1. Child has belongs-to Parent (e.g., Comment belongs-to Post) - FK is on child
// 2. Parent has belongs-to Child (e.g., Post belongs-to User/Author) - FK is on parent
func findParentRelationshipFromType(childType reflect.Type, parentType reflect.Type) (foreignKeyCol string, err error) {
	if parentType == nil {
		return "", nil
	}
	// Case 1: child belongs-to parent
	if fk := parseForeignKeyFromRelation(childType, parentType); fk != "" {
		return fk, nil
	}
	// Case 2: parent belongs-to child (inverted)
	if fk := parseForeignKeyFromRelation(parentType, childType); fk != "" {
		return fk, nil
	}
	return "", fmt.Errorf("no relationship between %s and %s found", childType.Name(), parentType.Name())
}

// parseForeignKeyFromRelation looks for a belongs-to field on sourceType pointing to targetType
// Returns the FK column name from the bun tag, or empty string if not found
func parseForeignKeyFromRelation(sourceType, targetType reflect.Type) string {
	for i := 0; i < sourceType.NumField(); i++ {
		field := sourceType.Field(i)

		fieldType := field.Type
		if fieldType.Kind() == reflect.Ptr {
			fieldType = fieldType.Elem()
		}

		if fieldType == targetType {
			bunTag := field.Tag.Get("bun")
			if bunTag == "" {
				continue
			}
			// Parse bun tag for join clause: "rel:belongs-to,join:post_id=id"
			for _, part := range strings.Split(bunTag, ",") {
				part = strings.TrimSpace(part)
				if strings.HasPrefix(part, "join:") {
					joinClause := strings.TrimPrefix(part, "join:")
					if idx := strings.Index(joinClause, "="); idx != -1 {
						return strings.TrimSpace(joinClause[:idx])
					}
				}
			}
		}
	}
	return ""
}

// getTableName extracts table name from bun tag on the struct
func getTableName(tType reflect.Type) string {
	// Look for bun.BaseModel field
	for i := 0; i < tType.NumField(); i++ {
		field := tType.Field(i)
		bunTag := field.Tag.Get("bun")
		if strings.HasPrefix(bunTag, "table:") {
			parts := strings.Split(bunTag, ",")
			for _, part := range parts {
				if strings.HasPrefix(part, "table:") {
					return strings.TrimPrefix(part, "table:")
				}
			}
		}
	}
	// Fallback: pluralize type name and lowercase
	return strings.ToLower(tType.Name()) + "s"
}

// mergeQueryConfigs applies query configurations to metadata and returns a new copy.
// Last config wins for each setting.
func mergeQueryConfigs(meta *metadata.TypeMetadata, queryConfigs []QueryConfig) *metadata.TypeMetadata {
	result := meta.Clone()

	for _, qc := range queryConfigs {
		if len(qc.FilterableFields) > 0 {
			result.FilterableFields = qc.FilterableFields
		}
		if len(qc.SortableFields) > 0 {
			result.SortableFields = qc.SortableFields
		}
		if qc.DefaultSort != "" {
			result.DefaultSort = qc.DefaultSort
		}
		if qc.DefaultLimit > 0 {
			result.DefaultLimit = qc.DefaultLimit
		}
		if qc.MaxLimit > 0 {
			result.MaxLimit = qc.MaxLimit
		}
	}

	return result
}

// validateParentRelationship logs warnings for mismatched parent/child relationships
func validateParentRelationship(parentMeta *metadata.TypeMetadata, foreignKeyCol, typeName string) {
	if parentMeta != nil {
		// We're nested but have no parent relationship - could be belongs-to registered for ?include=
		if foreignKeyCol == "" {
			slog.Warn("type registered as nested but has no foreign key to parent - CRUD operations on this route may not work correctly",
				"type", typeName,
				"parent", parentMeta.TypeName)
		}
	} else if foreignKeyCol != "" {
		// We have a parent relationship but we're not nested
		slog.Warn("type has parent relationship but is not registered as nested",
			"type", typeName,
			"foreignKey", foreignKeyCol)
	}
}

// registerChildAuthConfig registers the child's auth config with parent's auth configs
// for ?include= authorization. Only applies when relationName is provided.
func registerChildAuthConfig(b *Builder, relationName string, authMap map[string]*AuthConfig) {
	if relationName == "" {
		return
	}

	childAuthGet := authMap[MethodGet]
	if childAuthGet == nil {
		return
	}

	if b.parentAuthGet != nil {
		if b.parentAuthGet.ChildAuth == nil {
			b.parentAuthGet.ChildAuth = make(map[string]*AuthConfig)
		}
		b.parentAuthGet.ChildAuth[relationName] = childAuthGet
	}

	if b.parentAuthList != nil {
		if b.parentAuthList.ChildAuth == nil {
			b.parentAuthList.ChildAuth = make(map[string]*AuthConfig)
		}
		b.parentAuthList.ChildAuth[relationName] = childAuthGet
	}
}
