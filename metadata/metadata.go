package metadata

import (
	"context"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	apperrors "github.com/sjgoldie/go-restgen/errors"

	"github.com/google/uuid"
)

// Operation represents the type of mutation being performed
type Operation string

const (
	OpCreate Operation = "create"
	OpUpdate Operation = "update"
	OpDelete Operation = "delete"
)

// ValidationContext provides context for validation functions
// For Create: Old is nil, New contains the item to be created
// For Update: Old contains the existing item, New contains the updated item
// For Delete: Old contains the item to be deleted, New is nil
type ValidationContext[T any] struct {
	Operation Operation
	New       *T              // The incoming item (nil for delete)
	Old       *T              // The existing item (nil for create)
	Ctx       context.Context // Contains AuthInfo, parentIDs, etc.
}

// ValidatorFunc is a function that validates a mutation operation
// Return nil to allow the operation, or an error to reject it
// The error message will be returned to the client as a 400 Bad Request
type ValidatorFunc[T any] func(ValidationContext[T]) error

// AuditContext provides context for audit functions
// For Create: Old is nil, New contains the created item (with ID populated)
// For Update: Old contains the previous state, New contains the updated item
// For Delete: Old contains the deleted item, New is nil
type AuditContext[T any] struct {
	Operation Operation
	New       *T              // The item after operation (nil for delete)
	Old       *T              // The item before operation (nil for create)
	Ctx       context.Context // Contains AuthInfo, parentIDs, etc.
}

// AuditFunc is a function that creates an audit record for a mutation operation
// Return nil to skip audit for this operation
// The returned audit record (any bun model) will be inserted in the same transaction
type AuditFunc[T any] func(AuditContext[T]) any

// AuthInfo contains authentication and authorization information.
// Developers populate this in their auth middleware and add to context.
type AuthInfo struct {
	UserID string   // External user ID (e.g., Auth0 ID, Firebase UID, JWT sub claim)
	Scopes []string // List of scopes/permissions the user has
}

// authInfoKeyType is the context key type for storing AuthInfo
type authInfoKeyType string

// AuthInfoKey is the context key for storing AuthInfo
const AuthInfoKey authInfoKeyType = "restgen_auth_info"

// parentIDsKeyType is the context key type for storing parent IDs
type parentIDsKeyType string

// ParentIDsKey is the context key for storing parent IDs in nested routes
const ParentIDsKey parentIDsKeyType = "restgen_parent_ids"

// ownershipEnforcedKeyType is the context key type for ownership enforcement flag
type ownershipEnforcedKeyType string

// OwnershipEnforcedKey is the context key for the ownership enforcement flag
const OwnershipEnforcedKey ownershipEnforcedKeyType = "restgen_ownership_enforced"

// ownershipUserIDKeyType is the context key type for ownership user ID
type ownershipUserIDKeyType string

// OwnershipUserIDKey is the context key for the ownership user ID
const OwnershipUserIDKey ownershipUserIDKeyType = "restgen_ownership_user_id"

// ownershipFieldsKeyType is the context key type for ownership fields
type ownershipFieldsKeyType string

// OwnershipFieldsKey is the context key for the ownership fields
const OwnershipFieldsKey ownershipFieldsKeyType = "restgen_ownership_fields"

// parentOwnershipKeyType is the context key type for parent ownership metadata
type parentOwnershipKeyType string

// ParentOwnershipKey is the context key for storing parent metadata that need ownership filtering
// Value is []*TypeMetadata - list of parent types in the chain that require ownership checks
const ParentOwnershipKey parentOwnershipKeyType = "restgen_parent_ownership"

