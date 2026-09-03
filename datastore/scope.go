package datastore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"slices"

	"github.com/uptrace/bun"

	apperrors "github.com/sjgoldie/go-restgen/errors"
	"github.com/sjgoldie/go-restgen/internal/common"
	"github.com/sjgoldie/go-restgen/metadata"
)

// ownershipScope returns the ownership fields and bypass scopes in force for
// meta on this request. The auth middleware records the current HTTP method's
// OwnershipConfig in context for the route's own type; that wins so per-method
// ownership is honoured. For any other type (a child being included, a parent
// in the chain) and for callers without a request, the type-wide metadata is
// the source.
func ownershipScope(ctx context.Context, meta *metadata.TypeMetadata) (fields, bypass []string) {
	if meta == nil {
		return nil, nil
	}
	scope, ok := ctx.Value(metadata.OwnershipScopeKey).(*metadata.OwnershipScope)
	if ok && scope != nil && len(scope.Fields) > 0 {
		if routeMeta, err := metadata.FromContext(ctx); err == nil && routeMeta.TypeID == meta.TypeID {
			return scope.Fields, scope.BypassScopes
		}
	}
	return meta.OwnershipFields, meta.BypassScopes
}

// hasBypassScope reports whether the authenticated caller holds any of the bypass scopes.
func hasBypassScope(ctx context.Context, bypass []string) bool {
	authInfo, ok := ctx.Value(metadata.AuthInfoKey).(*metadata.AuthInfo)
	if !ok || authInfo == nil {
		return false
	}
	for _, scope := range bypass {
		if slices.Contains(authInfo.Scopes, scope) {
			return true
		}
	}
	return false
}

// pkColumn resolves the primary key column for a type from Bun's schema on
// this store, so filters never assume the column is literally "id". A model
// with a single PK uses that column; otherwise the configured PKField is
// resolved by Go name, and "id" is the last resort.
func (w *Wrapper[T]) pkColumn(meta *metadata.TypeMetadata) string {
	if meta == nil || meta.ModelType == nil {
		return "id"
	}
	table := w.Store.GetDB().Table(derefType(meta.ModelType))
	if len(table.PKs) == 1 {
		return table.PKs[0].Name
	}
	if meta.PKField != "" {
		for _, field := range table.Fields {
			if field.GoName == meta.PKField {
				return field.Name
			}
		}
	}
	return "id"
}

// reassertOwnership copies the ownership fields from the existing row onto the
// incoming item, so a caller without a bypass scope cannot reassign or orphan a
// row on update or patch. Mirrors setTenantField, which already prevents
// cross-tenant moves. Callers holding a bypass scope may change ownership.
func (w *Wrapper[T]) reassertOwnership(ctx context.Context, meta *metadata.TypeMetadata, existing, item *T) {
	enforced, ok := ctx.Value(metadata.OwnershipEnforcedKey).(bool)
	if !ok || !enforced || existing == nil || item == nil {
		return
	}
	fields, bypass := ownershipScope(ctx, meta)
	if len(fields) == 0 || hasBypassScope(ctx, bypass) {
		return
	}

	src := reflect.ValueOf(existing).Elem()
	dst := reflect.ValueOf(item).Elem()
	for _, name := range fields {
		from := src.FieldByName(name)
		to := dst.FieldByName(name)
		if from.IsValid() && to.IsValid() && to.CanSet() && from.Type() == to.Type() {
			to.Set(from)
		}
	}
}

// enforceTenantTablePK makes a create on the tenant entity itself target the
// caller's own tenant. An empty primary key is filled with the tenant ID; a
// different one is rejected, so a tenant cannot pre-create another tenant's row.
func (w *Wrapper[T]) enforceTenantTablePK(ctx context.Context, meta *metadata.TypeMetadata, item *T) error {
	if !meta.IsTenantTable {
		return nil
	}
	enforced, ok := ctx.Value(metadata.TenantScopedKey).(bool)
	if !ok || !enforced {
		return nil
	}
	tenantID, ok := ctx.Value(metadata.TenantIDValueKey).(string)
	if !ok || tenantID == "" {
		return fmt.Errorf("tenant scoped but tenant ID missing from context")
	}

	switch pk := common.GetFieldAsString(item, meta.PKField); pk {
	case "":
		if err := common.SetFieldFromString(item, meta.PKField, tenantID); err != nil {
			return fmt.Errorf("cannot set tenant primary key: %w", err)
		}
		return nil
	case tenantID:
		return nil
	default:
		return apperrors.NewValidationError("tenant primary key must match the caller's tenant")
	}
}

