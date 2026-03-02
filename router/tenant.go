package router

// TenantConfig configures tenant scoping for a route.
// The Field is the Go struct field name that holds the tenant ID (e.g., "OrgID").
// All models in the hierarchy must have this field. Children inherit from parent.
type TenantConfig struct {
	Field string // Go field name holding tenant ID (e.g., "OrgID")
}

// TenantTableConfig marks a route as the tenant entity itself.
// For these routes, the primary key IS the tenant ID.
// Queries filter by WHERE id = tenantID instead of WHERE tenant_field = tenantID.
type TenantTableConfig struct{}

// WithTenantScope returns a TenantConfig that enables tenant scoping.
// The field parameter is the Go struct field name holding the tenant ID.
// Configure on the root route — child routes inherit automatically.
func WithTenantScope(field string) TenantConfig {
	return TenantConfig{Field: field}
}

// IsTenantTable marks this route as the tenant entity itself (e.g., Organization).
// The model's primary key IS the tenant ID, so queries use WHERE id = tenantID.
// Does not propagate to child routes.
func IsTenantTable() TenantTableConfig {
	return TenantTableConfig{}
}
