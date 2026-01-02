package datastore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	apperrors "github.com/sjgoldie/go-restgen/errors"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/driver/pgdriver"
)

// Wrapper is a generic struct that wraps a Store interface to provide CRUD operations
type Wrapper[T any] struct {
	Store Store
}

// preFetchResult holds the result of pre-fetching items for batch operations
type preFetchResult[T any] struct {
	ids           []string
	existingItems []*T
}

// GetAll retrieves all items of type T from the datastore
// Filters from context are applied automatically
// Relations can be loaded via ?include= query parameter (parsed into QueryOptions.Include)
// Returns items, total count (0 if not requested), and error
func (w *Wrapper[T]) GetAll(ctx context.Context) ([]*T, int, error) {
	ctx, cancel := context.WithTimeout(ctx, w.Store.GetTimeout())
	defer cancel()

	items := []*T{}
	query := w.Store.GetDB().NewSelect().Model(&items)

	// Get metadata from context
	meta, err := metadata.FromContext(ctx)
	if err != nil {
		return nil, 0, err
	}

	// Apply parent filters and JOINs from metadata
	query, err = w.applyParentFiltersWithMeta(ctx, query, meta)
	if err != nil {
		return nil, 0, err
	}

	// Apply ownership filter for type T
	query, err = w.applyOwnershipFilterWithMeta(ctx, query, meta)
	if err != nil {
		return nil, 0, err
	}

	// Get query options from context (optional)
	opts := metadata.QueryOptionsFromContext(ctx)

	// Apply filters from query options
	query = w.applyQueryFilters(query, opts, meta)

	// Get total count BEFORE sorting/pagination (if requested)
	// Use the same query with filters already applied
	var totalCount int
	if opts != nil && opts.CountTotal {
		totalCount, err = query.Count(ctx)
		if err != nil {
			return nil, 0, err
		}
	}

	// Apply sorting from query options (or default sort)
	query = w.applyQuerySorting(query, opts, meta)

	// Apply pagination AFTER count
	query = w.applyQueryPagination(query, opts, meta)

	// Apply relation includes from query options
	query = w.applyRelationIncludes(ctx, query, opts, meta)

	if err := query.Scan(ctx); err != nil {
		// Pass through context errors unchanged
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, 0, err
		}
		// Check for connection issues
		if errors.Is(err, sql.ErrConnDone) {
			return nil, 0, apperrors.ErrUnavailable
		}
		return nil, 0, err
	}

	return items, totalCount, nil
}

// Get retrieves a single item of type T by ID from the datastore
// Filters from context (parent IDs) are applied automatically
// Relations can be loaded via ?include= query parameter (parsed into QueryOptions.Include)
// The id parameter is a string to support both integer and UUID primary keys
func (w *Wrapper[T]) Get(ctx context.Context, id string) (*T, error) {
	// Get metadata from context
	meta, err := metadata.FromContext(ctx)
	if err != nil {
		return nil, err
	}
	item, err := w.getWithMeta(ctx, meta, id)
	if err != nil {
		return nil, err
	}
	return item.(*T), nil
}

// Create inserts a new item of type T into the datastore
func (w *Wrapper[T]) Create(ctx context.Context, item T) (*T, error) {
	ctx, cancel := context.WithTimeout(ctx, w.Store.GetTimeout())
	defer cancel()

	// Get metadata from context
	meta, err := metadata.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	// If this type has a parent, validate parent exists and set foreign key
	if meta.ParentMeta != nil {
		parentMeta := meta.ParentMeta

		// Extract parent ID from context
		parentIDs, ok := ctx.Value(metadata.ParentIDsKey).(map[string]string)
		if !ok || parentIDs == nil {
			return nil, fmt.Errorf("parent context missing for nested resource")
		}

		parentID, exists := parentIDs[parentMeta.URLParamUUID]
		if !exists {
			return nil, fmt.Errorf("parent ID not found in context")
		}

		// Validate parent exists by calling getByType on it
		// This validates the full parent chain automatically
		// Parent validation will use the parent's own ownership config from metadata
		parentItem, err := w.getWithMeta(ctx, parentMeta, parentID)
		if err != nil {
			return nil, err
		}

		// Set the foreign key field on the item using the parent's actual PK value
		if err := w.setForeignKey(&item, meta.ForeignKeyCol, parentItem, parentMeta.PKField); err != nil {
			return nil, err
		}
	}

	// Set ownership field if enforced
	if err := w.setOwnershipField(ctx, &item); err != nil {
		return nil, err
	}

	// Run custom validation (after ownership is set so validator sees final state)
	if err := w.runValidation(ctx, meta, metadata.OpCreate, nil, &item); err != nil {
		return nil, err
	}

	// If audit is configured, wrap in transaction
	if meta.Auditor != nil {
		err = w.Store.GetDB().RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			// Insert the item
			_, err := tx.NewInsert().Model(&item).Returning("*").Exec(ctx)
			if err != nil {
				return err
			}
			// Run audit (item now has ID populated)
			return w.runAudit(ctx, tx, meta, metadata.OpCreate, nil, &item)
		})
	} else {
		// No audit, just insert directly
		_, err = w.Store.GetDB().NewInsert().Model(&item).Returning("*").Exec(ctx)
	}

	if err != nil {
		// Pass through context errors unchanged
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		// Check for connection issues
		if errors.Is(err, sql.ErrConnDone) {
			return nil, apperrors.ErrUnavailable
		}
		// Check for constraint violations
		var pgErr pgdriver.Error
		if errors.As(err, &pgErr) {
			switch pgErr.Field('C') {
			case "23505": // unique_violation
				return nil, apperrors.ErrDuplicate
			case "23503": // foreign_key_violation
				return nil, apperrors.ErrInvalidReference
			}
		}
		// SQLite constraint violations
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return nil, apperrors.ErrDuplicate
		}
		if strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
			return nil, apperrors.ErrInvalidReference
		}
		return nil, err
	}

	return &item, nil
}