// TypeMetadata contains all metadata for a registered type
type TypeMetadata struct {
	TypeID          string        // Unique UUID for this type
	TypeName        string        // Go type name (e.g., "User")
	TableName       string        // Database table name
	URLParamUUID    string        // UUID used as chi URL parameter name (e.g., "abc-123")
	PKField         string        // Primary key field name (default: "ID", use WithAlternatePK to override)
	ModelType       reflect.Type  // Go type of this model
	ParentType      reflect.Type  // Go type of parent (nil if root)
	ParentMeta      *TypeMetadata // Direct pointer to parent metadata (nil if root)
	ForeignKeyCol   string        // Column in THIS table that references parent (e.g., "user_id")
	OwnershipFields []string      // Model field names for ownership validation (OR logic)
	BypassScopes    []string      // Scopes that bypass ownership validation (e.g., "admin")

	// Child routes for relation loading via ?include=
	ChildMeta map[string]*TypeMetadata // relation name -> child type metadata

	// Single route configuration (for belongs-to relations like /posts/{id}/author)
	RelationName  string // Field name on parent for relation loading (e.g., "Author")
	ParentFKField string // Field name on parent that holds this object's ID (e.g., "AuthorID")

	// Query options for GetAll
	FilterableFields []string // Field names allowed for filtering (empty = no filtering)
	SortableFields   []string // Field names allowed for sorting (empty = no sorting)
	DefaultSort      string   // Default sort field (prefix with - for descending)
	DefaultLimit     int      // Default page size (0 = no limit)
	MaxLimit         int      // Maximum allowed limit (0 = no max)

	// Validation
	Validator any // ValidatorFunc[T] stored as any for type erasure

	// Audit
	Auditor any // AuditFunc[T] stored as any for type erasure

	// File resource
	IsFileResource bool // Whether this type is a file resource (uses multipart upload)

	// Batch operations
	BatchLimit int // Maximum items in batch operations (0 = no limit)
}

// Clone returns a deep copy of the TypeMetadata.
// Slices and maps are fully copied; pointer fields (ParentMeta) reference the same object.
func (m *TypeMetadata) Clone() *TypeMetadata {
	result := &TypeMetadata{
		TypeID:         m.TypeID,
		TypeName:       m.TypeName,
		TableName:      m.TableName,
		URLParamUUID:   m.URLParamUUID,
		PKField:        m.PKField,
		ModelType:      m.ModelType,
		ParentType:     m.ParentType,
		ParentMeta:     m.ParentMeta, // Intentionally shared - parent is not owned by this metadata
		ForeignKeyCol:  m.ForeignKeyCol,
		RelationName:   m.RelationName,
		ParentFKField:  m.ParentFKField,
		DefaultSort:    m.DefaultSort,
		DefaultLimit:   m.DefaultLimit,
		MaxLimit:       m.MaxLimit,
		Validator:      m.Validator,
		Auditor:        m.Auditor,
		IsFileResource: m.IsFileResource,
		BatchLimit:     m.BatchLimit,
	}

	// Deep copy slices
	if len(m.OwnershipFields) > 0 {
		result.OwnershipFields = make([]string, len(m.OwnershipFields))
		copy(result.OwnershipFields, m.OwnershipFields)
	}
	if len(m.BypassScopes) > 0 {
		result.BypassScopes = make([]string, len(m.BypassScopes))
		copy(result.BypassScopes, m.BypassScopes)
	}
	if len(m.FilterableFields) > 0 {
		result.FilterableFields = make([]string, len(m.FilterableFields))
		copy(result.FilterableFields, m.FilterableFields)
	}
	if len(m.SortableFields) > 0 {
		result.SortableFields = make([]string, len(m.SortableFields))
		copy(result.SortableFields, m.SortableFields)
	}

	// Deep copy map
	if m.ChildMeta != nil {
		result.ChildMeta = make(map[string]*TypeMetadata, len(m.ChildMeta))
		for k, v := range m.ChildMeta {
			result.ChildMeta[k] = v // Values are pointers to other metadata, intentionally shared
		}
	}

	return result
}

// QueryOptions holds parsed query parameters for filtering, sorting, and pagination
type QueryOptions struct {
	Filters    map[string]FilterValue // field -> value/operator
	Sort       []SortField            // ordered list of sort fields
	Limit      int                    // 0 means use default
	Offset     int                    // 0 means start from beginning
	CountTotal bool                   // whether to return total count
	Include    []string               // relation names to include via ?include=
}

// AllowedIncludes maps relation names to whether ownership filtering should be applied.
// If a relation name is in the map, the user is authorized to include it.
// The bool value indicates whether to apply ownership filtering:
//   - true:  apply ownership filter (user is authorized but doesn't have bypass scope)
//   - false: skip ownership filter (user has bypass scope for this child type)
type AllowedIncludes map[string]bool

// allowedIncludesKeyType is the context key type for storing AllowedIncludes
type allowedIncludesKeyType string

// AllowedIncludesKey is the context key for storing allowed includes
const AllowedIncludesKey allowedIncludesKeyType = "restgen_allowed_includes"

// AllowedIncludesFromContext retrieves allowed includes from context.
// Returns nil if not present.
func AllowedIncludesFromContext(ctx context.Context) AllowedIncludes {
	includes, _ := ctx.Value(AllowedIncludesKey).(AllowedIncludes)
	return includes
}

