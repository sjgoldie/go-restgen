package datastore

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/uptrace/bun"

	apperrors "github.com/sjgoldie/go-restgen/errors"
	"github.com/sjgoldie/go-restgen/metadata"
)

// columnRef identifies the table a condition refers to. The zero value refers to the
// query's own model through ?TableAlias; otherwise table is quoted as an identifier.
type columnRef struct {
	table string
}

// column returns a query fragment and args referring to col on this table.
func (r columnRef) column(col string) (string, []any) {
	if r.table == "" {
		return "?TableAlias.?", []any{bun.Ident(col)}
	}
	return "?.?", []any{bun.Ident(r.table), bun.Ident(col)}
}

// clause is a single WHERE or JOIN ON condition with its args.
type clause struct {
	query string
	args  []any
}

var noRows = clause{query: "1 = 0"}

// partitionScopeFor returns the partition access in force for the route's own queries
// (path "") or for a relation path, and whether partition narrowing is enforced for this
// request. Enforcement starts when the auth middleware has evaluated the request; callers
// without a request context (internal service use) are not narrowed, as with tenant and
// ownership scoping.
func partitionScopeFor(ctx context.Context, path string) (metadata.PartitionScope, bool) {
	includeScopes, authorized := ctx.Value(metadata.IncludePartitionScopesKey).(map[string]metadata.PartitionScope)
	if path == "" {
		if scope, ok := ctx.Value(metadata.PartitionScopeKey).(metadata.PartitionScope); ok {
			return scope, true
		}
		return nil, authorized
	}
	if !authorized {
		return nil, false
	}
	return includeScopes[path], true
}

// partitionClauses returns one condition per partition declared on meta, narrowing the
// rows at ref to the access in scope. A partition missing from scope matches no rows.
func (w *Wrapper[T]) partitionClauses(ctx context.Context, meta *metadata.TypeMetadata, ref columnRef, scope metadata.PartitionScope) []clause {
	clauses := make([]clause, 0, len(meta.Partitions))
	for _, p := range meta.Partitions {
		access, ok := scope[p.Name]
		if !ok {
			clauses = append(clauses, noRows)
			continue
		}
		if access.Unrestricted {
			continue
		}
		clauses = append(clauses, w.partitionValueClause(ctx, meta, p.Name, ref, access.Values))
	}
	return clauses
}

// partitionValueClause builds the condition that a row of meta at ref belongs to one of
// values for the named partition. When meta holds the partition field the column is
// compared directly; when the value is inherited, the row's parent must match, resolved
// through a subquery up the parent chain to the type that holds the field. A partition
// declared on the primary key is invalid and matches no rows.
func (w *Wrapper[T]) partitionValueClause(ctx context.Context, meta *metadata.TypeMetadata, name string, ref columnRef, values []string) clause {
	idx := slices.IndexFunc(meta.Partitions, func(p metadata.Partition) bool { return p.Name == name })
	if idx < 0 || len(values) == 0 {
		return noRows
	}
	partition := meta.Partitions[idx]
	modelType := derefType(meta.ModelType)

	if len(partition.Via) > 0 {
		holder := partition.Via[len(partition.Via)-1]
		return viaClause(w.Store.GetDB(), partition.Via, ref, func(holderRef columnRef) clause {
			return w.partitionHolderClause(ctx, partition, holder, holderRef, values)
		})
	}

	if partition.Field != "" && partition.Field == meta.PKField {
		return noRows
	}
	if partition.Field != "" {
		col, err := ColumnName(modelType, partition.Field)
		if err != nil {
			return noRows
		}
		query, args := ref.column(col)
		return clause{query: query + " IN (?)", args: append(args, bun.List(partitionFilterValues(ctx, modelType, partition.Field, values)))}
	}

	parent := meta.ParentMeta
	if parent == nil || meta.ForeignKeyCol == "" {
		return noRows
	}
	parentClause := w.partitionValueClause(ctx, parent, name, columnRef{table: parent.TableName}, values)
	joinCol := defaultParentJoinCol(meta.ParentJoinCol)

	subq := w.Store.GetDB().NewSelect().Table(parent.TableName)
	var childCol string
	if w.hasColumn(meta.ModelType, meta.ForeignKeyCol) {
		// child.fk = parent.joinCol
		subq = subq.ColumnExpr("?.?", bun.Ident(parent.TableName), bun.Ident(joinCol))
		childCol = meta.ForeignKeyCol
	} else {
		// Inverted: parent.fk = child.joinCol
		subq = subq.ColumnExpr("?.?", bun.Ident(parent.TableName), bun.Ident(meta.ForeignKeyCol))
		childCol = joinCol
	}
	subq = subq.Where(parentClause.query, parentClause.args...)

	query, args := ref.column(childCol)
	return clause{query: query + " IN (?)", args: append(args, subq)}
}