// Update updates an existing item of type T in the datastore
// The id parameter is a string to support both integer and UUID primary keys
func (w *Wrapper[T]) Update(ctx context.Context, id string, item T) (*T, error) {
	ctx, cancel := context.WithTimeout(ctx, w.Store.GetTimeout())
	defer cancel()

	// Get metadata from context
	meta, err := metadata.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	// Validate item exists (and belongs to parent chain if applicable)
	// This also provides the old value for validation
	existing, err := w.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Run custom validation with old and new values
	if err := w.runValidation(ctx, meta, metadata.OpUpdate, existing, &item); err != nil {
		return nil, err
	}

	// If audit is configured, wrap in transaction
	if meta.Auditor != nil {
		err = w.Store.GetDB().RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			// Update the item
			err := tx.NewUpdate().Model(&item).WherePK().Returning("*").Scan(ctx)
			if err != nil {
				return err
			}
			// Run audit with old and new values
			return w.runAudit(ctx, tx, meta, metadata.OpUpdate, existing, &item)
		})
	} else {
		// No audit, just update directly
		err = w.Store.GetDB().NewUpdate().Model(&item).WherePK().Returning("*").Scan(ctx)
	}

	if err != nil {
		// Pass through context errors unchanged
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		// Check for connection issues
		if errors.Is(err, sql.ErrConnDone) {
			return nil, apperrors.ErrUnavailable
		}
		// Check for not found
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperrors.ErrNotFound
		}
		// Check for foreign key violations
		var pgErr pgdriver.Error
		if errors.As(err, &pgErr) && pgErr.Field('C') == "23503" {
			return nil, apperrors.ErrInvalidReference
		}
		if strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
			return nil, apperrors.ErrInvalidReference
		}
		return nil, err
	}

	return &item, nil
}

// Delete removes an item of type T from the datastore by ID
// The id parameter is a string to support both integer and UUID primary keys
func (w *Wrapper[T]) Delete(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, w.Store.GetTimeout())
	defer cancel()

	// Get metadata from context
	meta, err := metadata.FromContext(ctx)
	if err != nil {
		return err
	}

	// Validate item exists (and belongs to parent chain if applicable)
	// This also provides the item for validation
	existing, err := w.Get(ctx, id)
	if err != nil {
		return err
	}

	// Run custom validation with old value (new is nil for delete)
	if err := w.runValidation(ctx, meta, metadata.OpDelete, existing, nil); err != nil {
		return err
	}

	var item T
	var result sql.Result

	// If audit is configured, wrap in transaction
	if meta.Auditor != nil {
		err = w.Store.GetDB().RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			// Delete the item using ?TablePKs to support any PK type
			var txErr error
			result, txErr = tx.NewDelete().Model(&item).Where("?TablePKs = ?", id).Exec(ctx)
			if txErr != nil {
				return txErr
			}
			// Run audit with old value (new is nil for delete)
			return w.runAudit(ctx, tx, meta, metadata.OpDelete, existing, nil)
		})
	} else {
		// No audit, just delete directly using ?TablePKs to support any PK type
		result, err = w.Store.GetDB().NewDelete().Model(&item).Where("?TablePKs = ?", id).Exec(ctx)
	}

	if err != nil {
		// Pass through context errors unchanged
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		// Check for connection issues
		if errors.Is(err, sql.ErrConnDone) {
			return apperrors.ErrUnavailable
		}
		return err
	}

	// Check if any rows were affected
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}

	return nil
}

