package datastore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
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

// GetAll retrieves all items of type T from the datastore
// Filters from context are applied automatically
func (w *Wrapper[T]) GetAll(ctx context.Context, relations []string) ([]*T, error) {
	ctx, cancel := context.WithTimeout(ctx, w.Store.GetTimeout())
	defer cancel()

	items := []*T{}
	query := w.Store.GetDB().NewSelect().Model(&items)

	// Get metadata from context
	meta, err := metadata.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	// Apply parent filters and JOINs from metadata
	query, err = w.applyParentFiltersWithMeta(ctx, query, meta)
	if err != nil {
		return nil, err
	}

	// Apply ownership filter for type T
	query, err = w.applyOwnershipFilterWithMeta(ctx, query, meta)
	if err != nil {
		return nil, err
	}

	// Add relations if specified
	for _, relation := range relations {
		query = query.Relation(relation)
	}

	if err := query.Scan(ctx); err != nil {
		// Pass through context errors unchanged
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		// Check for connection issues
		if errors.Is(err, sql.ErrConnDone) {
			return nil, apperrors.ErrUnavailable
		}
		return nil, err
	}

	return items, nil
}

// Get retrieves a single item of type T by ID from the datastore
// Filters from context (parent IDs) are applied automatically
func (w *Wrapper[T]) Get(ctx context.Context, id int, relations []string) (*T, error) {
	// Get metadata from context
	meta, err := metadata.FromContext(ctx)
	if err != nil {
		return nil, err
	}
	item, err := w.getWithMeta(ctx, meta, id, relations)
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
		parentIDs, ok := ctx.Value("parentIDs").(map[string]int)
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
		_, err = w.getWithMeta(ctx, parentMeta, parentID, []string{})
		if err != nil {
			return nil, err
		}

		// Set the foreign key field on the item
		if err := w.setForeignKey(&item, meta.ForeignKeyCol, parentID); err != nil {
			return nil, err
		}
	}

	// Set ownership field if enforced
	if err := w.setOwnershipField(ctx, &item); err != nil {
		return nil, err
	}

	// Use Returning to get the created record back with generated fields
	_, err = w.Store.GetDB().NewInsert().Model(&item).Returning("*").Exec(ctx)
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
func (w *Wrapper[T]) Update(ctx context.Context, id int, item T) (*T, error) {
	ctx, cancel := context.WithTimeout(ctx, w.Store.GetTimeout())
	defer cancel()

	// Validate item exists (and belongs to parent chain if applicable)
	// This also provides a hook for future authorization checks
	_, err := w.Get(ctx, id, []string{})
	if err != nil {
		return nil, err
	}

	// Use Returning to get the updated record back
	err = w.Store.GetDB().NewUpdate().Model(&item).WherePK().Returning("*").Scan(ctx)
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
func (w *Wrapper[T]) Delete(ctx context.Context, id int) error {
	ctx, cancel := context.WithTimeout(ctx, w.Store.GetTimeout())
	defer cancel()

	// Validate item exists (and belongs to parent chain if applicable)
	// This also provides a hook for future authorization checks
	_, err := w.Get(ctx, id, []string{})
	if err != nil {
		return err
	}

	var item T
	result, err := w.Store.GetDB().NewDelete().Model(&item).Where("? = ?", bun.Ident("id"), id).Exec(ctx)
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
func (w *Wrapper[T]) getWithMeta(ctx context.Context, meta *metadata.TypeMetadata, id int, relations []string) (interface{}, error) {
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

	// Add relations if specified
	for _, relation := range relations {
		query = query.Relation(relation)
	}

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

// setForeignKey sets the foreign key field on the item using reflection
func (w *Wrapper[T]) setForeignKey(item *T, foreignKeyCol string, parentID int) error {
	// Convert column name to field name (e.g., "author_id" -> "AuthorID")
	fieldName := fieldNameFromColumn(foreignKeyCol)

	// Use reflection to set the field
	itemValue := reflect.ValueOf(item).Elem()
	fkField := itemValue.FieldByName(fieldName)
	if !fkField.IsValid() || !fkField.CanSet() {
		return fmt.Errorf("cannot set foreign key field %s", fieldName)
	}
	fkField.SetInt(int64(parentID))

	return nil
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
	parentIDs, ok := ctx.Value("parentIDs").(map[string]int)
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

		// Check if child is the base model being queried
		if join.childType == baseType {
			// Child is the base model, use ?TableAlias
			query = query.Join("JOIN ? ON ?TableAlias.? = ?.?",
				bun.Ident(join.parentTable),
				bun.Ident(join.childFKCol),
				bun.Ident(join.parentTable), bun.Ident("id"))
		} else {
			// Child is a previously joined table, use table name
			query = query.Join("JOIN ? ON ?.? = ?.?",
				bun.Ident(join.parentTable),
				bun.Ident(join.childTable), bun.Ident(join.childFKCol),
				bun.Ident(join.parentTable), bun.Ident("id"))
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
	enforced, ok := ctx.Value("ownershipEnforced").(bool)
	if !ok || !enforced {
		return query, nil
	}

	// Get ownership information from context
	userID, ok := ctx.Value("ownershipUserID").(string)
	if !ok || userID == "" {
		return nil, fmt.Errorf("ownership enforced but user ID missing from context")
	}

	// If no metadata or no ownership fields configured for this type, skip filter
	if meta == nil || len(meta.OwnershipFields) == 0 {
		return query, nil
	}

	// Check if user has bypass scope
	// Compare user's scopes (from AuthInfo in context) with bypass scopes from metadata
	if authInfo, ok := ctx.Value("authInfo").(*metadata.AuthInfo); ok && authInfo != nil && len(meta.BypassScopes) > 0 {
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
	enforced, ok := ctx.Value("ownershipEnforced").(bool)
	if !ok || !enforced {
		return nil
	}

	// Get ownership information from context
	userID, ok := ctx.Value("ownershipUserID").(string)
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
		return "", fmt.Errorf("ownership field %s not found on type %s", fieldName, tType.Name())
	}

	// Check bun tag for column name
	bunTag := field.Tag.Get("bun")
	if bunTag == "" {
		return "", fmt.Errorf("ownership field %s on type %s must have bun tag with column name", fieldName, tType.Name())
	}

	parts := strings.Split(bunTag, ",")
	if len(parts) == 0 || parts[0] == "" || parts[0] == "-" {
		return "", fmt.Errorf("ownership field %s on type %s has invalid bun tag: column name required", fieldName, tType.Name())
	}

	return parts[0], nil
}
