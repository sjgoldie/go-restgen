package datastore

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/schema"

	"github.com/sjgoldie/go-restgen/metadata"
)

// joinClauses combines clauses with sep into one parenthesised clause.
func joinClauses(clauses []clause, sep string) clause {
	if len(clauses) == 1 {
		return clause{query: "(" + clauses[0].query + ")", args: clauses[0].args}
	}
	parts := make([]string, len(clauses))
	var args []any
	for i, c := range clauses {
		parts[i] = "(" + c.query + ")"
		args = append(args, c.args...)
	}
	return clause{query: "(" + strings.Join(parts, sep) + ")", args: args}
}

// accessQuery narrows query to rows that pass every restrict clause, or, when shared is set,
// rows that pass them all or are shared with the caller. With no restrictions nothing is added.
func accessQuery(query *bun.SelectQuery, restrict []clause, shared *clause) *bun.SelectQuery {
	if len(restrict) == 0 {
		return query
	}
	if shared == nil {
		for _, c := range restrict {
			query = query.Where(c.query, c.args...)
		}
		return query
	}
	c := joinClauses([]clause{joinClauses(restrict, " AND "), *shared}, " OR ")
	return query.Where(c.query, c.args...)
}

// accessJoinConditions returns JOIN ON conditions equivalent to accessQuery for a joined relation.
func accessJoinConditions(restrict []clause, shared *clause) []schema.QueryWithArgs {
	if len(restrict) == 0 {
		return nil
	}
	if shared != nil {
		restrict = []clause{joinClauses([]clause{joinClauses(restrict, " AND "), *shared}, " OR ")}
	}
	conditions := make([]schema.QueryWithArgs, len(restrict))
	for i, c := range restrict {
		conditions[i] = schema.SafeQuery(c.query, c.args)
	}
	return conditions
}

// rowAccess applies the parent chain, ownership, tenant, partition, and share scoping for the
// route's own rows of meta. Ownership and partitions (on the row and on the parents in the URL)
// are widened by the method's share setting; parent IDs and tenant scope are not.
func (w *Wrapper[T]) rowAccess(ctx context.Context, query *bun.SelectQuery, meta *metadata.TypeMetadata) (*bun.SelectQuery, error) {
	query, restrict, err := w.applyParentFiltersWithMeta(ctx, query, meta)
	if err != nil {
		return nil, err
	}

	owned, err := w.ownershipClauses(ctx, meta)
	if err != nil {
		return nil, err
	}
	if len(owned) == 0 {
		owned = w.parentOwnershipClauses(ctx, meta)
	}
	restrict = append(restrict, owned...)

	query, err = w.applyTenantFilter(ctx, query, meta)
	if err != nil {
		return nil, err
	}

	restrict = append(restrict, w.partitionRestrict(ctx, meta, columnRef{}, "")...)
	return accessQuery(query, restrict, w.shareClause(ctx, meta, columnRef{}, shareFor(ctx, ""))), nil
}

// parentOwnershipClauses returns the ownership condition for a parent type fetched directly,
// such as when validating the parent of a new child, when the auth middleware marked it as a
// parent needing ownership. This applies the same parent ownership rule as the URL chain.
func (w *Wrapper[T]) parentOwnershipClauses(ctx context.Context, meta *metadata.TypeMetadata) []clause {
	parents, _ := ctx.Value(metadata.ParentOwnershipKey).([]*metadata.TypeMetadata)
	if !slices.Contains(parents, meta) || len(meta.OwnershipFields) == 0 {
		return nil
	}
	authInfo, _ := ctx.Value(metadata.AuthInfoKey).(*metadata.AuthInfo)
	if authInfo == nil || authInfo.UserID == "" {
		return []clause{noRows}
	}
	bypass := w.scopedBypassClauses(ctx, meta, columnRef{}, meta.BypassScopes)
	return []clause{w.ownedClause(meta, columnRef{}, meta.OwnershipFields, authInfo.UserID, bypass)}
}