// getWithMeta retrieves a single item by ID using metadata
// This allows validating parent existence with full chain validation
// Relations can be loaded via ?include= query parameter (parsed into QueryOptions.Include)
// The id parameter is a string to support both integer and UUID primary keys
func (w *Wrapper[T]) getWithMeta(ctx context.Context, meta *metadata.TypeMetadata, id string) (interface{}, error) {
	ctx, cancel := context.WithTimeout(ctx, w.Store.GetTimeout())
	defer cancel()

	// Create new instance of the type from metadata
	item := reflect.New(meta.ModelType).Interface()
	query := w.Store.GetDB().NewSelect().Model(item)

	// Apply parent filters and JOINs from metadata
	query, err := w.applyParentFiltersWithMeta(ctx, query, meta)
	if err != nil {
		return nil, err
	}

	// Apply ownership filter using the metadata
	query, err = w.applyOwnershipFilterWithMeta(ctx, query, meta)
	if err != nil {
		return nil, err
	}

	// Get query options from context and apply relation includes
	opts := metadata.QueryOptionsFromContext(ctx)
	query = w.applyRelationIncludes(ctx, query, opts, meta)

	// Filter by ID using Bun's table primary key placeholder
	query = query.Where("?TablePKs = ?", id)

	if err := query.Scan(ctx); err != nil {
		// Pass through context errors unchanged
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperrors.ErrNotFound
		}
		// Check for connection issues
		if errors.Is(err, sql.ErrConnDone) {
			return nil, apperrors.ErrUnavailable
		}
		return nil, err
	}

	return item, nil
}

// setForeignKey sets the foreign key field on the item using reflection.
// parentItem is the parent object from which we extract the primary key value.
// parentPKField specifies the parent's primary key field name (from parentMeta.PKField).
func (w *Wrapper[T]) setForeignKey(item *T, foreignKeyCol string, parentItem interface{}, parentPKField string) error {
	// Convert column name to field name (e.g., "author_id" -> "AuthorID")
	fieldName := fieldNameFromColumn(foreignKeyCol)

	// Use reflection to set the field
	itemValue := reflect.ValueOf(item).Elem()
	fkField := itemValue.FieldByName(fieldName)
	if !fkField.IsValid() || !fkField.CanSet() {
		return fmt.Errorf("cannot set foreign key field %s", fieldName)
	}

	// Extract the PK from the parent item
	parentValue := reflect.ValueOf(parentItem)
	if parentValue.Kind() == reflect.Ptr {
		parentValue = parentValue.Elem()
	}
	parentIDField := parentValue.FieldByName(parentPKField)
	if !parentIDField.IsValid() {
		return fmt.Errorf("parent item has no %s field", parentPKField)
	}

	// Set the foreign key based on its type
	switch fkField.Kind() {
	case reflect.Int, reflect.Int64:
		// FK is int, parent ID should be int
		if parentIDField.Kind() == reflect.Int || parentIDField.Kind() == reflect.Int64 {
			fkField.SetInt(parentIDField.Int())
		} else {
			return fmt.Errorf("foreign key field %s is int but parent %s is %s", fieldName, parentPKField, parentIDField.Type())
		}
	case reflect.String:
		// FK is string (for UUID stored as string)
		fkField.SetString(fmt.Sprintf("%v", parentIDField.Interface()))
	default:
		// For uuid.UUID or other types, try direct assignment if types match
		if fkField.Type() == parentIDField.Type() {
			fkField.Set(parentIDField)
		} else {
			return fmt.Errorf("cannot set foreign key field %s: type mismatch (field: %s, parent: %s)",
				fieldName, fkField.Type(), parentIDField.Type())
		}
	}

	return nil
}

// hasField checks if a type has a field matching the given column name
func hasField(t reflect.Type, colName string) bool {
	if colName == "" {
		return false
	}
	fieldName := fieldNameFromColumn(colName)
	_, found := t.FieldByName(fieldName)
	return found
}

// fieldNameFromColumn converts a database column name to a Go field name
// e.g., "author_id" -> "AuthorID", "post_id" -> "PostID"
func fieldNameFromColumn(col string) string {
	parts := strings.Split(col, "_")
	for i, part := range parts {
		if len(part) > 0 {
			// Special case for "id" -> "ID"
			if strings.ToLower(part) == "id" {
				parts[i] = "ID"
			} else {
				parts[i] = strings.ToUpper(part[:1]) + part[1:]
			}
		}
	}
	return strings.Join(parts, "")
}