// viaClause builds the condition that a row at ref references, through the belongs-to steps,
// a row satisfying leaf. leaf receives a reference to the last related table.
func viaClause(db *bun.DB, steps []metadata.RelationStep, ref columnRef, leaf func(columnRef) clause) clause {
	last := len(steps) - 1
	cond := leaf(columnRef{table: steps[last].Table})
	for i := last; i >= 0; i-- {
		step := steps[i]
		subq := db.NewSelect().
			Table(step.Table).
			ColumnExpr("?.?", bun.Ident(step.Table), bun.Ident(step.JoinColumn)).
			Where(cond.query, cond.args...)

		prev := ref
		if i > 0 {
			prev = columnRef{table: steps[i-1].Table}
		}
		query, args := prev.column(step.FKColumn)
		cond = clause{query: query + " IN (?)", args: append(args, subq)}
	}
	return cond
}

// partitionFilterValues converts partition values to the field's Go kind so they compare
// correctly with the column.
func partitionFilterValues(ctx context.Context, modelType reflect.Type, fieldName string, values []string) []any {
	field, found := modelType.FieldByName(fieldName)
	result := make([]any, len(values))
	for i, v := range values {
		if !found {
			result[i] = v
			continue
		}
		result[i] = convertSingleValue(ctx, field.Type.Kind(), fieldName, v)
	}
	return result
}

// scopedBypassClauses returns conditions under which a scoped grant for one of the
// ownership bypass scopes lifts ownership for a row of meta at ref: the row is within
// the grant's values for a partition meta declares. Any one matching grant is enough.
func (w *Wrapper[T]) scopedBypassClauses(ctx context.Context, meta *metadata.TypeMetadata, ref columnRef, bypass []string) []clause {
	if meta == nil || len(meta.Partitions) == 0 || len(bypass) == 0 {
		return nil
	}
	if _, enforced := partitionScopeFor(ctx, ""); !enforced {
		return nil
	}
	values := scopedBypassValues(ctx, meta, bypass)
	clauses := make([]clause, 0, len(values))
	for _, p := range meta.Partitions {
		if vals := values[p.Name]; len(vals) > 0 {
			clauses = append(clauses, w.partitionValueClause(ctx, meta, p.Name, ref, vals))
		}
	}
	return clauses
}

// scopedBypassValues collects, per partition declared on meta, the values of the caller's
// grants for any of the bypass scopes.
func scopedBypassValues(ctx context.Context, meta *metadata.TypeMetadata, bypass []string) map[string][]string {
	authInfo, ok := ctx.Value(metadata.AuthInfoKey).(*metadata.AuthInfo)
	if !ok || authInfo == nil {
		return nil
	}
	values := make(map[string][]string)
	for _, grant := range authInfo.Grants {
		if grant.Partition == "" || !slices.Contains(bypass, grant.Scope) {
			continue
		}
		if !slices.ContainsFunc(meta.Partitions, func(p metadata.Partition) bool { return p.Name == grant.Partition }) {
			continue
		}
		for _, v := range grant.Values {
			if !slices.Contains(values[grant.Partition], v) {
				values[grant.Partition] = append(values[grant.Partition], v)
			}
		}
	}
	return values
}

// hasScopedBypass reports whether a scoped bypass grant covers an already loaded row.
// Only partitions whose value is held on the row itself can be checked in memory; inherited
// and related-row partitions never grant the bypass here.
func hasScopedBypass(ctx context.Context, meta *metadata.TypeMetadata, bypass []string, row any) bool {
	if meta == nil || len(meta.Partitions) == 0 || len(bypass) == 0 || row == nil {
		return false
	}
	if _, enforced := partitionScopeFor(ctx, ""); !enforced {
		return false
	}
	values := scopedBypassValues(ctx, meta, bypass)
	for _, p := range meta.Partitions {
		if p.Field == "" || len(p.Via) > 0 || len(values[p.Name]) == 0 {
			continue
		}
		if value, ok := partitionFieldValue(row, p.Field); ok && slices.Contains(values[p.Name], value) {
			return true
		}
	}
	return false
}