// applyChildScopeFilters narrows a relation subquery on tableName to the rows
// the caller may see: the child's ownership fields when applyOwnership is set
// (unless the caller holds a bypass scope), and the child's tenant field when
// the request is tenant scoped. This is the same scoping ?include= applies, so
// relation counts and existence filters cannot reveal rows a caller cannot list.
func (w *Wrapper[T]) applyChildScopeFilters(ctx context.Context, q *bun.SelectQuery, childMeta *metadata.TypeMetadata, tableName string, applyOwnership bool) *bun.SelectQuery {
	childType := derefType(childMeta.ModelType)

	if applyOwnership && len(childMeta.OwnershipFields) > 0 && !hasBypassScope(ctx, childMeta.BypassScopes) {
		authInfo, _ := ctx.Value(metadata.AuthInfoKey).(*metadata.AuthInfo)
		if authInfo == nil || authInfo.UserID == "" {
			return q.Where("1 = 0")
		}
		q = q.WhereGroup(" AND ", func(g *bun.SelectQuery) *bun.SelectQuery {
			first := true
			for _, field := range childMeta.OwnershipFields {
				col, err := ColumnName(childType, field)
				if err != nil {
					continue
				}
				if first {
					g = g.Where("?.? = ?", bun.Ident(tableName), bun.Ident(col), authInfo.UserID)
					first = false
				} else {
					g = g.WhereOr("?.? = ?", bun.Ident(tableName), bun.Ident(col), authInfo.UserID)
				}
			}
			return g
		})
	}

	if enforced, ok := ctx.Value(metadata.TenantScopedKey).(bool); ok && enforced && childMeta.TenantField != "" {
		tenantID, _ := ctx.Value(metadata.TenantIDValueKey).(string)
		if col, err := ColumnName(childType, childMeta.TenantField); err == nil {
			q = q.Where("?.? = ?", bun.Ident(tableName), bun.Ident(col), tenantID)
		}
	}

	return q
}

// GetMany fetches the rows for ids in a single query, applying the same parent,
// ownership, tenant, and include handling as Get. Results are in the order of
// ids. Any ID that is missing or not visible to the caller yields ErrNotFound.
func (w *Wrapper[T]) GetMany(ctx context.Context, ids []string) ([]*T, error) {
	ctx, cancel := context.WithTimeout(ctx, w.Store.GetTimeout())
	defer cancel()

	meta, err := metadata.FromContext(ctx)
	if err != nil {
		return nil, err
	}
	return w.fetchByIDs(ctx, meta, ids)
}

// fetchByIDs is GetMany without the timeout, for use inside an operation that
// already has one (and, inside a transaction, uses that transaction).
func (w *Wrapper[T]) fetchByIDs(ctx context.Context, meta *metadata.TypeMetadata, ids []string) ([]*T, error) {
	if len(ids) == 0 {
		return []*T{}, nil
	}

	rows := []*T{}
	query := w.getDB(ctx).NewSelect().Model(&rows)

	query, err := w.applyParentFiltersWithMeta(ctx, query, meta)
	if err != nil {
		return nil, err
	}
	query, err = w.applyOwnershipFilterWithMeta(ctx, query, meta)
	if err != nil {
		return nil, err
	}
	query, err = w.applyTenantFilter(ctx, query, meta)
	if err != nil {
		return nil, err
	}
	query = w.applyRelationIncludes(ctx, query, metadata.QueryOptionsFromContext(ctx), meta)

	unique := slices.Compact(slices.Sorted(slices.Values(ids)))
	pkValues := make([]any, len(unique))
	for i, id := range unique {
		pkValues[i] = id
	}
	query = query.Where("?TablePKs IN (?)", bun.List(pkValues))

	if err := query.Scan(ctx); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		if errors.Is(err, sql.ErrConnDone) {
			return nil, apperrors.ErrUnavailable
		}
		return nil, err
	}

	byID := make(map[string]*T, len(rows))
	for _, row := range rows {
		byID[common.GetFieldAsString(row, meta.PKField)] = row
	}

	result := make([]*T, len(ids))
	for i, id := range ids {
		row, ok := byID[id]
		if !ok {
			return nil, apperrors.ErrNotFound
		}
		result[i] = row
	}
	return result, nil
}