// applyParentFiltersWithMeta applies parent ID filters and JOINs using metadata chain
func (w *Wrapper[T]) applyParentFiltersWithMeta(ctx context.Context, query *bun.SelectQuery, currentMeta *metadata.TypeMetadata) (*bun.SelectQuery, error) {
	// Metadata is required - nil means programming error
	if currentMeta == nil {
		return nil, fmt.Errorf("metadata is nil for type")
	}

	// If no parent, this is a root resource - no filters needed
	if currentMeta.ParentMeta == nil {
		return query, nil
	}

	// Extract parent IDs from context
	parentIDs, ok := ctx.Value(metadata.ParentIDsKey).(map[string]string)
	if !ok || parentIDs == nil {
		return query, nil
	}

	// Walk up the parent chain to collect all join info
	// Build list from child -> parent -> grandparent
	type joinInfo struct {
		childType     reflect.Type
		childTable    string
		childFKCol    string
		parentTable   string
		parentURLUUID string
	}

	var joins []joinInfo
	childMeta := currentMeta
	parentMeta := currentMeta.ParentMeta

	// Walk up the chain using ParentMeta pointers
	for parentMeta != nil {
		joins = append(joins, joinInfo{
			childType:     childMeta.ModelType,
			childTable:    childMeta.TableName,
			childFKCol:    childMeta.ForeignKeyCol,
			parentTable:   parentMeta.TableName,
			parentURLUUID: parentMeta.URLParamUUID,
		})

		// Move up the chain
		childMeta = parentMeta
		parentMeta = parentMeta.ParentMeta
	}

	// Now build the JOINs and WHERE clauses
	baseType := currentMeta.ModelType
	for _, join := range joins {
		// Check if we have a parent ID for this level
		parentID, exists := parentIDs[join.parentURLUUID]
		if !exists {
			continue
		}

		// Determine if FK is on child or parent by checking if child has the FK field
		fkOnChild := hasField(join.childType, join.childFKCol)

		// Check if child is the base model being queried
		if join.childType == baseType {
			// Child is the base model, use ?TableAlias
			if fkOnChild {
				// Normal case: child.FK = parent.id
				query = query.Join("JOIN ? ON ?TableAlias.? = ?.?",
					bun.Ident(join.parentTable),
					bun.Ident(join.childFKCol),
					bun.Ident(join.parentTable), bun.Ident("id"))
			} else {
				// Inverted case: parent.FK = child.id
				query = query.Join("JOIN ? ON ?.? = ?TableAlias.?",
					bun.Ident(join.parentTable),
					bun.Ident(join.parentTable), bun.Ident(join.childFKCol),
					bun.Ident("id"))
			}
		} else {
			// Child is a previously joined table, use table name
			if fkOnChild {
				// Normal case: child.FK = parent.id
				query = query.Join("JOIN ? ON ?.? = ?.?",
					bun.Ident(join.parentTable),
					bun.Ident(join.childTable), bun.Ident(join.childFKCol),
					bun.Ident(join.parentTable), bun.Ident("id"))
			} else {
				// Inverted case: parent.FK = child.id
				query = query.Join("JOIN ? ON ?.? = ?.?",
					bun.Ident(join.parentTable),
					bun.Ident(join.parentTable), bun.Ident(join.childFKCol),
					bun.Ident(join.childTable), bun.Ident("id"))
			}
		}

		// WHERE parent_table.id = ?
		query = query.Where("?.? = ?",
			bun.Ident(join.parentTable), bun.Ident("id"), parentID)
	}

	return query, nil
}

// applyOwnershipFilterWithMeta applies ownership filtering to a query if enforced in context
// Uses the provided metadata for ownership configuration
func (w *Wrapper[T]) applyOwnershipFilterWithMeta(ctx context.Context, query *bun.SelectQuery, meta *metadata.TypeMetadata) (*bun.SelectQuery, error) {
	// Check if ownership is enforced
	enforced, ok := ctx.Value(metadata.OwnershipEnforcedKey).(bool)
	if !ok || !enforced {
		return query, nil
	}

	// Get ownership information from context
	userID, ok := ctx.Value(metadata.OwnershipUserIDKey).(string)
	if !ok || userID == "" {
		return nil, fmt.Errorf("ownership enforced but user ID missing from context")
	}

	// If no metadata or no ownership fields configured for this type, skip filter
	if meta == nil || len(meta.OwnershipFields) == 0 {
		return query, nil
	}

	// Check if user has bypass scope
	// Compare user's scopes (from AuthInfo in context) with bypass scopes from metadata
	if authInfo, ok := ctx.Value(metadata.AuthInfoKey).(*metadata.AuthInfo); ok && authInfo != nil && len(meta.BypassScopes) > 0 {
		// Check if user has any bypass scope
		for _, bypassScope := range meta.BypassScopes {
			for _, userScope := range authInfo.Scopes {
				if userScope == bypassScope {
					// User has bypass scope, don't apply ownership filter
					return query, nil
				}
			}
		}
	}

	// Get the model type for column name lookup
	itemType := meta.ModelType
	if itemType.Kind() == reflect.Ptr {
		itemType = itemType.Elem()
	}

	// Build OR conditions: WHERE (field1 = ? OR field2 = ? OR ...)
	// Use ?TableAlias to properly qualify columns when JOINs are present
	if len(meta.OwnershipFields) == 1 {
		// Single field - simple WHERE clause
		colName, err := fieldToColumnName(itemType, meta.OwnershipFields[0])
		if err != nil {
			return nil, fmt.Errorf("failed to get column name for ownership field: %w", err)
		}
		query = query.Where("?TableAlias.? = ?", bun.Ident(colName), userID)
	} else {
		// Multiple fields - OR logic
		for i, fieldName := range meta.OwnershipFields {
			colName, err := fieldToColumnName(itemType, fieldName)
			if err != nil {
				return nil, fmt.Errorf("failed to get column name for ownership field: %w", err)
			}
			if i == 0 {
				query = query.Where("?TableAlias.? = ?", bun.Ident(colName), userID)
			} else {
				query = query.WhereOr("?TableAlias.? = ?", bun.Ident(colName), userID)
			}
		}
	}

	return query, nil
}

