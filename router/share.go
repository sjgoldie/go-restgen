package router

import (
	"context"
	"log/slog"
	"reflect"
	"strings"

	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/metadata"
)

// ShareConfig lets rows shared with the caller be accessed through an AuthConfig's methods,
// in addition to ownership and partition access. A share is a row of the share model that
// links a user ID to a shared row, optionally with a level.
//
// Target is the shared model. Leave it nil when the route's own rows are shared; set it to
// an ancestor route's model (e.g. (*Project)(nil) on a task route) to accept shares of the
// parent row for its children. A child route does not accept parent shares unless it says so.
//
// Via accepts shares of a row reached from the route's row through belongs-to relations,
// e.g. "Project" on a top-level task route accepts shares of each task's project. Via and
// Target cannot be combined.
//
// Shares widen access: a shared row is accessible even when the caller's ownership or
// partition access would exclude it, but tenant scope and the route's required scopes
// still apply. The caller must have a user ID.
type ShareConfig struct {
	Model       any      // Share model, e.g. (*ProjectShare)(nil)
	Target      any      // Shared model, e.g. (*Project)(nil); nil for the route's own model
	Via         string   // Belongs-to relation path from the route's model to the shared model (e.g., "Project")
	TargetField string   // Share model field holding the shared row's primary key (e.g., "ProjectID")
	UserField   string   // Share model field holding the user ID (e.g., "UserID")
	LevelField  string   // Optional share model field holding the share level (e.g., "Level")
	Levels      []string // Levels accepted for these methods; empty accepts any share
}

// resolveShare resolves a ShareConfig against the route's model and parent chain. An invalid
// config is logged and resolves to nil, so it grants no access.
func resolveShare(cfg *ShareConfig, meta *metadata.TypeMetadata, path string) *metadata.Share {
	if cfg == nil {
		return nil
	}

	warn := func(msg string, args ...any) *metadata.Share {
		args = append([]any{"type", meta.TypeName, "path", path}, args...)
		slog.WarnContext(context.Background(), "ShareConfig "+msg+"; shares are not accepted", args...)
		return nil
	}

	shareType := modelType(cfg.Model)
	if shareType == nil {
		return warn("requires a share Model")
	}

	baseType := derefModelType(meta.ModelType)
	targetType := baseType
	var via []metadata.RelationStep
	switch {
	case cfg.Via != "" && cfg.Target != nil:
		return warn("cannot set both Via and Target")
	case cfg.Via != "":
		steps, related, err := datastore.ResolveRelationPath(baseType, strings.Split(cfg.Via, "."))
		if err != nil {
			return warn("Via is not a chain of belongs-to relations", "via", cfg.Via, "error", err)
		}
		via, targetType = steps, related
	case cfg.Target != nil:
		targetType = modelType(cfg.Target)
		if !shareTargetInChain(meta, targetType) {
			return warn("Target is not this route's model or an ancestor's", "target", targetType.Name())
		}
	}

	for _, field := range []string{cfg.TargetField, cfg.UserField} {
		if field == "" {
			return warn("requires TargetField and UserField")
		}
		if _, err := datastore.ColumnName(shareType, field); err != nil {
			return warn("field is not a column on the share model", "field", field, "error", err)
		}
	}
	if cfg.LevelField == "" && len(cfg.Levels) > 0 {
		return warn("sets Levels without a LevelField")
	}
	if cfg.LevelField != "" {
		if _, err := datastore.ColumnName(shareType, cfg.LevelField); err != nil {
			return warn("field is not a column on the share model", "field", cfg.LevelField, "error", err)
		}
	}

	share := &metadata.Share{
		ModelType:   shareType,
		TargetType:  targetType,
		TargetField: cfg.TargetField,
		UserField:   cfg.UserField,
		LevelField:  cfg.LevelField,
		Levels:      cfg.Levels,
	}
	if len(via) > 0 {
		share.BaseType = baseType
		share.Via = via
	}
	return share
}

// resolveShares resolves the share setting of every config for a route.
func resolveShares(configs map[string]*AuthConfig, meta *metadata.TypeMetadata, path string) {
	for _, config := range configs {
		if config != nil {
			config.share = resolveShare(config.Share, meta, path)
		}
	}
}

// shareTargetInChain reports whether target is the model of meta or of one of its ancestors.
func shareTargetInChain(meta *metadata.TypeMetadata, target reflect.Type) bool {
	for current := meta; current != nil; current = current.ParentMeta {
		if derefModelType(current.ModelType) == target {
			return true
		}
	}
	return false
}

// modelType returns the struct type of a model value such as (*Project)(nil).
func modelType(model any) reflect.Type {
	if model == nil {
		return nil
	}
	return derefModelType(reflect.TypeOf(model))
}

// derefModelType unwraps a pointer type to its element type.
func derefModelType(t reflect.Type) reflect.Type {
	if t != nil && t.Kind() == reflect.Pointer {
		return t.Elem()
	}
	return t
}