// Filter operators
const (
	OpEq   = "eq"   // Equals (default)
	OpNeq  = "neq"  // Not equals
	OpGt   = "gt"   // Greater than
	OpGte  = "gte"  // Greater than or equal
	OpLt   = "lt"   // Less than
	OpLte  = "lte"  // Less than or equal
	OpLike = "like" // SQL LIKE pattern
	OpIn   = "in"   // In list
	OpNin  = "nin"  // Not in list
	OpBt   = "bt"   // Between (inclusive)
	OpNbt  = "nbt"  // Not between
)

// FilterValue represents a filter with value and operator
type FilterValue struct {
	Value    string
	Operator string // One of Op* constants (OpEq is default)
}

// SortField represents a single sort field with direction
type SortField struct {
	Field string
	Desc  bool
}

// queryOptionsKeyType is the context key type for storing QueryOptions
type queryOptionsKeyType string

// QueryOptionsKey is the context key for storing QueryOptions
const QueryOptionsKey queryOptionsKeyType = "restgen_query_options"

// QueryOptionsFromContext retrieves QueryOptions from context
// Returns nil if not present (not an error - query options are optional)
func QueryOptionsFromContext(ctx context.Context) *QueryOptions {
	opts, _ := ctx.Value(QueryOptionsKey).(*QueryOptions)
	return opts
}

// metadataKeyType is the context key type for storing TypeMetadata
type metadataKeyType string

// MetadataKey is the context key for storing TypeMetadata
const MetadataKey metadataKeyType = "restgen_metadata"

// FromContext retrieves TypeMetadata from context
func FromContext(ctx context.Context) (*TypeMetadata, error) {
	meta, ok := ctx.Value(MetadataKey).(*TypeMetadata)
	if !ok || meta == nil {
		return nil, apperrors.ErrMetadataNotFound
	}
	return meta, nil
}

// GenerateTypeID creates a unique ID for a type registration
// Returns a UUID with hyphens replaced by underscores to be compatible with chi URL parameters
func GenerateTypeID() string {
	return strings.ReplaceAll(uuid.New().String(), "-", "_")
}

// ParseQueryOptions extracts filtering, sorting, and pagination options from query parameters.
// This is called by middleware so all handlers have access to parsed query options via context.
func ParseQueryOptions(query url.Values) *QueryOptions {
	opts := &QueryOptions{
		Filters: make(map[string]FilterValue),
	}

	// Parse filters: filter[field]=value or filter[field][op]=value
	for key, values := range query {
		if len(values) == 0 {
			continue
		}
		value := values[0]

		// Check for filter[field] or filter[field][op] pattern
		if strings.HasPrefix(key, "filter[") && strings.HasSuffix(key, "]") {
			// Remove "filter[" prefix and "]" suffix
			inner := key[7 : len(key)-1]

			// Check for nested operator: field][op
			if idx := strings.Index(inner, "]["); idx != -1 {
				field := inner[:idx]
				op := inner[idx+2:]
				opts.Filters[field] = FilterValue{Value: value, Operator: op}
			} else {
				// Simple filter: filter[field]=value (default eq operator)
				opts.Filters[inner] = FilterValue{Value: value, Operator: OpEq}
			}
		}
	}

	// Parse sort: sort=field1,-field2
	if sortStr := query.Get("sort"); sortStr != "" {
		fields := strings.Split(sortStr, ",")
		for _, field := range fields {
			field = strings.TrimSpace(field)
			if field == "" {
				continue
			}
			desc := false
			if strings.HasPrefix(field, "-") {
				desc = true
				field = field[1:]
			}
			opts.Sort = append(opts.Sort, SortField{Field: field, Desc: desc})
		}
	}

	// Parse pagination
	if limitStr := query.Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 {
			opts.Limit = limit
		}
	}

	if offsetStr := query.Get("offset"); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil && offset >= 0 {
			opts.Offset = offset
		}
	}

	// Parse count flag
	if countStr := query.Get("count"); countStr == "true" || countStr == "1" {
		opts.CountTotal = true
	}

	// Parse include: include=Relation1,Relation2
	if includeStr := query.Get("include"); includeStr != "" {
		for _, rel := range strings.Split(includeStr, ",") {
			rel = strings.TrimSpace(rel)
			if rel != "" {
				opts.Include = append(opts.Include, rel)
			}
		}
	}

	return opts
}