// setOwnershipField sets the ownership field on an item if enforced in context
// Uses metadata from context to determine which field to set
// Always sets the field when ownership is configured, regardless of bypass scopes
// (Bypass scopes only affect filtering on reads, not field population on creates)
func (w *Wrapper[T]) setOwnershipField(ctx context.Context, item *T) error {
	// Check if ownership is enforced
	enforced, ok := ctx.Value(metadata.OwnershipEnforcedKey).(bool)
	if !ok || !enforced {
		return nil
	}

	// Get ownership information from context
	userID, ok := ctx.Value(metadata.OwnershipUserIDKey).(string)
	if !ok || userID == "" {
		return fmt.Errorf("ownership enforced but user ID missing from context")
	}

	// Get metadata from context
	meta, err := metadata.FromContext(ctx)
	if err != nil || len(meta.OwnershipFields) == 0 {
		// No ownership configured for this type
		return nil
	}

	// Always set the ownership field when creating, even if user has bypass scope
	// Bypass scopes only affect read filtering, not field population on create
	// This ensures admins still "own" resources they create by default
	itemValue := reflect.ValueOf(item).Elem()
	field := itemValue.FieldByName(meta.OwnershipFields[0])
	if !field.IsValid() || !field.CanSet() {
		return fmt.Errorf("cannot set ownership field %s", meta.OwnershipFields[0])
	}

	// Set as string (ownership fields should be string type)
	if field.Kind() != reflect.String {
		return fmt.Errorf("ownership field %s must be string type, got %s", meta.OwnershipFields[0], field.Kind())
	}

	field.SetString(userID)
	return nil
}

// fieldToColumnName converts a Go field name to database column name using bun tags
// Returns error if field doesn't exist or lacks proper bun tag (required for ownership security)
func fieldToColumnName(tType reflect.Type, fieldName string) (string, error) {
	field, found := tType.FieldByName(fieldName)
	if !found {
		return "", fmt.Errorf("field %s not found on type %s", fieldName, tType.Name())
	}

	// Check bun tag for column name
	bunTag := field.Tag.Get("bun")
	if bunTag == "" {
		return "", fmt.Errorf("field %s on type %s must have bun tag with column name", fieldName, tType.Name())
	}

	parts := strings.Split(bunTag, ",")
	if len(parts) == 0 || parts[0] == "" || parts[0] == "-" {
		return "", fmt.Errorf("field %s on type %s has invalid bun tag: column name required", fieldName, tType.Name())
	}

	return parts[0], nil
}

// applyQueryFilters applies filters from QueryOptions to the query
// Only fields in metadata.FilterableFields are allowed (others silently ignored)
func (w *Wrapper[T]) applyQueryFilters(query *bun.SelectQuery, opts *metadata.QueryOptions, meta *metadata.TypeMetadata) *bun.SelectQuery {
	if opts == nil || len(opts.Filters) == 0 {
		return query
	}

	for field, filter := range opts.Filters {
		// Skip fields not in allowlist
		if !slices.Contains(meta.FilterableFields, field) {
			continue
		}

		colName, err := fieldToColumnName(meta.ModelType, field)
		if err != nil {
			continue // skip if can't resolve column
		}

		// Apply operator
		switch filter.Operator {
		case "eq", "":
			query = query.Where("?TableAlias.? = ?", bun.Ident(colName), filter.Value)
		case "neq":
			query = query.Where("?TableAlias.? != ?", bun.Ident(colName), filter.Value)
		case "gt":
			query = query.Where("?TableAlias.? > ?", bun.Ident(colName), filter.Value)
		case "gte":
			query = query.Where("?TableAlias.? >= ?", bun.Ident(colName), filter.Value)
		case "lt":
			query = query.Where("?TableAlias.? < ?", bun.Ident(colName), filter.Value)
		case "lte":
			query = query.Where("?TableAlias.? <= ?", bun.Ident(colName), filter.Value)
		case "like":
			query = query.Where("?TableAlias.? LIKE ?", bun.Ident(colName), filter.Value)
		}
	}
	return query
}

