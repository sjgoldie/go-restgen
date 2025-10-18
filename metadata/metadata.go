package metadata

import (
	"fmt"
	"reflect"
	"strings"
	"sync"

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
	TypeID          string       // Unique UUID for this type
	TypeName        string       // Go type name (e.g., "User")
	TableName       string       // Database table name
	URLParamUUID    string       // UUID used as chi URL parameter name (e.g., "abc-123")
	ParentType      reflect.Type // Go type of parent (nil if root)
	ForeignKeyCol   string       // Column in THIS table that references parent (e.g., "user_id")
	OwnershipFields []string     // Model field names for ownership validation (OR logic)
	BypassScopes    []string     // Scopes that bypass ownership validation (e.g., "admin")
}

var (
	registry     = make(map[reflect.Type]*TypeMetadata) // Go type -> metadata
	registryLock sync.RWMutex
)

// Register stores metadata for a type
func Register(metadata *TypeMetadata, goType reflect.Type) {
	registryLock.Lock()
	defer registryLock.Unlock()

	registry[goType] = metadata
}

// Get retrieves metadata for a type T
func Get[T any]() (*TypeMetadata, error) {
	var t T
	tType := reflect.TypeOf(t)
	if tType.Kind() == reflect.Ptr {
		tType = tType.Elem()
	}

	return GetByType(tType)
}

// GetByType retrieves metadata for a reflect.Type
func GetByType(t reflect.Type) (*TypeMetadata, error) {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	registryLock.RLock()
	defer registryLock.RUnlock()

	metadata, exists := registry[t]
	if !exists {
		return nil, fmt.Errorf("type %s not registered", t.Name())
	}

	return metadata, nil
}

// GenerateTypeID creates a unique ID for a type registration
// Returns a UUID with hyphens replaced by underscores to be compatible with chi URL parameters
func GenerateTypeID() string {
	return strings.ReplaceAll(uuid.New().String(), "-", "_")
}

// UpdateOwnership updates the ownership configuration for a registered type
func UpdateOwnership(goType reflect.Type, ownershipFields, bypassScopes []string) error {
	if goType.Kind() == reflect.Ptr {
		goType = goType.Elem()
	}

	registryLock.Lock()
	defer registryLock.Unlock()

	metadata, exists := registry[goType]
	if !exists {
		return fmt.Errorf("type %s not registered", goType.Name())
	}

	metadata.OwnershipFields = ownershipFields
	metadata.BypassScopes = bypassScopes
	return nil
}
