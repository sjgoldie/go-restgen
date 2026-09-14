package router

import (
	"context"
	"log/slog"
	"reflect"
	"slices"

	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/metadata"
)

// ScopedGrant is re-exported from metadata package for convenience
type ScopedGrant = metadata.ScopedGrant

// PartitionConfig declares a partition on a route.
type PartitionConfig struct {
	Name  string // Partition name, matched against ScopedGrant.Partition (e.g., "region")
	Field string // Go field on the model holding the partition value (e.g., "Region")
}

// WithPartition divides a route's rows by a named partition held in field.
//
// A caller holding one of the method's required scopes in AuthInfo.Scopes is unrestricted.
// Otherwise the caller needs a ScopedGrant for one of those scopes on this partition, and
// reads, includes, counts, and relation filters are narrowed to the grant values. Creates
// and updates must set field to one of those values (a missing value is rejected with 400,
// a value outside the grant with 403), and an update cannot move a row out of them.
//
// Child routes inherit the partition and are narrowed through their parent row. Declare
// WithPartition with the same name on a child to narrow it by its own field instead.
// The field cannot be the primary key.
// Every method on a partitioned route must require explicit scopes: public, auth-only,
// and scope-less configs are blocked.
func WithPartition(name, field string) PartitionConfig {
	return PartitionConfig{Name: name, Field: field}
}

// resolvePartitions builds the partitions for a type: the parent's partitions inherited,
// overridden or extended by the route's own WithPartition declarations.
func resolvePartitions(parentMeta *metadata.TypeMetadata, tType reflect.Type, pkField string, configs []PartitionConfig) []metadata.Partition {
	var partitions []metadata.Partition
	if parentMeta != nil {
		for _, p := range parentMeta.Partitions {
			partitions = append(partitions, metadata.Partition{Name: p.Name})
		}
	}

	for _, cfg := range configs {
		if cfg.Name == "" || cfg.Field == "" {
			slog.WarnContext(context.Background(), "WithPartition requires a name and a field; only callers with a global scope can access this route",
				"type", tType.Name(),
				"partition", cfg.Name,
				"field", cfg.Field)
		} else if cfg.Field == pkField {
			slog.WarnContext(context.Background(), "WithPartition cannot use the primary key; scoped callers see no rows",
				"type", tType.Name(),
				"partition", cfg.Name,
				"field", cfg.Field)
		} else if _, err := datastore.ColumnName(tType, cfg.Field); err != nil {
			slog.WarnContext(context.Background(), "WithPartition field is not a column on the model; scoped callers see no rows",
				"type", tType.Name(),
				"partition", cfg.Name,
				"field", cfg.Field,
				"error", err)
		}

		idx := slices.IndexFunc(partitions, func(p metadata.Partition) bool { return p.Name == cfg.Name })
		if idx >= 0 {
			partitions[idx].Field = cfg.Field
		} else {
			partitions = append(partitions, metadata.Partition{Name: cfg.Name, Field: cfg.Field})
		}
	}

	return partitions
}

// warnPartitionAuth logs registration warnings for auth configs that cannot be used on a
// partitioned route. Such configs are blocked at request time by checkAuth.
func warnPartitionAuth(meta *metadata.TypeMetadata, path string, configs map[string]*AuthConfig) {
	if len(meta.Partitions) == 0 {
		return
	}
	for method, config := range configs {
		if config == nil || partitionScopesUsable(config.Scopes) {
			continue
		}
		slog.WarnContext(context.Background(), "partitioned route requires explicit scopes; public, auth-only, and scope-less configs are blocked",
			"type", meta.TypeName,
			"path", path,
			"method", method)
	}
}

// partitionScopesUsable reports whether a scope list can be resolved against partitions.
func partitionScopesUsable(scopes []string) bool {
	return len(scopes) > 0 && !containsScope(scopes, ScopePublic) && !containsScope(scopes, ScopeAuthOnly)
}

// resolvePartitionAccess resolves the caller's access to each partition for a config's
// required scopes. A global scope makes every partition unrestricted. Otherwise each
// partition needs at least one grant for a required scope; ok is false if any has none.
func resolvePartitionAccess(authInfo *AuthInfo, scopes []string, partitions []metadata.Partition) (metadata.PartitionScope, bool) {
	result := make(metadata.PartitionScope, len(partitions))

	if hasAnyScope(authInfo.Scopes, scopes) {
		for _, p := range partitions {
			result[p.Name] = metadata.PartitionAccess{Unrestricted: true}
		}
		return result, true
	}

	for _, p := range partitions {
		var values []string
		granted := false
		for _, grant := range authInfo.Grants {
			if grant.Partition == "" || grant.Partition != p.Name || !containsScope(scopes, grant.Scope) {
				continue
			}
			granted = true
			for _, v := range grant.Values {
				if !slices.Contains(values, v) {
					values = append(values, v)
				}
			}
		}
		if !granted {
			return nil, false
		}
		result[p.Name] = metadata.PartitionAccess{Values: values}
	}

	return result, true
}