// applyQuerySorting applies sorting from QueryOptions to the query
// Falls back to metadata.DefaultSort if no sort specified
// Only fields in metadata.SortableFields are allowed (others silently ignored)
func (w *Wrapper[T]) applyQuerySorting(query *bun.SelectQuery, opts *metadata.QueryOptions, meta *metadata.TypeMetadata) *bun.SelectQuery {
	// Use sort from options if provided
	if opts != nil && len(opts.Sort) > 0 {
		for _, sort := range opts.Sort {
			if !slices.Contains(meta.SortableFields, sort.Field) {
				continue // skip invalid sort fields
			}

			colName, err := fieldToColumnName(meta.ModelType, sort.Field)
			if err != nil {
				continue
			}

			if sort.Desc {
				query = query.OrderExpr("?TableAlias.? DESC", bun.Ident(colName))
			} else {
				query = query.OrderExpr("?TableAlias.? ASC", bun.Ident(colName))
			}
		}
		return query
	}

	// Fall back to default sort from metadata
	if meta.DefaultSort != "" {
		field := meta.DefaultSort
		desc := false
		if strings.HasPrefix(field, "-") {
			desc = true
			field = field[1:]
		}

		colName, err := fieldToColumnName(meta.ModelType, field)
		if err == nil {
			if desc {
				query = query.OrderExpr("?TableAlias.? DESC", bun.Ident(colName))
			} else {
				query = query.OrderExpr("?TableAlias.? ASC", bun.Ident(colName))
			}
		}
	}

	return query
}

// applyQueryPagination applies limit/offset from QueryOptions to the query
// Uses metadata defaults if not specified in options
func (w *Wrapper[T]) applyQueryPagination(query *bun.SelectQuery, opts *metadata.QueryOptions, meta *metadata.TypeMetadata) *bun.SelectQuery {
	limit := meta.DefaultLimit
	offset := 0

	if opts != nil {
		if opts.Limit > 0 {
			limit = opts.Limit
		}
		if opts.Offset > 0 {
			offset = opts.Offset
		}
	}

	// Enforce max limit if configured
	if meta.MaxLimit > 0 && limit > meta.MaxLimit {
		limit = meta.MaxLimit
	}

	if limit > 0 {
		query = query.Limit(limit)
	}

	if offset > 0 {
		query = query.Offset(offset)
	}

	return query
}

// runValidation executes the validator function if one is configured in metadata
// Returns a ValidationError if validation fails, nil otherwise
func (w *Wrapper[T]) runValidation(ctx context.Context, meta *metadata.TypeMetadata, op metadata.Operation, old, new *T) error {
	if meta.Validator == nil {
		return nil
	}

	// Type assert the validator function
	validator, ok := meta.Validator.(metadata.ValidatorFunc[T])
	if !ok {
		return nil // validator type mismatch, skip (shouldn't happen)
	}

	// Build validation context
	vc := metadata.ValidationContext[T]{
		Operation: op,
		Old:       old,
		New:       new,
		Ctx:       ctx,
	}

	// Run the validator
	if err := validator(vc); err != nil {
		// Wrap the error in a ValidationError to preserve the message
		return apperrors.NewValidationError(err.Error())
	}

	return nil
}

// runAudit executes the audit function if one is configured in metadata
// Inserts the audit record using the provided database handle (can be tx or db)
// Returns an error if the audit insert fails
func (w *Wrapper[T]) runAudit(ctx context.Context, db bun.IDB, meta *metadata.TypeMetadata, op metadata.Operation, old, new *T) error {
	if meta.Auditor == nil {
		return nil
	}

	// Type assert the auditor function
	auditor, ok := meta.Auditor.(metadata.AuditFunc[T])
	if !ok {
		return nil // auditor type mismatch, skip (shouldn't happen)
	}

	// Build audit context
	ac := metadata.AuditContext[T]{
		Operation: op,
		Old:       old,
		New:       new,
		Ctx:       ctx,
	}

	// Run the auditor to get the audit record
	auditRecord := auditor(ac)
	if auditRecord == nil {
		return nil // nil means skip audit for this operation
	}

	// Insert the audit record
	_, err := db.NewInsert().Model(auditRecord).Exec(ctx)
	return err
}

// GetByParentRelation retrieves a single item of type T via the parent's foreign key field
// Fetches the parent, extracts the FK value, then calls normal Get (preserving security checks)
func (w *Wrapper[T]) GetByParentRelation(ctx context.Context, parentID string) (*T, error) {
	childID, err := w.resolveChildIDFromParent(ctx, parentID)
	if err != nil {
		return nil, err
	}
	return w.Get(ctx, childID)
}