// partitionFieldValue returns the string form of a partition field on item, and false
// when the field is missing or holds its zero value.
func partitionFieldValue(item any, fieldName string) (string, bool) {
	v := reflect.ValueOf(item)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	field := v.FieldByName(fieldName)
	if !field.IsValid() || field.IsZero() {
		return "", false
	}
	return fmt.Sprintf("%v", field.Interface()), true
}

// enforcePartitionWrite checks the partition fields of an item being created or updated.
// Each partition held on the model must be set (400 when missing) and within the caller's
// access (403 when outside). On update, a stored row whose value is outside the caller's
// access was reached through a share: its value must stay unchanged, so it can be edited
// but not moved, even into the caller's own access.
// A partition declared on the primary key is invalid, so only an unrestricted caller can
// write. On update, existing is the stored row: a restricted caller cannot change the parent
// of a row whose partition is inherited, since that would move it between partitions.
func (w *Wrapper[T]) enforcePartitionWrite(ctx context.Context, meta *metadata.TypeMetadata, existing, item *T) error {
	if len(meta.Partitions) == 0 {
		return nil
	}
	scope, enforced := partitionScopeFor(ctx, "")
	if !enforced {
		return nil
	}

	for _, p := range meta.Partitions {
		access, ok := scope[p.Name]
		if !ok {
			return apperrors.ErrForbidden
		}

		if len(p.Via) > 0 {
			if err := w.enforceRelatedPartition(ctx, meta, p, access, existing, item); err != nil {
				return err
			}
			continue
		}

		if p.Field == "" {
			if existing != nil && !access.Unrestricted && w.parentChanged(meta, existing, item) {
				return apperrors.ErrForbidden
			}
			continue
		}

		if p.Field == meta.PKField {
			// The primary key cannot be a partition; only unrestricted callers can write
			if access.Unrestricted {
				continue
			}
			return apperrors.ErrForbidden
		}

		value, ok := partitionFieldValue(item, p.Field)
		if !ok {
			return apperrors.NewValidationError(fmt.Sprintf("%s is required", partitionFieldLabel(meta, p.Field)))
		}
		if existing != nil {
			// A stored row outside the caller's access was reached through a share: it can be
			// edited but its partition value cannot change
			if before, ok := partitionFieldValue(existing, p.Field); ok && !access.Allows(before) {
				if before == value {
					continue
				}
				return apperrors.ErrForbidden
			}
		}
		if !access.Allows(value) {
			return apperrors.ErrForbidden
		}
	}
	return nil
}

// enforceRelatedPartition checks a partition held on a related row. The reference to the first
// related row must be set (400 when missing). A restricted caller must reference a related row
// within their access, or on create one shared with them through the method's Via share setting
// (403 otherwise). On update they cannot change the reference of a row whose related row is
// outside their access, since it was reached through a share.
func (w *Wrapper[T]) enforceRelatedPartition(ctx context.Context, meta *metadata.TypeMetadata, p metadata.Partition, access metadata.PartitionAccess, existing, item *T) error {
	fkField := w.goNameFromColumn(meta.ModelType, p.Via[0].FKColumn)
	if fkField == "" {
		return apperrors.ErrForbidden
	}
	ref := reflect.ValueOf(item).Elem().FieldByName(fkField)
	if !ref.IsValid() {
		return apperrors.ErrForbidden
	}
	if ref.IsZero() {
		return apperrors.NewValidationError(fmt.Sprintf("%s is required", partitionFieldLabel(meta, fkField)))
	}
	if access.Unrestricted {
		return nil
	}

	var before reflect.Value
	if existing != nil {
		before = reflect.ValueOf(existing).Elem().FieldByName(fkField)
		if before.IsValid() && reflect.DeepEqual(before.Interface(), ref.Interface()) {
			return nil
		}
	}

	if existing != nil && before.IsValid() {
		within, err := w.relatedWithin(ctx, p, before.Interface(), access.Values)
		if err != nil {
			return err
		}
		if !within {
			return apperrors.ErrForbidden
		}
	}

	within, err := w.relatedWithin(ctx, p, ref.Interface(), access.Values)
	if err != nil {
		return err
	}
	if within {
		return nil
	}
	if existing == nil {
		shared, err := w.itemShared(ctx, meta, item)
		if err != nil {
			return err
		}
		if shared {
			return nil
		}
	}
	return apperrors.ErrForbidden
}

