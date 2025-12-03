package metadata

import (
	"context"
	"reflect"
	"strings"

	apperrors "github.com/sjgoldie/go-restgen/errors"

	"github.com/google/uuid"
)

// AuthInfo contains authentication and authorization information.
// Developers populate this in their auth middleware and add to context.
type AuthInfo struct {
	UserID string   // External user ID (e.g., Auth0 ID, Firebase UID, JWT sub claim)
	Scopes []string // List of scopes/permissions the user has
}

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