// UpdateByParentRelation updates a single item of type T via the parent's foreign key field
// Fetches the parent, extracts the FK value, then calls normal Update (preserving security checks)
func (w *Wrapper[T]) UpdateByParentRelation(ctx context.Context, parentID string, item T) (*T, error) {
	childID, err := w.resolveChildIDFromParent(ctx, parentID)
	if err != nil {
		return nil, err
	}
	return w.Update(ctx, childID, item)
}

// resolveChildIDFromParent fetches the parent and extracts the foreign key value
// For belongs-to relations like /posts/{id}/author, Post.AuthorID points to User.ID
func (w *Wrapper[T]) resolveChildIDFromParent(ctx context.Context, parentID string) (string, error) {
	meta, err := metadata.FromContext(ctx)
	if err != nil {
		return "", err
	}

	if meta.ParentMeta == nil {
		return "", fmt.Errorf("resolveChildIDFromParent requires parent metadata")
	}

	if meta.ParentFKField == "" {
		return "", fmt.Errorf("resolveChildIDFromParent requires ParentFKField to be set")
	}

	// Fetch the parent using getWithMeta (reuses existing logic)
	parent, err := w.getWithMeta(ctx, meta.ParentMeta, parentID)
	if err != nil {
		return "", err
	}

	// Extract the FK field value from the parent
	parentValue := reflect.ValueOf(parent).Elem()
	fkField := parentValue.FieldByName(meta.ParentFKField)
	if !fkField.IsValid() {
		return "", fmt.Errorf("FK field %s not found on parent", meta.ParentFKField)
	}

	return fmt.Sprintf("%v", fkField.Interface()), nil
}

// applyRelationIncludes adds relation loading for includes specified in query options.
// Authorization is checked via AllowedIncludes from context (set by wrapWithAuth middleware).
// Only relations in AllowedIncludes AND registered in ChildMeta are loaded.
// The bool value in AllowedIncludes indicates whether to apply ownership filtering.
func (w *Wrapper[T]) applyRelationIncludes(ctx context.Context, query *bun.SelectQuery, opts *metadata.QueryOptions, meta *metadata.TypeMetadata) *bun.SelectQuery {
	if opts == nil || len(opts.Include) == 0 || meta == nil {
		return query
	}

	// Get allowed includes from context (set by wrapWithAuth based on child auth configs)
	allowedIncludes := metadata.AllowedIncludesFromContext(ctx)

	for _, relationName := range opts.Include {
		// Check if this relation is authorized (in AllowedIncludes)
		applyOwnership, authorized := allowedIncludes[relationName]
		if !authorized {
			// Silently ignore unauthorized relations for security
			continue
		}

		// Check if this relation is registered in ChildMeta
		childMeta, exists := meta.ChildMeta[relationName]
		if !exists {
			// Silently ignore unknown relations for security
			continue
		}

		// Capture for closure
		cm := childMeta
		shouldApplyOwnership := applyOwnership

		// Add the relation with ownership filtering applied based on authorization
		query = query.Relation(relationName, func(q *bun.SelectQuery) *bun.SelectQuery {
			if !shouldApplyOwnership {
				// User has bypass scope for this child - no ownership filter
				return q
			}

			// Apply ownership filter with child's metadata
			filtered, err := w.applyOwnershipFilterWithMeta(ctx, q, cm)
			if err != nil {
				// On error, return empty result set (secure default)
				return q.Where("1 = 0")
			}
			return filtered
		})
	}

	return query
}

// BatchCreate creates multiple items in a single transaction.
// All-or-nothing: if any item fails, the entire batch is rolled back.
// Ownership and parent validation are applied per-item.
func (w *Wrapper[T]) BatchCreate(ctx context.Context, items []T) ([]*T, error) {
	ctx, cancel := context.WithTimeout(ctx, w.Store.GetTimeout())
	defer cancel()

	meta, err := metadata.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	results := make([]*T, 0, len(items))

	err = w.Store.GetDB().RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		for i := range items {
			item := &items[i]

			// Validate parent and set FK if nested
			if meta.ParentMeta != nil {
				parentMeta := meta.ParentMeta
				parentIDs, ok := ctx.Value(metadata.ParentIDsKey).(map[string]string)
				if !ok || parentIDs == nil {
					return fmt.Errorf("parent context missing for nested resource")
				}
				parentID, exists := parentIDs[parentMeta.URLParamUUID]
				if !exists {
					return fmt.Errorf("parent ID not found in context")
				}
				parentItem, err := w.getWithMeta(ctx, parentMeta, parentID)
				if err != nil {
					return err
				}
				if err := w.setForeignKey(item, meta.ForeignKeyCol, parentItem, parentMeta.PKField); err != nil {
					return err
				}
			}

			// Set ownership field
			if err := w.setOwnershipField(ctx, item); err != nil {
				return err
			}

			// Run validation
			if err := w.runValidation(ctx, meta, metadata.OpCreate, nil, item); err != nil {
				return err
			}

			// Insert the item
			_, err := tx.NewInsert().Model(item).Returning("*").Exec(ctx)
			if err != nil {
				return w.translateError(err)
			}

			// Run audit
			if err := w.runAudit(ctx, tx, meta, metadata.OpCreate, nil, item); err != nil {
				return err
			}

			results = append(results, item)
		}
		return nil
	})

	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, err
	}

	return results, nil
}