// itemShared reports whether a new item references, through the method's Via share setting,
// a row shared with the caller, so it can be created under a shared related row.
func (w *Wrapper[T]) itemShared(ctx context.Context, meta *metadata.TypeMetadata, item *T) (bool, error) {
	share := shareFor(ctx, "")
	if share == nil || len(share.Via) == 0 || derefType(meta.ModelType) != share.BaseType {
		return false, nil
	}
	authInfo, ok := ctx.Value(metadata.AuthInfoKey).(*metadata.AuthInfo)
	if !ok || authInfo == nil || authInfo.UserID == "" {
		return false, nil
	}

	first := share.Via[0]
	fkField := w.goNameFromColumn(meta.ModelType, first.FKColumn)
	if fkField == "" {
		return false, nil
	}
	reference := reflect.ValueOf(item).Elem().FieldByName(fkField)
	if !reference.IsValid() || reference.IsZero() {
		return false, nil
	}

	targets, err := w.sharedTargets(ctx, share, authInfo.UserID)
	if err != nil {
		return false, nil
	}
	target := share.Via[len(share.Via)-1]
	targetClause := func(targetRef columnRef) clause {
		query, args := targetRef.column(target.PKColumn)
		return clause{query: query + " IN (?)", args: append(args, targets)}
	}

	firstRef := columnRef{table: first.Table}
	cond := targetClause(firstRef)
	if len(share.Via) > 1 {
		cond = viaClause(w.Store.GetDB(), share.Via[1:], firstRef, targetClause)
	}

	count, err := w.getDB(ctx).NewSelect().
		Table(first.Table).
		Where("?.? = ?", bun.Ident(first.Table), bun.Ident(first.JoinColumn), reference.Interface()).
		Where(cond.query, cond.args...).
		Count(ctx)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// relatedWithin reports whether the related row referenced by reference exists and holds a
// partition value within values, following the rest of the relation path.
func (w *Wrapper[T]) relatedWithin(ctx context.Context, p metadata.Partition, reference any, values []string) (bool, error) {
	if len(values) == 0 {
		return false, nil
	}
	first := p.Via[0]
	firstRef := columnRef{table: first.Table}

	var cond clause
	if len(p.Via) == 1 {
		cond = w.partitionHolderClause(ctx, p, first, firstRef, values)
	} else {
		holder := p.Via[len(p.Via)-1]
		cond = viaClause(w.Store.GetDB(), p.Via[1:], firstRef, func(holderRef columnRef) clause {
			return w.partitionHolderClause(ctx, p, holder, holderRef, values)
		})
	}

	count, err := w.getDB(ctx).NewSelect().
		Table(first.Table).
		Where("?.? = ?", bun.Ident(first.Table), bun.Ident(first.JoinColumn), reference).
		Where(cond.query, cond.args...).
		Count(ctx)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// partitionHolderClause builds the condition that the model holding a partition's field, at ref,
// has a value within values.
func (w *Wrapper[T]) partitionHolderClause(ctx context.Context, p metadata.Partition, holder metadata.RelationStep, ref columnRef, values []string) clause {
	col, err := ColumnName(holder.ModelType, p.Field)
	if err != nil {
		return noRows
	}
	query, args := ref.column(col)
	return clause{query: query + " IN (?)", args: append(args, bun.List(partitionFilterValues(ctx, holder.ModelType, p.Field, values)))}
}

// parentChanged reports whether an update changes the column that links a row to its parent.
func (w *Wrapper[T]) parentChanged(meta *metadata.TypeMetadata, existing, item *T) bool {
	col := meta.ForeignKeyCol
	if col == "" || !w.hasColumn(meta.ModelType, col) {
		return false
	}
	goName := w.goNameFromColumn(meta.ModelType, col)
	if goName == "" {
		return false
	}
	before := reflect.ValueOf(existing).Elem().FieldByName(goName)
	after := reflect.ValueOf(item).Elem().FieldByName(goName)
	if !before.IsValid() || !after.IsValid() {
		return false
	}
	return !reflect.DeepEqual(before.Interface(), after.Interface())
}

// partitionFieldLabel returns the JSON name of a field for client-facing messages,
// falling back to the Go field name.
func partitionFieldLabel(meta *metadata.TypeMetadata, fieldName string) string {
	field, found := derefType(meta.ModelType).FieldByName(fieldName)
	if !found {
		return fieldName
	}
	name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
	if name == "" || name == "-" {
		return fieldName
	}
	return name
}
