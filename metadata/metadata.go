package metadata

import (
	"context"
	"reflect"
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

// AuthInfoKey is the context key for storing AuthInfo
const AuthInfoKey = "authInfo"

// TypeMetadata contains all metadata for a registered type
type TypeMetadata struct {
	TypeID          string        // Unique UUID for this type
	TypeName        string        // Go type name (e.g., "User")
	TableName       string        // Database table name
	URLParamUUID    string        // UUID used as chi URL parameter name (e.g., "abc-123")
	ModelType       reflect.Type  // Go type of this model
	ParentType      reflect.Type  // Go type of parent (nil if root)
	ParentMeta      *TypeMetadata // Direct pointer to parent metadata (nil if root)
	ForeignKeyCol   string        // Column in THIS table that references parent (e.g., "user_id")
	OwnershipFields []string      // Model field names for ownership validation (OR logic)
	BypassScopes    []string      // Scopes that bypass ownership validation (e.g., "admin")

	// Child routes for relation loading via ?include=
	ChildMeta map[string]*TypeMetadata // relation name -> child type metadata

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
}

// Clone returns a deep copy of the TypeMetadata.
// Slices and maps are fully copied; pointer fields (ParentMeta) reference the same object.
func (m *TypeMetadata) Clone() *TypeMetadata {
	result := &TypeMetadata{
		TypeID:        m.TypeID,
		TypeName:      m.TypeName,
		TableName:     m.TableName,
		URLParamUUID:  m.URLParamUUID,
		ModelType:     m.ModelType,
		ParentType:    m.ParentType,
		ParentMeta:    m.ParentMeta, // Intentionally shared - parent is not owned by this metadata
		ForeignKeyCol: m.ForeignKeyCol,
		DefaultSort:   m.DefaultSort,
		DefaultLimit:  m.DefaultLimit,
		MaxLimit:      m.MaxLimit,
		Validator:     m.Validator,
		Auditor:       m.Auditor,
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

// FilterValue represents a filter with value and operator
type FilterValue struct {
	Value    string
	Operator string // eq, gt, gte, lt, lte, like, neq (eq is default)
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
