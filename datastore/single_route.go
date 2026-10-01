package datastore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/uptrace/bun"

	apperrors "github.com/sjgoldie/go-restgen/errors"
	"github.com/sjgoldie/go-restgen/metadata"
)

// ResolveSingleRouteKey returns the key of a nested single route's row: the value of the
// parent's ParentFKField on the parent row whose key is parentID, as a string. ErrNotFound when
// the parent row does not exist or does not point at a row. Access to the parent is not checked
// here; requests that use the key still scope every level of the parent chain.
func ResolveSingleRouteKey(ctx context.Context, meta *metadata.TypeMetadata, parentID string) (string, error) {
	parent := meta.ParentMeta
	if parent == nil || meta.ParentFKField == "" {
		return "", fmt.Errorf("type %s is not a nested single route", meta.TypeName)
	}
	store, err := Get()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, store.GetTimeout())
	defer cancel()

	parentType := derefType(parent.ModelType)
	table := store.GetDB().Table(parentType)
	if len(table.PKs) != 1 {
		return "", fmt.Errorf("type %s must have a single primary key", parentType.Name())
	}
	fkCol, err := ColumnName(parentType, meta.ParentFKField)
	if err != nil {
		return "", err
	}

	var key sql.NullString
	err = store.GetDB().NewSelect().
		Table(table.Name).
		ColumnExpr("?", bun.Ident(fkCol)).
		Where("? = ?", bun.Ident(table.PKs[0].Name), parentID).
		Limit(1).
		Scan(ctx, &key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", apperrors.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if !key.Valid || key.String == "" {
		return "", apperrors.ErrNotFound
	}
	return key.String, nil
}