// partitionRestrict returns the partition conditions for the rows of meta at ref, using the
// access for path ("" for the route's own type). Nil when meta has no partitions or narrowing
// is not enforced for the request.
func (w *Wrapper[T]) partitionRestrict(ctx context.Context, meta *metadata.TypeMetadata, ref columnRef, path string) []clause {
	if meta == nil || len(meta.Partitions) == 0 {
		return nil
	}
	scope, enforced := partitionScopeFor(ctx, path)
	if !enforced {
		return nil
	}
	return w.partitionClauses(ctx, meta, ref, scope)
}

// shareFor returns the share setting for the route's own method (path "") or for a relation path.
func shareFor(ctx context.Context, path string) *metadata.Share {
	if path == "" {
		share, _ := ctx.Value(metadata.ShareKey).(*metadata.Share)
		return share
	}
	shares, _ := ctx.Value(metadata.IncludeSharesKey).(map[string]*metadata.Share)
	return shares[path]
}

// shareClause builds the condition that a row of meta at ref is shared with the caller: the
// row itself is the share target, or its ancestor chain leads to a shared target row. Nil when
// there is no share setting, no caller user ID, or the target is not meta or an ancestor.
func (w *Wrapper[T]) shareClause(ctx context.Context, meta *metadata.TypeMetadata, ref columnRef, share *metadata.Share) *clause {
	if share == nil || meta == nil {
		return nil
	}
	authInfo, ok := ctx.Value(metadata.AuthInfoKey).(*metadata.AuthInfo)
	if !ok || authInfo == nil || authInfo.UserID == "" {
		return nil
	}

	if derefType(meta.ModelType) == share.TargetType {
		subq, err := w.sharedTargets(ctx, share, authInfo.UserID)
		if err != nil {
			return &noRows
		}
		query, args := ref.column(w.pkColumn(meta))
		return &clause{query: query + " IN (?)", args: append(args, subq)}
	}

	parent := meta.ParentMeta
	if parent == nil || meta.ForeignKeyCol == "" {
		return nil
	}
	parentClause := w.shareClause(ctx, parent, columnRef{table: parent.TableName}, share)
	if parentClause == nil {
		return nil
	}

	joinCol := defaultParentJoinCol(meta.ParentJoinCol)
	subq := w.Store.GetDB().NewSelect().Table(parent.TableName)
	childCol := meta.ForeignKeyCol
	if w.hasColumn(meta.ModelType, meta.ForeignKeyCol) {
		subq = subq.ColumnExpr("?.?", bun.Ident(parent.TableName), bun.Ident(joinCol))
	} else {
		subq = subq.ColumnExpr("?.?", bun.Ident(parent.TableName), bun.Ident(meta.ForeignKeyCol))
		childCol = joinCol
	}
	subq = subq.Where(parentClause.query, parentClause.args...)

	query, args := ref.column(childCol)
	return &clause{query: query + " IN (?)", args: append(args, subq)}
}

// sharedTargets selects the primary keys of target rows shared with userID at an accepted level.
func (w *Wrapper[T]) sharedTargets(ctx context.Context, share *metadata.Share, userID string) (*bun.SelectQuery, error) {
	table := w.Store.GetDB().Table(share.ModelType).Name
	targetCol, err := ColumnName(share.ModelType, share.TargetField)
	if err != nil {
		return nil, err
	}
	userCol, err := ColumnName(share.ModelType, share.UserField)
	if err != nil {
		return nil, err
	}

	subq := w.Store.GetDB().NewSelect().
		Table(table).
		ColumnExpr("?.?", bun.Ident(table), bun.Ident(targetCol)).
		Where("?.? = ?", bun.Ident(table), bun.Ident(userCol), userID)

	if len(share.Levels) > 0 {
		levelCol, err := ColumnName(share.ModelType, share.LevelField)
		if err != nil {
			return nil, fmt.Errorf("share level field: %w", err)
		}
		levels := partitionFilterValues(ctx, share.ModelType, share.LevelField, share.Levels)
		subq = subq.Where("?.? IN (?)", bun.Ident(table), bun.Ident(levelCol), bun.List(levels))
	}
	return subq, nil
}
