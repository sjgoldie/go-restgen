package router

import (
	"context"
	"fmt"
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
	router     chi.Router             // Chi router being configured
	parentMeta *metadata.TypeMetadata // Metadata of the immediate parent (nil for root)
}

// NewBuilder creates a new Builder for registering routes
func NewBuilder(r chi.Router) *Builder {
	return &Builder{
		router:     r,
		parentMeta: nil,
	}
}

// RegisterRoutes registers CRUD routes for a resource type T
// Accepts optional auth configs, query configs, validator, auditor, and nested function for child routes
// Configs can be mixed with nested function in any order
func RegisterRoutes[T any](b *Builder, path string, options ...interface{}) {
	// Separate auth configs, query configs, validator, auditor, and nested function
	var authConfigs []AuthConfig
	var queryConfigs []QueryConfig
	var validator metadata.ValidatorFunc[T]
	var auditor metadata.AuditFunc[T]
	var nested NestedFunc

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
		case func(*Builder):
			nested = v
		}
	}

	registerRoutesWithBuilder[T](b, path, nested, authConfigs, queryConfigs, validator, auditor)
}

// registerRoutesWithBuilder is the internal implementation
func registerRoutesWithBuilder[T any](b *Builder, path string, nested NestedFunc, authConfigs []AuthConfig, queryConfigs []QueryConfig, validator metadata.ValidatorFunc[T], auditor metadata.AuditFunc[T]) {
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

	// Get chi router from builder
	r := b.router

	// Check if this type has a parent relationship
	var parentType reflect.Type
	if b.parentMeta != nil {
		parentType = b.parentMeta.ModelType
	}
	foreignKeyCol, err := findParentRelationshipFromType(tType, parentType)
	if err != nil {
		panic(fmt.Sprintf("error analyzing type %s: %v", typeName, err))
	}

	// Validate parent relationship
	if b.parentMeta != nil {
		// We're nested, so we must have a parent relationship
		if foreignKeyCol == "" {
			panic(fmt.Sprintf("type %s is registered as nested under %s but has no bun relation field for parent type",
				typeName, b.parentMeta.TypeName))
		}
	} else if foreignKeyCol != "" {
		// We have a parent relationship but we're not nested
		panic(fmt.Sprintf("type %s has a parent relationship (foreign key %s) but is not registered as nested. Use b.RegisterRoutes[%s] inside a parent's nested function",
			typeName, foreignKeyCol, typeName))
	}

	// Create metadata with all configuration
	meta := &metadata.TypeMetadata{
		TypeID:        typeID,
		TypeName:      typeName,
		TableName:     tableName,
		URLParamUUID:  urlParamUUID,
		ModelType:     tType,
		ParentType:    parentType,
		ParentMeta:    b.parentMeta,
		ForeignKeyCol: foreignKeyCol,
	}

	// Merge auth configs (last wins for each method)
	authMap := mergeAuthConfigs(authConfigs)

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
	for _, qc := range queryConfigs {
		if len(qc.FilterableFields) > 0 {
			meta.FilterableFields = qc.FilterableFields
		}
		if len(qc.SortableFields) > 0 {
			meta.SortableFields = qc.SortableFields
		}
		if qc.DefaultSort != "" {
			meta.DefaultSort = qc.DefaultSort
		}
		if qc.DefaultLimit > 0 {
			meta.DefaultLimit = qc.DefaultLimit
		}
		if qc.MaxLimit > 0 {
			meta.MaxLimit = qc.MaxLimit
		}
	}

	// Set validator if provided
	if validator != nil {
		meta.Validator = validator
	}

	// Set auditor if provided
	if auditor != nil {
		meta.Auditor = auditor
	}

	// Create middleware to inject metadata into context
	metadataMiddleware := createMetadataMiddleware(meta)

	// Register routes with UUID parameter names
	r.Route(path, func(r chi.Router) {
		// Add metadata middleware to all routes in this group
		r.Use(metadataMiddleware)

		// List endpoint - GET /resources
		listHandler := http.Handler(handler.GetAll[T]())
		if authConfig := authMap[MethodList]; authConfig != nil {
			listHandler = wrapWithAuth(listHandler, authConfig)
		} else {
			listHandler = blockUnauthorized(listHandler)
		}
		r.Method("GET", "/", listHandler)

		// Create endpoint - POST /resources
		createHandler := http.Handler(handler.Create[T]())
		if authConfig := authMap[MethodPost]; authConfig != nil {
			createHandler = wrapWithAuth(createHandler, authConfig)
		} else {
			createHandler = blockUnauthorized(createHandler)
		}
		r.Method("POST", "/", createHandler)

		// Register item routes and nested routes under /{id}
		r.Route("/{"+urlParamUUID+"}", func(r chi.Router) {
			// If there are nested routes, add middleware first (before defining routes)
			if nested != nil {
				// Add middleware to extract parent ID and store in context
				r.Use(createParentIDMiddleware(urlParamUUID))
			}

			// Get endpoint - GET /resources/{id}
			getHandler := http.Handler(handler.Get[T]())
			if authConfig := authMap[MethodGet]; authConfig != nil {
				getHandler = wrapWithAuth(getHandler, authConfig)
			} else {
				getHandler = blockUnauthorized(getHandler)
			}
			r.Method("GET", "/", getHandler)

			// Update endpoint - PUT /resources/{id}
			updateHandler := http.Handler(handler.Update[T]())
			if authConfig := authMap[MethodPut]; authConfig != nil {
				updateHandler = wrapWithAuth(updateHandler, authConfig)
			} else {
				updateHandler = blockUnauthorized(updateHandler)
			}
			r.Method("PUT", "/", updateHandler)

			// Delete endpoint - DELETE /resources/{id}
			deleteHandler := http.Handler(handler.Delete[T]())
			if authConfig := authMap[MethodDelete]; authConfig != nil {
				deleteHandler = wrapWithAuth(deleteHandler, authConfig)
			} else {
				deleteHandler = blockUnauthorized(deleteHandler)
			}
			r.Method("DELETE", "/", deleteHandler)

			// Register nested routes
			if nested != nil {
				// Create a new builder with this type's metadata as parent
				childBuilder := &Builder{
					router:     r,
					parentMeta: meta, // Pass current metadata as parent
				}

				nested(childBuilder)
			}
		})
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
			parentIDs, ok := ctx.Value("parentIDs").(map[string]string)
			if !ok || parentIDs == nil {
				parentIDs = make(map[string]string)
			}

			// Add this parent ID to the map, keyed by UUID
			parentIDs[paramUUID] = parentID

			// Store updated map in context
			ctx = context.WithValue(ctx, "parentIDs", parentIDs) //nolint:staticcheck // Framework-internal context key
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// findParentRelationshipFromType looks for a field with the parent type and extracts the foreign key
// from the bun relation tag. Returns the foreign key column name.
func findParentRelationshipFromType(childType reflect.Type, parentType reflect.Type) (foreignKeyCol string, err error) {
	// If no parent type specified, this is a root resource
	if parentType == nil {
		return "", nil
	}

	// Look for a field with type matching parent type
	for i := 0; i < childType.NumField(); i++ {
		field := childType.Field(i)

		// Check if field type matches parent type (handle pointer types)
		fieldType := field.Type
		if fieldType.Kind() == reflect.Ptr {
			fieldType = fieldType.Elem()
		}

		// Check if this field's type matches the parent type
		if fieldType == parentType {
			// Found the parent field, now extract foreign key from bun relation tag
			bunTag := field.Tag.Get("bun")
			if bunTag == "" {
				return "", fmt.Errorf("field %s of type %s has no bun tag", field.Name, parentType.Name())
			}

			// Parse the relation tag to extract foreign key
			// Expected format: "rel:belongs-to,join:user_id=id"
			foreignKey := parseForeignKeyFromRelation(bunTag)
			if foreignKey == "" {
				return "", fmt.Errorf("field %s has bun tag but no valid rel:belongs-to with join clause: %s", field.Name, bunTag)
			}

			return foreignKey, nil
		}
	}

	// No field with parent type found
	return "", fmt.Errorf("no field with parent type %s found", parentType.Name())
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

// parseForeignKeyFromRelation extracts the foreign key column name from a bun relation tag
// Expected format: "rel:belongs-to,join:user_id=id" -> returns "user_id"
func parseForeignKeyFromRelation(bunTag string) string {
	parts := strings.Split(bunTag, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)

		// Look for join clause
		if strings.HasPrefix(part, "join:") {
			joinClause := strings.TrimPrefix(part, "join:")
			// Parse "user_id=id" to get "user_id"
			if idx := strings.Index(joinClause, "="); idx != -1 {
				return strings.TrimSpace(joinClause[:idx])
			}
		}
	}

	return ""
}