// BatchUpdate updates multiple items in a single transaction.
// All-or-nothing: if any item fails, the entire batch is rolled back.
// Ownership validation is applied per-item via Get before the transaction starts.
func (w *Wrapper[T]) BatchUpdate(ctx context.Context, items []T) ([]*T, error) {
	ctx, cancel := context.WithTimeout(ctx, w.Store.GetTimeout())
	defer cancel()

	meta, err := metadata.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	// Pre-fetch all existing items and validate before starting transaction
	preFetch, err := w.preFetchItems(ctx, meta, items, metadata.OpUpdate)
	if err != nil {
		return nil, err
	}

	results := make([]*T, 0, len(items))

	err = w.Store.GetDB().RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		for i := range items {
			item := &items[i]

			// Update the item
			err := tx.NewUpdate().Model(item).WherePK().Returning("*").Scan(ctx)
			if err != nil {
				return w.translateError(err)
			}

			// Run audit within transaction
			if err := w.runAudit(ctx, tx, meta, metadata.OpUpdate, preFetch.existingItems[i], item); err != nil {
				return err
			}

			results = append(results, item)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return results, nil
}

// BatchDelete deletes multiple items in a single transaction.
// All-or-nothing: if any item fails, the entire batch is rolled back.
// Items must have at least an ID field set.
func (w *Wrapper[T]) BatchDelete(ctx context.Context, items []T) error {
	ctx, cancel := context.WithTimeout(ctx, w.Store.GetTimeout())
	defer cancel()

	meta, err := metadata.FromContext(ctx)
	if err != nil {
		return err
	}

	// Pre-fetch all existing items and validate before starting transaction
	preFetch, err := w.preFetchItems(ctx, meta, items, metadata.OpDelete)
	if err != nil {
		return err
	}

	err = w.Store.GetDB().RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		for i := range items {
			item := &items[i]

			// Delete the item
			result, err := tx.NewDelete().Model(item).Where("?TablePKs = ?", preFetch.ids[i]).Exec(ctx)
			if err != nil {
				return w.translateError(err)
			}

			rowsAffected, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if rowsAffected == 0 {
				return apperrors.ErrNotFound
			}

			// Run audit within transaction
			if err := w.runAudit(ctx, tx, meta, metadata.OpDelete, preFetch.existingItems[i], nil); err != nil {
				return err
			}
		}
		return nil
	})

	return err
}

// extractID extracts the primary key field value from an item as a string.
// The pkFieldName parameter specifies which field to extract (from metadata.PKField).
func (w *Wrapper[T]) extractID(item *T, pkFieldName string) string {
	v := reflect.ValueOf(item).Elem()
	idField := v.FieldByName(pkFieldName)
	if !idField.IsValid() {
		return ""
	}
	return fmt.Sprintf("%v", idField.Interface())
}

// preFetchItems validates and fetches existing items before a batch operation.
// This validates existence, ownership, and parent chain for each item.
// For update operations, pass the new item to validation; for delete, pass nil.
func (w *Wrapper[T]) preFetchItems(ctx context.Context, meta *metadata.TypeMetadata, items []T, op metadata.Operation) (*preFetchResult[T], error) {
	result := &preFetchResult[T]{
		ids:           make([]string, len(items)),
		existingItems: make([]*T, len(items)),
	}

	for i := range items {
		id := w.extractID(&items[i], meta.PKField)
		if id == "" {
			return nil, fmt.Errorf("item at index %d missing ID", i)
		}
		result.ids[i] = id

		existing, err := w.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		result.existingItems[i] = existing

		// Run validation: for update pass new item, for delete pass nil
		var newItem *T
		if op == metadata.OpUpdate {
			newItem = &items[i]
		}
		if err := w.runValidation(ctx, meta, op, existing, newItem); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// translateError converts database errors to application errors
func (w *Wrapper[T]) translateError(err error) error {
	if errors.Is(err, sql.ErrConnDone) {
		return apperrors.ErrUnavailable
	}
	if errors.Is(err, sql.ErrNoRows) {
		return apperrors.ErrNotFound
	}
	var pgErr pgdriver.Error
	if errors.As(err, &pgErr) {
		switch pgErr.Field('C') {
		case "23505":
			return apperrors.ErrDuplicate
		case "23503":
			return apperrors.ErrInvalidReference
		}
	}
	if strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return apperrors.ErrDuplicate
	}
	if strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
		return apperrors.ErrInvalidReference
	}
	return err
}
