package router

import (
	"reflect"
	"testing"

	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/metadata"
)

type shareInternalProject struct {
	bun.BaseModel `bun:"table:share_internal_projects"`
	ID            int `bun:"id,pk,autoincrement"`
}

type shareInternalTask struct {
	bun.BaseModel `bun:"table:share_internal_tasks"`
	ID            int `bun:"id,pk,autoincrement"`
	ProjectID     int `bun:"project_id"`
}

type shareInternalReport struct {
	bun.BaseModel `bun:"table:share_internal_reports"`
	ID            int                   `bun:"id,pk,autoincrement"`
	ProjectID     int                   `bun:"project_id"`
	Project       *shareInternalProject `bun:"rel:belongs-to,join:project_id=id"`
}

type shareInternalGrant struct {
	bun.BaseModel `bun:"table:share_internal_grants"`
	ID            int    `bun:"id,pk,autoincrement"`
	ProjectID     int    `bun:"project_id"`
	UserID        string `bun:"user_id"`
	Level         string `bun:"level"`
}

func TestResolveShare(t *testing.T) {
	projectMeta := &metadata.TypeMetadata{TypeName: "shareInternalProject", ModelType: reflect.TypeFor[shareInternalProject]()}
	taskMeta := &metadata.TypeMetadata{TypeName: "shareInternalTask", ModelType: reflect.TypeFor[shareInternalTask](), ParentMeta: projectMeta}

	valid := func() *ShareConfig {
		return &ShareConfig{
			Model:       (*shareInternalGrant)(nil),
			TargetField: "ProjectID",
			UserField:   "UserID",
			LevelField:  "Level",
			Levels:      []string{"editor"},
		}
	}

	t.Run("own model", func(t *testing.T) {
		got := resolveShare(valid(), projectMeta, "/projects")
		want := &metadata.Share{
			ModelType:   reflect.TypeFor[shareInternalGrant](),
			TargetType:  reflect.TypeFor[shareInternalProject](),
			TargetField: "ProjectID",
			UserField:   "UserID",
			LevelField:  "Level",
			Levels:      []string{"editor"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("ancestor model", func(t *testing.T) {
		cfg := valid()
		cfg.Target = (*shareInternalProject)(nil)
		got := resolveShare(cfg, taskMeta, "/projects/{id}/tasks")
		if got == nil || got.TargetType != reflect.TypeFor[shareInternalProject]() {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("relation path", func(t *testing.T) {
		reportMeta := &metadata.TypeMetadata{TypeName: "shareInternalReport", ModelType: reflect.TypeFor[shareInternalReport]()}
		cfg := valid()
		cfg.Via = "Project"
		got := resolveShare(cfg, reportMeta, "/reports")
		if got == nil {
			t.Fatal("expected a share")
		}
		if got.BaseType != reflect.TypeFor[shareInternalReport]() || got.TargetType != reflect.TypeFor[shareInternalProject]() {
			t.Errorf("got base %v, target %v", got.BaseType, got.TargetType)
		}
		wantVia := []metadata.RelationStep{{
			Name: "Project", ModelType: reflect.TypeFor[shareInternalProject](), Table: "share_internal_projects",
			FKColumn: "project_id", JoinColumn: "id", PKColumn: "id",
		}}
		if !reflect.DeepEqual(got.Via, wantVia) {
			t.Errorf("got %+v, want %+v", got.Via, wantVia)
		}

		cfg.Target = (*shareInternalProject)(nil)
		if got := resolveShare(cfg, reportMeta, "/reports"); got != nil {
			t.Errorf("Via with Target: expected no share, got %+v", got)
		}
		cfg.Target = nil
		cfg.Via = "Owner"
		if got := resolveShare(cfg, reportMeta, "/reports"); got != nil {
			t.Errorf("unknown relation: expected no share, got %+v", got)
		}
	})

	invalid := map[string]func() *ShareConfig{
		"no model": func() *ShareConfig { c := valid(); c.Model = nil; return c },
		"target not in the chain": func() *ShareConfig {
			c := valid()
			c.Target = (*shareInternalTask)(nil)
			return c
		},
		"missing target field":       func() *ShareConfig { c := valid(); c.TargetField = ""; return c },
		"unknown user field":         func() *ShareConfig { c := valid(); c.UserField = "Missing"; return c },
		"levels without level field": func() *ShareConfig { c := valid(); c.LevelField = ""; return c },
		"unknown level field":        func() *ShareConfig { c := valid(); c.LevelField = "Missing"; return c },
	}
	for name, cfg := range invalid {
		t.Run(name, func(t *testing.T) {
			if got := resolveShare(cfg(), projectMeta, "/projects"); got != nil {
				t.Errorf("expected no share, got %+v", got)
			}
		})
	}

	if got := resolveShare(nil, projectMeta, "/projects"); got != nil {
		t.Errorf("nil config: got %+v", got)
	}
}

func TestResolveShares(t *testing.T) {
	projectMeta := &metadata.TypeMetadata{TypeName: "shareInternalProject", ModelType: reflect.TypeFor[shareInternalProject]()}
	withShare := &AuthConfig{Share: &ShareConfig{Model: (*shareInternalGrant)(nil), TargetField: "ProjectID", UserField: "UserID"}}
	withoutShare := &AuthConfig{Scopes: []string{"read"}}

	resolveShares(map[string]*AuthConfig{MethodGet: withShare, MethodPost: withoutShare, MethodDelete: nil}, projectMeta, "/projects")

	if withShare.share == nil {
		t.Error("expected the share setting to be resolved")
	}
	if withoutShare.share != nil {
		t.Errorf("config without a share setting: got %+v", withoutShare.share)
	}
}
