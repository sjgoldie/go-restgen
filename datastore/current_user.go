package datastore

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/uptrace/bun"

	apperrors "github.com/sjgoldie/go-restgen/errors"
	"github.com/sjgoldie/go-restgen/internal/common"
	"github.com/sjgoldie/go-restgen/metadata"
)

// ResolveKeyByField returns the primary key, as a string, of the one row of meta's type in store
// whose field equals value. On a tenant route the row must belong to tenantID, and on an RLS route
// the lookup runs in a transaction scoped to it. ErrNotFound when no row, or more than one, matches.
func ResolveKeyByField(ctx context.Context, store Store, meta *metadata.TypeMetadata, field, value, tenantID string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, store.GetTimeout())
	defer cancel()

	db := store.GetDB()
	modelType := derefType(meta.ModelType)
	table := db.Table(modelType)
	if len(table.PKs) != 1 {
		return "", fmt.Errorf("type %s must have a single primary key", modelType.Name())
	}
	col, err := ColumnName(modelType, field)
	if err != nil {
		return "", err
	}

	var idb bun.IDB = db
	if meta.UseRLS && tenantID != "" {
		tx, err := BeginTenantTx(ctx, db, tenantID)
		if err != nil {
			return "", err
		}
		defer func() { _ = tx.Rollback() }()
		idb = tx
	}

	query := idb.NewSelect().
		Table(table.Name).
		ColumnExpr("?", bun.Ident(table.PKs[0].Name)).
		Where("? = ?", bun.Ident(col), value).
		Limit(2)
	if meta.TenantField != "" {
		tenantCol, err := ColumnName(modelType, meta.TenantField)
		if err != nil {
			return "", err
		}
		query = query.Where("? = ?", bun.Ident(tenantCol), tenantID)
	}

	var keys []string
	if err := query.Scan(ctx, &keys); err != nil {
		return "", err
	}
	if len(keys) > 1 {
		slog.ErrorContext(ctx, "current user field matches more than one row", "type", modelType.Name(), "field", field)
	}
	if len(keys) != 1 {
		return "", apperrors.ErrNotFound
	}
	return keys[0], nil
}

// setCurrentUserField writes the caller's user ID to the identity field of an AsCurrentUserExternal
// route's row, so an update cannot change which caller the row belongs to.
func setCurrentUserField(ctx context.Context, meta *metadata.TypeMetadata, item any) error {
	if meta.CurrentUserField == "" {
		return nil
	}
	authInfo, _ := ctx.Value(metadata.AuthInfoKey).(*metadata.AuthInfo)
	if authInfo == nil || authInfo.UserID == "" {
		return apperrors.ErrForbidden
	}
	if err := common.SetFieldFromString(item, meta.CurrentUserField, authInfo.UserID); err != nil {
		return fmt.Errorf("set current user field: %w", err)
	}
	return nil
}
